package test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"essl_integration_service/app"
	"essl_integration_service/database"
	"essl_integration_service/devsim"
	"essl_integration_service/models"
	"essl_integration_service/services"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type sseEvent struct {
	Name string
	Data map[string]any
}

// openStream connects to the live stream as the cookie's user and returns its events.
func openStream(t *testing.T, server *httptest.Server, cookie *http.Cookie) <-chan sseEvent {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	req, _ := http.NewRequestWithContext(ctx, "GET", server.URL+app.SERVICE_BASE_PATH+"/v1/events/stream", nil)
	req.AddCookie(cookie)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "text/event-stream", resp.Header.Get("Content-Type"))

	events := make(chan sseEvent, 100)
	go func() {
		defer close(events)
		defer resp.Body.Close()
		scanner := bufio.NewScanner(resp.Body)
		var name string
		for scanner.Scan() {
			line := scanner.Text()
			switch {
			case strings.HasPrefix(line, "event: "):
				name = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				data := map[string]any{}
				json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &data)
				events <- sseEvent{Name: name, Data: data}
			}
		}
	}()

	ready := nextEvent(t, events, 3*time.Second)
	require.Equal(t, "ready", ready.Name)
	return events
}

func nextEvent(t *testing.T, events <-chan sseEvent, wait time.Duration) sseEvent {
	t.Helper()
	select {
	case e, ok := <-events:
		if !ok {
			return sseEvent{Name: "closed"}
		}
		return e
	case <-time.After(wait):
		t.Fatalf("no event within %s", wait)
		return sseEvent{}
	}
}

func assertNoEvent(t *testing.T, events <-chan sseEvent, wait time.Duration) {
	t.Helper()
	select {
	case e := <-events:
		t.Fatalf("unexpected event %s %v", e.Name, e.Data)
	case <-time.After(wait):
	}
}

// newLiveServer serves the app over real HTTP. Closing is a cleanup registered before any stream
// is opened: cleanups run last-in first-out, so streams disconnect first. httptest.Server.Close
// waits for open connections, and a live stream only ends when its client goes away.
func newLiveServer(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(app.Router)
	t.Cleanup(server.Close)
	return server
}

// startListener runs the pg_notify relay for one test and waits until it is listening.
func startListener(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go services.StartLiveEvents(ctx)

	probe, unsubscribe := services.Hub.Subscribe(-1)
	defer unsubscribe()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		database.LiveDb.Notify(context.Background(), models.LiveEvent{Type: models.LIVE_EVENT_CHECKINS, GymId: -1})
		select {
		case <-probe.Events:
			return
		case <-time.After(100 * time.Millisecond):
		}
	}
	t.Fatal("listener did not start")
}

func TestLiveCheckinsReachOnlyTheirGym(t *testing.T) {
	requireDb(t)
	startListener(t)
	server := newLiveServer(t)

	admin := createSuperadmin(t, "admin@najah.in", "admin-password-1")
	gymA := setupGymWithDevice(t, admin, "Gym A", "owner@gyma.in", "LIVEDEVA01")
	gymB := setupGymWithDevice(t, admin, "Gym B", "owner@gymb.in", "LIVEDEVB01")
	member := addMember(t, gymA.owner, "Rohan Naik", "9822011101", planIdForMonths(t, gymA.owner, 1))

	streamA := openStream(t, server, gymA.owner)
	streamB := openStream(t, server, gymB.owner)

	device := devsim.New(server.URL, "LIVEDEVA01")
	_, err := device.Handshake()
	require.NoError(t, err)
	assert.Equal(t, "device", nextEvent(t, streamA, 3*time.Second).Name, "first contact announces the device")

	require.NoError(t, device.Punch(fmt.Sprint(member["devicePin"]), devsim.VerifyFace))

	event := nextEvent(t, streamA, 3*time.Second)
	assert.Equal(t, "checkin", event.Name)
	assert.Equal(t, "Rohan Naik", event.Data["memberName"])
	assert.Equal(t, "allowed", event.Data["verdict"])
	assert.Equal(t, "face", event.Data["verifyMethod"])

	assertNoEvent(t, streamB, 500*time.Millisecond)

	// A punch the device sends twice is stored once, so it is announced once.
	at := time.Now().Add(-time.Hour)
	require.NoError(t, device.PunchAt(fmt.Sprint(member["devicePin"]), devsim.VerifyFace, at))
	assert.Equal(t, "checkin", nextEvent(t, streamA, 3*time.Second).Name)
	require.NoError(t, device.PunchAt(fmt.Sprint(member["devicePin"]), devsim.VerifyFace, at))
	assertNoEvent(t, streamA, 500*time.Millisecond)
}

