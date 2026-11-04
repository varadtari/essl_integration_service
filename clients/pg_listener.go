package clients

import (
	"context"
	"time"

	"essl_integration_service/logger"

	"github.com/jackc/pgx/v5"
	"go.uber.org/zap"
)

const (
	listenerMinBackoff = time.Second
	listenerMaxBackoff = 30 * time.Second
)

// Listen holds one dedicated connection that LISTENs on channel and calls onNotify with each
// payload, until ctx ends. When the connection drops it reconnects with backoff and calls
// onReconnect once listening again: anything notified in the gap was missed.
func Listen(ctx context.Context, databaseUrl string, channel string, onNotify func(payload string), onReconnect func()) {

	backoff := listenerMinBackoff
	connectedBefore := false

	for ctx.Err() == nil {
		err := listenOnce(ctx, databaseUrl, channel, onNotify, func() {
			backoff = listenerMinBackoff
			if connectedBefore {
				onReconnect()
			}
			connectedBefore = true
		})
		if ctx.Err() != nil {
			return
		}

		logger.Log.Warn("pg_listener_disconnected", zap.String("channel", channel), zap.Error(err), zap.Duration("retryIn", backoff))
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, listenerMaxBackoff)
	}
}

func listenOnce(ctx context.Context, databaseUrl string, channel string, onNotify func(string), onListening func()) error {

	conn, err := pgx.Connect(ctx, databaseUrl)
	if err != nil {
		return err
	}
	defer conn.Close(context.WithoutCancel(ctx))

	if _, err := conn.Exec(ctx, "LISTEN "+pgx.Identifier{channel}.Sanitize()); err != nil {
		return err
	}
	logger.Log.Info("pg_listener_listening", zap.String("channel", channel))
	onListening()

	for {
		notification, err := conn.WaitForNotification(ctx)
		if err != nil {
			return err
		}
		onNotify(notification.Payload)
	}
}
