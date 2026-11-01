package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"time"

	"essl_integration_service/database"
	"essl_integration_service/logger"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Request body fields that are never written to logs or debug_activity_logs_api. The template
// logs every POST/PATCH body verbatim; this service has a login endpoint, so it must not.
var redactedBodyFields = []string{"password", "currentPassword", "newPassword"}

func LogRequest() gin.HandlerFunc {

	return func(c *gin.Context) {

		var requestBodyString string

		startTime := time.Now()

		requestMethod := c.Request.Method

		if requestMethod == "POST" || requestMethod == "PATCH" || requestMethod == "PUT" {

			requestBody, err := io.ReadAll(c.Request.Body)
			if err != nil {
				logger.Log.Error("GetRawData returned error!", []zapcore.Field{zap.String("Error:", err.Error())}...)
			} else {
				c.Request.Body = io.NopCloser(bytes.NewBuffer(requestBody))
				requestBodyString = redactRequestBody(requestBody)
			}

		}

		c.Next()

		apiLatency := int64(time.Since(startTime) / time.Microsecond)

		fields := []zapcore.Field{
			zap.String("x-request-id", c.GetString("requestId")),
			zap.String("path", c.Request.URL.Path),
			zap.String("method", requestMethod),
			zap.Int("status", c.Writer.Status()),
			zap.String("user-agent", c.Request.UserAgent()),
			zap.Int64("latency", apiLatency),
		}

		if requestBodyString != "" {
			fields = append(fields, zap.String("body", requestBodyString))
		}

		logger.Log.Info("api_stats", fields...)

		if requestBodyString == "" {
			requestBodyString = "{}"
		}

		// Written with a context of its own: the request's context is already canceled when the
		// client hung up, and those are exactly the requests worth having in the log.
		appError := database.AlDb.SaveDebugApiActivityLog(
			context.WithoutCancel(c.Request.Context()),
			c.GetString("requestId"),
			requestMethod,
			c.Request.URL.Path,
			redactHeaders(c.Request.Header),
			requestBodyString,
			c.Writer.Status(),
			nil,
			apiLatency,
			c.Request.UserAgent(),
		)

		if appError != nil {
			logger.Log.Error("logging api activity to db failed", []zapcore.Field{zap.String("Error:", appError.Message.ErrorMessage)}...)
		}

	}

}

// redactRequestBody returns the body as a JSON string with secret fields masked. A body that is
// not a JSON object is replaced by "{}" because requestBody is a JSONB column.
func redactRequestBody(requestBody []byte) string {

	var body map[string]interface{}
	if err := json.Unmarshal(requestBody, &body); err != nil {
		return "{}"
	}

	for _, field := range redactedBodyFields {
		if _, ok := body[field]; ok {
			body[field] = "***"
		}
	}

	redacted, err := json.Marshal(body)
	if err != nil {
		return "{}"
	}

	return string(redacted)
}

// redactHeaders drops the session cookie and any Authorization header before the headers are
// stored, so a debug table read never hands out a working session.
func redactHeaders(headers map[string][]string) map[string][]string {

	redacted := make(map[string][]string, len(headers))
	for key, values := range headers {
		if key == "Cookie" || key == "Authorization" || key == "X-Api-Key" {
			redacted[key] = []string{"***"}
			continue
		}
		redacted[key] = values
	}

	return redacted
}
