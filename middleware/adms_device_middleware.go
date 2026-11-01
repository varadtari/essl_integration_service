package middleware

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"essl_integration_service/database"
	"essl_integration_service/logger"
	"essl_integration_service/models"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

const (
	ADMS_DEVICE_CONTEXT_KEY = "admsDevice"

	// ADMS_MAX_BODY_BYTES caps an upload. A full ATTLOG backlog from a Magnum (200k records) is
	// far larger, but devices send it in batches.
	ADMS_MAX_BODY_BYTES = 8 << 20

	// Raw-log rows keep at most this much body; bodyBytes records the real size.
	ADMS_RAW_BODY_LIMIT = 64 << 10

	// Unknown serials are refused; this caps how often one IP may try, so a scanner hitting
	// /iclock cannot fill the raw log.
	UNKNOWN_DEVICE_REQUESTS_PER_MINUTE = 30
)

// logHeartbeats keeps every empty poll in the raw log. Off by default: a device polls every
// ten seconds. Turn it on for protocol debugging with ADMS_LOG_HEARTBEATS=true.
var logHeartbeats = os.Getenv("ADMS_LOG_HEARTBEATS") == "true"

type captureWriter struct {
	gin.ResponseWriter
	body bytes.Buffer
}

func (w *captureWriter) Write(b []byte) (int, error) {
	w.body.Write(b)
	return w.ResponseWriter.Write(b)
}

type ipLimiter struct {
	mu      sync.Mutex
	windows map[string][]time.Time
}

var unknownDeviceLimiter = &ipLimiter{windows: map[string][]time.Time{}}

func (l *ipLimiter) allow(ip string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	recent := l.windows[ip][:0]
	for _, at := range l.windows[ip] {
		if now.Sub(at) < time.Minute {
			recent = append(recent, at)
		}
	}
	if len(recent) >= UNKNOWN_DEVICE_REQUESTS_PER_MINUTE {
		l.windows[ip] = recent
		return false
	}
	l.windows[ip] = append(recent, now)
	return true
}

// AuthorizeAdmsDevice admits /iclock requests from registered devices, keyed by the SN query
// parameter every ADMS request carries, and records each exchange in device_raw_messages.
//
// The serial is the only credential ADMS offers (the device's Comm Key applies to the LAN SDK,
// not to push), so registration in a gym is what authorises a device.
func AuthorizeAdmsDevice() gin.HandlerFunc {
	return func(c *gin.Context) {

		ctx := context.WithValue(c.Request.Context(), models.CONTEXT_REQUEST_ID_KEY, c.GetString("requestId"))
		serial := strings.TrimSpace(c.Query("SN"))
		remoteIp := c.ClientIP()

		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, ADMS_MAX_BODY_BYTES)
		body, err := io.ReadAll(c.Request.Body)
		if err != nil {
			c.String(http.StatusRequestEntityTooLarge, "body too large")
			c.Abort()
			return
		}
		c.Request.Body = io.NopCloser(bytes.NewReader(body))

		writer := &captureWriter{ResponseWriter: c.Writer}
		c.Writer = writer

		var device *models.AdmsDevice
		if serial != "" {
			found, appError := database.DevDb.GetAdmsDevice(ctx, serial)
			if appError != nil && !database.IsNotFound(appError) {
				c.String(http.StatusServiceUnavailable, "retry")
				c.Abort()
				return
			}
			device = found
		}

		if device == nil {
			if !unknownDeviceLimiter.allow(remoteIp, time.Now()) {
				c.String(http.StatusTooManyRequests, "slow down")
				c.Abort()
				return
			}
			logger.Log.Info("adms_unknown_device", zap.String("serial", serial), zap.String("ip", remoteIp), zap.String("path", c.Request.URL.Path))
			c.String(http.StatusUnauthorized, "Unknown device")
			c.Abort()
			saveRawMessage(c, nil, serial, body, writer)
			return
		}

		if appError := database.DevDb.TouchDevice(ctx, device.Id, remoteIp, c.Query("pushver")); appError != nil {
			logger.Log.Error("adms_touch_failed", zap.String("serial", serial), zap.String("error", appError.Message.ErrorMessage))
		}

		c.Set(ADMS_DEVICE_CONTEXT_KEY, *device)
		c.Next()

		isEmptyPoll := strings.HasSuffix(c.Request.URL.Path, "/getrequest") && strings.TrimSpace(writer.body.String()) == "OK"
		if logHeartbeats || !isEmptyPoll {
			saveRawMessage(c, &device.Id, device.SerialNumber, body, writer)
		}
	}
}

func saveRawMessage(c *gin.Context, deviceId *int, serial string, body []byte, writer *captureWriter) {

	stored := body
	if len(stored) > ADMS_RAW_BODY_LIMIT {
		stored = stored[:ADMS_RAW_BODY_LIMIT]
	}

	response := writer.body.String()
	if len(response) > ADMS_RAW_BODY_LIMIT {
		response = response[:ADMS_RAW_BODY_LIMIT]
	}

	message := models.RawDeviceMessage{
		DeviceId:     deviceId,
		SerialNumber: serial,
		Method:       c.Request.Method,
		Path:         c.Request.URL.Path,
		Query:        c.Request.URL.RawQuery,
		Body:         strings.ToValidUTF8(string(stored), "�"),
		BodyBytes:    len(body),
		Status:       c.Writer.Status(),
		Response:     response,
		RemoteIp:     c.ClientIP(),
	}

	// The device has its answer already; a failed debug write must not change it.
	if appError := database.RawDb.SaveRawMessage(context.WithoutCancel(c.Request.Context()), message); appError != nil {
		logger.Log.Error("adms_raw_log_failed", zap.String("error", appError.Message.ErrorMessage))
	}
}
