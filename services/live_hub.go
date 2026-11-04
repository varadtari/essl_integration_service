package services

import (
	"context"
	"encoding/json"
	"sync"

	"essl_integration_service/clients"
	"essl_integration_service/database"
	"essl_integration_service/logger"
	"essl_integration_service/models"

	"go.uber.org/zap"
)

// LIVE_SUBSCRIBER_BUFFER is how many events a slow tab may fall behind before it is told to
// refresh instead.
const LIVE_SUBSCRIBER_BUFFER = 64

// LiveSubscriber is one open stream. Events arrive on Events; NeedsRefresh is set when events
// were dropped because the stream fell behind.
type LiveSubscriber struct {
	gymId        int
	Events       chan models.LiveEvent
	mu           sync.Mutex
	needsRefresh bool
	closed       bool
}

// TakeRefresh reports (and clears) whether events were dropped for this subscriber.
func (s *LiveSubscriber) TakeRefresh() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	refresh := s.needsRefresh
	s.needsRefresh = false
	return refresh
}

func (s *LiveSubscriber) deliver(event models.LiveEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	select {
	case s.Events <- event:
	default:
		s.needsRefresh = true
	}
}

func (s *LiveSubscriber) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.closed {
		s.closed = true
		close(s.Events)
	}
}

// LiveHub fans events out to the open streams of each gym. Publishing never blocks: a full
// subscriber gets a refresh flag instead of holding up the others.
type LiveHub struct {
	mu   sync.RWMutex
	subs map[int]map[*LiveSubscriber]struct{}
}

func NewLiveHub() *LiveHub {
	return &LiveHub{subs: map[int]map[*LiveSubscriber]struct{}{}}
}

// Hub is the process-wide hub the stream endpoint subscribes to.
var Hub = NewLiveHub()

func (h *LiveHub) Subscribe(gymId int) (*LiveSubscriber, func()) {

	sub := &LiveSubscriber{gymId: gymId, Events: make(chan models.LiveEvent, LIVE_SUBSCRIBER_BUFFER)}

	h.mu.Lock()
	if h.subs[gymId] == nil {
		h.subs[gymId] = map[*LiveSubscriber]struct{}{}
	}
	h.subs[gymId][sub] = struct{}{}
	h.mu.Unlock()

	unsubscribe := func() {
		h.mu.Lock()
		delete(h.subs[gymId], sub)
		if len(h.subs[gymId]) == 0 {
			delete(h.subs, gymId)
		}
		h.mu.Unlock()
		sub.close()
	}

	return sub, unsubscribe
}

// Publish delivers an event to its gym's streams; a refresh with GymId 0 goes to every gym.
func (h *LiveHub) Publish(event models.LiveEvent) {

	h.mu.RLock()
	defer h.mu.RUnlock()

	if event.GymId == 0 && event.Type == models.LIVE_EVENT_REFRESH {
		for _, subs := range h.subs {
			for sub := range subs {
				sub.deliver(event)
			}
		}
		return
	}

	for sub := range h.subs[event.GymId] {
		sub.deliver(event)
	}
}

// Close ends every stream, so long-lived requests return promptly on shutdown.
func (h *LiveHub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for gymId, subs := range h.subs {
		for sub := range subs {
			sub.close()
		}
		delete(h.subs, gymId)
	}
}

func (h *LiveHub) SubscriberCount(gymId int) int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.subs[gymId])
}

// StartLiveEvents relays pg_notify events into Hub until ctx ends. After a reconnect every
// gym is told to refresh, since notifications sent while disconnected are lost.
func StartLiveEvents(ctx context.Context) {
	clients.Listen(ctx, database.BuildDatabaseUrl(), models.LIVE_EVENTS_CHANNEL,
		func(payload string) {
			var event models.LiveEvent
			if err := json.Unmarshal([]byte(payload), &event); err != nil {
				logger.Log.Error("live_event_bad_payload", zap.String("payload", payload), zap.Error(err))
				return
			}
			Hub.Publish(event)
		},
		func() { Hub.Publish(models.LiveEvent{Type: models.LIVE_EVENT_REFRESH}) },
	)
}
