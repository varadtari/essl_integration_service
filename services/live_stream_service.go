package services

import (
	"context"
	"time"

	"essl_integration_service/database"
	"essl_integration_service/logger"
	"essl_integration_service/models"

	"go.uber.org/zap"
)

// LIVE_HEARTBEAT is how often an idle stream sends a keep-alive, which also keeps proxies from
// closing it and is when the session is re-checked. A variable so tests can shorten it.
var LIVE_HEARTBEAT = 25 * time.Second

// LiveWriter writes Server-Sent Events to one client.
type LiveWriter interface {
	Event(name string, data any) error
	Comment(text string) error
}

// ServeLiveEvents streams the gym's live events to one client until the client leaves, the
// service shuts down, or the session stops being valid. It returns when the stream should end.
func ServeLiveEvents(ctx context.Context, session models.Session, w LiveWriter) *models.ApiError {

	gymId, apiError := requireGym(ctx, session)
	if apiError != nil {
		return apiError
	}

	sub, unsubscribe := Hub.Subscribe(gymId)
	defer unsubscribe()

	if err := w.Event("ready", map[string]any{"gymId": gymId}); err != nil {
		return nil
	}

	heartbeat := time.NewTicker(LIVE_HEARTBEAT)
	defer heartbeat.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil

		case event, open := <-sub.Events:
			if !open {
				return nil
			}
			if err := writeLiveEvent(ctx, gymId, event, w); err != nil {
				return nil
			}

		case <-heartbeat.C:
			// A deactivated user's open tab stops receiving the gym's check-ins too.
			valid, apiError := SessionStillValid(ctx, session)
			if apiError == nil && !valid {
				w.Event("signedout", map[string]any{})
				return nil
			}
			if err := w.Comment("ping"); err != nil {
				return nil
			}
		}

		if sub.TakeRefresh() {
			if err := w.Event(models.LIVE_EVENT_REFRESH, map[string]any{}); err != nil {
				return nil
			}
		}
	}
}

func writeLiveEvent(ctx context.Context, gymId int, event models.LiveEvent, w LiveWriter) error {

	switch event.Type {
	case models.LIVE_EVENT_CHECKINS:
		checkins, appError := database.LiveDb.GetCheckinsByIds(ctx, gymId, event.Ids)
		if appError != nil {
			logger.Log.Error("live_checkins_load_failed", zap.String("error", appError.Message.ErrorMessage))
			return w.Event(models.LIVE_EVENT_REFRESH, map[string]any{})
		}
		for _, checkin := range checkins {
			if err := w.Event("checkin", checkin); err != nil {
				return err
			}
		}
		return nil

	case models.LIVE_EVENT_DEVICE:
		return w.Event("device", map[string]any{"deviceId": event.DeviceId})

	default:
		return w.Event(models.LIVE_EVENT_REFRESH, map[string]any{})
	}
}
