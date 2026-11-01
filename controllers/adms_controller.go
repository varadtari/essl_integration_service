package controllers

import (
	"io"
	"net/http"
	"strings"

	"essl_integration_service/logger"
	"essl_integration_service/middleware"
	"essl_integration_service/models"
	"essl_integration_service/services"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// ADMS replies are plain text the device firmware parses, never JSON.
func admsReply(c *gin.Context, text string) {
	c.Data(http.StatusOK, "text/plain; charset=utf-8", []byte(text))
}

// admsFailure answers a device when storing its data failed. A non-OK status makes it keep the
// batch and retry after ErrorDelay, which is what we want: nothing is lost.
func admsFailure(c *gin.Context, apiError *models.ApiError) {
	c.Data(http.StatusServiceUnavailable, "text/plain; charset=utf-8", []byte("ERROR"))
	logger.Log.Error("adms_request_failed", zap.String("path", c.Request.URL.Path), zap.String("error", apiError.ApplicationError.Message.ErrorMessage))
}

// AdmsDispatch handles every /iclock request. One handler keeps unknown paths (newer firmwares
// also call /iclock/registry, /iclock/push and /iclock/ping) answered and logged instead of 404.
func AdmsDispatch(c *gin.Context) {

	ctx := getContextFromGinContext(c)
	device := c.MustGet(middleware.ADMS_DEVICE_CONTEXT_KEY).(models.AdmsDevice)
	path := strings.TrimSuffix(strings.TrimPrefix(c.Param("path"), "/"), ".aspx")

	switch {
	case path == "cdata" && c.Request.Method == http.MethodGet:
		// The handshake carries options=all; other GETs on cdata are time/keep-alive checks.
		if c.Query("options") != "" {
			admsReply(c, services.AdmsHandshake(ctx, device))
			return
		}
		admsReply(c, "OK")

	case path == "cdata" && c.Request.Method == http.MethodPost:
		body, _ := io.ReadAll(c.Request.Body)
		table := strings.ToUpper(c.Query("table"))
		stamp := c.Query("Stamp")

		var reply string
		var apiError *models.ApiError
		if table == "ATTLOG" {
			reply, apiError = services.AdmsIngestAttLog(ctx, device, stamp, string(body))
		} else {
			reply, apiError = services.AdmsAcceptUpload(ctx, device, table, stamp, string(body))
		}
		if apiError != nil {
			admsFailure(c, apiError)
			return
		}
		admsReply(c, reply)

	case path == "getrequest":
		reply, apiError := services.AdmsPoll(ctx, device)
		if apiError != nil {
			admsFailure(c, apiError)
			return
		}
		admsReply(c, reply)

	case path == "devicecmd":
		body, _ := io.ReadAll(c.Request.Body)
		reply, apiError := services.AdmsCommandResults(ctx, device, string(body))
		if apiError != nil {
			admsFailure(c, apiError)
			return
		}
		admsReply(c, reply)

	default:
		// fdata (photos), registry, push, ping, ...: accepted and kept in the raw log until the
		// spike shows what the Magnum needs from them.
		admsReply(c, "OK")
	}
}