func TestDeviceOnlineIsAnnouncedOnceAndBacklogsAsRefresh(t *testing.T) {
	requireDb(t)
	startListener(t)
	server := newLiveServer(t)

	admin := createSuperadmin(t, "admin@najah.in", "admin-password-1")
	gym := setupGymWithDevice(t, admin, "Gym A", "owner@gyma.in", "LIVEDEVA01")
	stream := openStream(t, server, gym.owner)

	device := devsim.New(server.URL, "LIVEDEVA01")
	_, err := device.Handshake()
	require.NoError(t, err)
	event := nextEvent(t, stream, 3*time.Second)
	assert.Equal(t, "device", event.Name)
	assert.Equal(t, float64(gym.deviceId), event.Data["deviceId"])

	_, err = device.Poll()
	require.NoError(t, err)
	assertNoEvent(t, stream, 500*time.Millisecond)

	// Silent past the online window, then back: announced again.
	exec(t, `UPDATE devices SET "lastSeenAt"=NOW() - INTERVAL '10 minutes'`)
	device.Poll()
	assert.Equal(t, "device", nextEvent(t, stream, 3*time.Second).Name)

	// An offline backlog larger than one notification arrives as a single refresh.
	device.SetOnline(false)
	start := time.Now().Add(-6 * time.Hour)
	for i := 0; i < models.MAX_IDS_PER_NOTIFY+50; i++ {
		device.PunchAt(fmt.Sprint(1000+i), devsim.VerifyFace, start.Add(time.Duration(i)*time.Minute))
	}
	device.SetOnline(true)
	require.NoError(t, device.Flush())
	assert.Equal(t, "refresh", nextEvent(t, stream, 3*time.Second).Name)
	assertNoEvent(t, stream, 300*time.Millisecond)
}

func TestDeactivatedStaffStreamIsSignedOut(t *testing.T) {
	requireDb(t)
	startListener(t)
	server := newLiveServer(t)

	previous := services.LIVE_HEARTBEAT
	services.LIVE_HEARTBEAT = 200 * time.Millisecond
	defer func() { services.LIVE_HEARTBEAT = previous }()

	admin := createSuperadmin(t, "admin@najah.in", "admin-password-1")
	owner, _ := createGym(t, admin, "Gym A", "owner@gyma.in")
	created := call(t, "POST", "/v1/staff", map[string]any{"name": "Desk", "email": "desk@gyma.in", "password": "desk-password-1"}, withCookie(owner))
	staff := login(t, "desk@gyma.in", "desk-password-1")

	stream := openStream(t, server, staff)
	call(t, "PATCH", path("/v1/staff/%v", created.messageMap()["id"]), map[string]any{"active": false}, withCookie(owner))

	assert.Equal(t, "signedout", nextEvent(t, stream, 3*time.Second).Name)
	assert.Equal(t, "closed", nextEvent(t, stream, 3*time.Second).Name)
}

func TestListenerReconnectTellsClientsToRefresh(t *testing.T) {
	requireDb(t)
	startListener(t)
	server := newLiveServer(t)

	admin := createSuperadmin(t, "admin@najah.in", "admin-password-1")
	owner, _ := createGym(t, admin, "Gym A", "owner@gyma.in")
	stream := openStream(t, server, owner)

	// Kill the listener's connection, as a database restart or network blip would.
	exec(t, `SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE query ILIKE 'LISTEN %' AND pid <> pg_backend_pid()`)

	assert.Equal(t, "refresh", nextEvent(t, stream, 5*time.Second).Name, "notifications may have been missed while reconnecting")
}

func TestSuperadminHasNoGymStream(t *testing.T) {
	requireDb(t)
	admin := createSuperadmin(t, "admin@najah.in", "admin-password-1")
	assert.Equal(t, http.StatusForbidden, call(t, "GET", "/v1/events/stream", nil, withCookie(admin)).Code)
}
