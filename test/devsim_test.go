package test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"essl_integration_service/app"
	"essl_integration_service/devsim"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSimulatedDeviceEndToEnd drives the service over real HTTP with the device simulator.
func TestSimulatedDeviceEndToEnd(t *testing.T) {
	requireDb(t)
	server := httptest.NewServer(app.Router)
	defer server.Close()

	admin := createSuperadmin(t, "admin@najah.in", "admin-password-1")
	gym := setupGymWithDevice(t, admin, "Gym A", "owner@gyma.in", "DEVSIM0001")
	member := addMember(t, gym.owner, "Rohan Naik", "9822011101", planIdForMonths(t, gym.owner, 1))
	pin := fmt.Sprint(member["devicePin"])

	checkins := func() []any {
		return call(t, "GET", "/v1/checkins?limit=200", nil, withCookie(gym.owner)).messageMap()["checkins"].([]any)
	}

	device := devsim.New(server.URL, "DEVSIM0001")
	_, err := device.Handshake()
	require.NoError(t, err)

	// A live punch shows up straight away.
	require.NoError(t, device.Punch(pin, devsim.VerifyFace))
	require.Len(t, checkins(), 1)
	assert.Equal(t, "allowed", checkins()[0].(map[string]any)["verdict"])

	// The network drops; punches stay on the device.
	device.SetOnline(false)
	earlier := time.Now().Add(-30 * time.Minute)
	for i := 0; i < 3; i++ {
		require.NoError(t, device.PunchAt(pin, devsim.VerifyFingerprint, earlier.Add(time.Duration(i)*time.Minute)))
	}
	assert.Len(t, checkins(), 1, "nothing arrives while offline")
	assert.Equal(t, 3, device.Unsent())

	// Back online: the backlog arrives with its original times.
	device.SetOnline(true)
	require.NoError(t, device.Flush())
	assert.Equal(t, 0, device.Unsent())
	all := checkins()
	require.Len(t, all, 4)
	oldest, _ := time.Parse(time.RFC3339, all[3].(map[string]any)["punchedAt"].(string))
	assert.WithinDuration(t, earlier.Truncate(time.Second), oldest, time.Second)

	// Open the door from the portal; the next heartbeat delivers it and the device acknowledges.
	commandsPath := path("/v1/devices/%d/commands", gym.deviceId)
	require.Equal(t, http.StatusCreated, call(t, "POST", commandsPath, map[string]any{"kind": "open_door"}, withCookie(gym.owner)).Code)
	received, err := device.Poll()
	require.NoError(t, err)
	assert.Equal(t, []string{"CONTROL DEVICE 01010105"}, received)
	assert.Equal(t, "acked", call(t, "GET", commandsPath, nil, withCookie(gym.owner)).messageList()[0].(map[string]any)["status"])

	// Re-pulling the last hour re-sends punches the server already has: nothing is duplicated.
	from := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	require.Equal(t, http.StatusCreated, call(t, "POST", commandsPath, map[string]any{"kind": "repull_logs", "from": from}, withCookie(gym.owner)).Code)
	received, err = device.Poll()
	require.NoError(t, err)
	require.Len(t, received, 1)
	assert.True(t, strings.HasPrefix(received[0], "DATA QUERY ATTLOG StartTime="))
	assert.Len(t, checkins(), 4)
	assert.Equal(t, "acked", call(t, "GET", commandsPath, nil, withCookie(gym.owner)).messageList()[0].(map[string]any)["status"])

	// After a reboot the device resumes from the stamp the server hands back.
	rebooted := devsim.New(server.URL, "DEVSIM0001")
	options, err := rebooted.Handshake()
	require.NoError(t, err)
	assert.NotContains(t, options, "ATTLOGStamp=None")

	device2 := devsim.New(server.URL, "NOTREGISTERED2")
	_, err = device2.Handshake()
	assert.ErrorContains(t, err, "HTTP 401", "an unregistered device is refused")
}
