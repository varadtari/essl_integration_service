package controllers

import (
	"encoding/json"
	"fmt"
	"net/http"

	"essl_integration_service/services"

	"github.com/gin-gonic/gin"
)

type sseWriter struct {
	c *gin.Context
}

func (w sseWriter) Event(name string, data any) error {
	payload, err := json.Marshal(data)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w.c.Writer, "event: %s\ndata: %s\n\n", name, payload); err != nil {
		return err
	}
	w.c.Writer.Flush()
	return nil
}

func (w sseWriter) Comment(text string) error {
	if _, err := fmt.Fprintf(w.c.Writer, ": %s\n\n", text); err != nil {
		return err
	}
	w.c.Writer.Flush()
	return nil
}

// StreamLiveEvents is the dashboard's Server-Sent Events stream of the gym's check-ins and
// device status.
func StreamLiveEvents(c *gin.Context) {

	ctx := getContextFromGinContext(c)

	header := c.Writer.Header()
	header.Set("Content-Type", "text/event-stream")
	header.Set("Cache-Control", "no-cache, no-transform")
	header.Set("Connection", "keep-alive")
	// Tells nginx-style proxies not to buffer the stream.
	header.Set("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)

	// A browser that loses the stream reconnects after this many milliseconds.
	fmt.Fprint(c.Writer, "retry: 5000\n\n")
	c.Writer.Flush()

	if apiError := services.ServeLiveEvents(ctx, getSession(c), sseWriter{c}); apiError != nil {
		sseWriter{c}.Event("error", map[string]any{"message": apiError.ApplicationError.Message.DisplayMessage})
	}
}
