package services

import (
	"testing"

	"essl_integration_service/models"

	"github.com/stretchr/testify/assert"
)

func TestHubDeliversOnlyToTheEventsGym(t *testing.T) {
	hub := NewLiveHub()
	a, unsubA := hub.Subscribe(1)
	b, unsubB := hub.Subscribe(2)
	defer unsubA()
	defer unsubB()

	hub.Publish(models.LiveEvent{Type: models.LIVE_EVENT_CHECKINS, GymId: 1, Ids: []int64{7}})

	assert.Equal(t, []int64{7}, (<-a.Events).Ids)
	assert.Len(t, b.Events, 0, "gym 2 hears nothing about gym 1")
}

func TestHubGlobalRefreshReachesEveryGym(t *testing.T) {
	hub := NewLiveHub()
	a, unsubA := hub.Subscribe(1)
	b, unsubB := hub.Subscribe(2)
	defer unsubA()
	defer unsubB()

	hub.Publish(models.LiveEvent{Type: models.LIVE_EVENT_REFRESH})

	assert.Equal(t, models.LIVE_EVENT_REFRESH, (<-a.Events).Type)
	assert.Equal(t, models.LIVE_EVENT_REFRESH, (<-b.Events).Type)
}

func TestHubFlagsASlowSubscriberInsteadOfBlocking(t *testing.T) {
	hub := NewLiveHub()
	slow, unsub := hub.Subscribe(1)
	defer unsub()

	for i := 0; i < LIVE_SUBSCRIBER_BUFFER+5; i++ {
		hub.Publish(models.LiveEvent{Type: models.LIVE_EVENT_CHECKINS, GymId: 1, Ids: []int64{int64(i)}})
	}

	assert.Len(t, slow.Events, LIVE_SUBSCRIBER_BUFFER)
	assert.True(t, slow.TakeRefresh(), "dropped events are reported")
	assert.False(t, slow.TakeRefresh(), "and the flag clears once taken")
}

func TestHubUnsubscribeAndCloseEndStreams(t *testing.T) {
	hub := NewLiveHub()
	a, unsubA := hub.Subscribe(1)
	unsubA()
	_, open := <-a.Events
	assert.False(t, open)
	assert.Equal(t, 0, hub.SubscriberCount(1))

	b, _ := hub.Subscribe(2)
	hub.Close()
	_, open = <-b.Events
	assert.False(t, open)

	// Publishing after close is harmless.
	hub.Publish(models.LiveEvent{Type: models.LIVE_EVENT_REFRESH})
}
