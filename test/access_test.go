package test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"essl_integration_service/devsim"
	"essl_integration_service/services"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// enforcingDevice connects a simulated terminal that refuses pins it was not given.
func enforcingDevice(t *testing.T, url string, serial string) *devsim.Device {
	t.Helper()
	device := devsim.New(url, serial)
	device.Enforce = true
	_, err := device.Handshake()
	require.NoError(t, err)
	return device
}

// poll delivers everything queued (a device polls every few seconds).
func poll(t *testing.T, device *devsim.Device) []string {
	t.Helper()
	var all []string
	for i := 0; i < 3; i++ {
		received, err := device.Poll()
		require.NoError(t, err)
		if len(received) == 0 {
			break
		}
		all = append(all, received...)
	}
	return all
}

func expire(t *testing.T, member map[string]any, daysAgo int) {
	t.Helper()
	exec(t, `UPDATE memberships SET "startDate"=$2::date - 30, "endDate"=$2::date WHERE "memberId"=$1`, member["id"], istDaysFromToday(-daysAgo))
}

func sweep() { services.SweepAllGyms(context.Background()) }

func pinOf(member map[string]any) string { return fmt.Sprint(member["devicePin"]) }

func accessOf(t *testing.T, owner *http.Cookie, name string) string {
	t.Helper()
	for _, m := range call(t, "GET", "/v1/members", nil, withCookie(owner)).messageList() {
		row := m.(map[string]any)
		if row["name"] == name {
			return row["access"].(map[string]any)["state"].(string)
		}
	}
	t.Fatalf("no member %s", name)
	return ""
}

func TestExpiredMemberIsRemovedFromTheDeviceAndRenewalRestoresThem(t *testing.T) {
	requireDb(t)
	server := newLiveServer(t)
	admin := createSuperadmin(t, "admin@najah.in", "admin-password-1")
	gym := setupGymWithDevice(t, admin, "Gym A", "owner@gyma.in", "ACCESSDEV1")
	oneMonth := planIdForMonths(t, gym.owner, 1)
	device := enforcingDevice(t, server.URL, "ACCESSDEV1")

	rohan := addMember(t, gym.owner, "Rohan Naik", "9822011101", oneMonth)
	priya := addMember(t, gym.owner, "Priya Kamat", "9545033303", oneMonth)

	// New members are put on the device straight away.
	received := poll(t, device)
	assert.Len(t, received, 2)
	assert.Contains(t, device.Users(), pinOf(rohan))
	assert.Contains(t, device.Users(), pinOf(priya))
	assert.Equal(t, "on_device", accessOf(t, gym.owner, "Priya Kamat"))

	// Priya enrols her face at the device; the template is uploaded and kept.
	require.NoError(t, device.Enroll(pinOf(priya)))
	assert.Equal(t, 1, countRows(t, `SELECT COUNT(*) FROM biometric_templates WHERE "memberId"=$1`, priya["id"]))
	require.NoError(t, device.Punch(pinOf(priya), devsim.VerifyFace))

	// Her membership ends; the next sweep removes her from the device.
	expire(t, priya, 1)
	sweep()
	assert.Equal(t, "syncing", accessOf(t, gym.owner, "Priya Kamat"), "until the device confirms")
	assert.Equal(t, []string{"DATA DELETE USERINFO PIN=" + pinOf(priya)}, poll(t, device))
	assert.NotContains(t, device.Users(), pinOf(priya))
	assert.Equal(t, "blocked", accessOf(t, gym.owner, "Priya Kamat"))

	// The door now refuses her, and still lets Rohan in.
	assert.ErrorIs(t, device.Punch(pinOf(priya), devsim.VerifyFace), devsim.ErrRefused)
	require.NoError(t, device.Punch(pinOf(rohan), devsim.VerifyFace))

	// Sweeping again sends nothing more.
	sweep()
	assert.Empty(t, poll(t, device))

	// She renews at the desk: back on the device at once, with her saved face.
	res := call(t, "POST", path("/v1/members/%v/renew", priya["id"]), map[string]any{"planId": oneMonth, "method": "cash"}, withCookie(gym.owner))
	require.Equal(t, http.StatusCreated, res.Code)
	received = poll(t, device)
	require.Len(t, received, 2)
	assert.True(t, strings.HasPrefix(received[0], "DATA UPDATE USERINFO PIN="+pinOf(priya)+"\t"))
	assert.True(t, strings.HasPrefix(received[1], "DATA UPDATE FACE PIN="+pinOf(priya)+"\t"))
	assert.Len(t, device.Templates(pinOf(priya)), 1, "no need to enrol again")
	require.NoError(t, device.Punch(pinOf(priya), devsim.VerifyFace))
	assert.Equal(t, "on_device", accessOf(t, gym.owner, "Priya Kamat"))
}

func TestRenewalWhileTheDeviceIsOfflineCancelsTheBlock(t *testing.T) {
	requireDb(t)
	server := newLiveServer(t)
	admin := createSuperadmin(t, "admin@najah.in", "admin-password-1")
	gym := setupGymWithDevice(t, admin, "Gym A", "owner@gyma.in", "ACCESSDEV1")
	oneMonth := planIdForMonths(t, gym.owner, 1)
	device := enforcingDevice(t, server.URL, "ACCESSDEV1")

	priya := addMember(t, gym.owner, "Priya Kamat", "9545033303", oneMonth)
	poll(t, device)

	// The device goes offline; her term ends and a delete is queued for it.
	device.SetOnline(false)
	expire(t, priya, 1)
	sweep()
	// Before the device is back, she renews.
	call(t, "POST", path("/v1/members/%v/renew", priya["id"]), map[string]any{"planId": oneMonth, "method": "upi"}, withCookie(gym.owner))

	device.SetOnline(true)
	received := poll(t, device)
	for _, cmd := range received {
		assert.NotContains(t, cmd, "DELETE", "the stale block must not reach the device")
	}
	assert.Contains(t, device.Users(), pinOf(priya))
	assert.Equal(t, 1, countRows(t, `SELECT COUNT(*) FROM device_commands WHERE "kind"='delete_user' AND "status"='superseded'`))
	assert.Equal(t, "on_device", accessOf(t, gym.owner, "Priya Kamat"))
}

func TestGraceDaysAndBlockOnExpiryAreRespected(t *testing.T) {
	requireDb(t)
	server := newLiveServer(t)
	admin := createSuperadmin(t, "admin@najah.in", "admin-password-1")
	gym := setupGymWithDevice(t, admin, "Gym A", "owner@gyma.in", "ACCESSDEV1")
	device := enforcingDevice(t, server.URL, "ACCESSDEV1")
	priya := addMember(t, gym.owner, "Priya Kamat", "9545033303", planIdForMonths(t, gym.owner, 1))
	poll(t, device)

	call(t, "PATCH", "/v1/gym", map[string]any{"graceDays": 3}, withCookie(gym.owner))
	expire(t, priya, 2)
	sweep()
	assert.Empty(t, poll(t, device), "two days late is inside a three-day grace")

	// Grace cut to zero: she is removed (the settings change reconciles straight away).
	call(t, "PATCH", "/v1/gym", map[string]any{"graceDays": 0}, withCookie(gym.owner))
	assert.Equal(t, []string{"DATA DELETE USERINFO PIN=" + pinOf(priya)}, poll(t, device))

	// The gym turns blocking off: everyone is let in again.
	call(t, "PATCH", "/v1/gym", map[string]any{"blockOnExpiry": false}, withCookie(gym.owner))
	poll(t, device)
	assert.Contains(t, device.Users(), pinOf(priya))
}

func TestDeletedMemberAndNewDevice(t *testing.T) {
	requireDb(t)
	server := newLiveServer(t)
	admin := createSuperadmin(t, "admin@najah.in", "admin-password-1")
	gym := setupGymWithDevice(t, admin, "Gym A", "owner@gyma.in", "ACCESSDEV1")
	oneMonth := planIdForMonths(t, gym.owner, 1)
	device := enforcingDevice(t, server.URL, "ACCESSDEV1")

	rohan := addMember(t, gym.owner, "Rohan Naik", "9822011101", oneMonth)
	priya := addMember(t, gym.owner, "Priya Kamat", "9545033303", oneMonth)
	savio := addMember(t, gym.owner, "Savio Fernandes", "9923045502", oneMonth)
	poll(t, device)
	expire(t, priya, 10)

	// Deleting a member removes them from the device.
	call(t, "DELETE", path("/v1/members/%v", rohan["id"]), nil, withCookie(gym.owner))
	assert.Contains(t, poll(t, device), "DATA DELETE USERINFO PIN="+pinOf(rohan))

	// A second device added later receives only members who should be let in.
	res := call(t, "POST", "/v1/devices", map[string]any{"serialNumber": "ACCESSDEV2", "name": "Back door"}, withCookie(gym.owner))
	require.Equal(t, http.StatusCreated, res.Code)
	second := enforcingDevice(t, server.URL, "ACCESSDEV2")
	poll(t, second)
	assert.Equal(t, map[string]string{pinOf(savio): "Savio Fernandes"}, second.Users())

	// "Sync members" resends everyone to a device, e.g. after a factory reset.
	sync := call(t, "POST", path("/v1/devices/%d/sync", gym.deviceId), nil, withCookie(gym.owner))
	assert.Equal(t, http.StatusOK, sync.Code)
	assert.Equal(t, float64(1), sync.messageMap()["commandsQueued"])
}

func TestADeviceThatKeepsRefusingShowsAnError(t *testing.T) {
	requireDb(t)
	server := newLiveServer(t)
	admin := createSuperadmin(t, "admin@najah.in", "admin-password-1")
	gym := setupGymWithDevice(t, admin, "Gym A", "owner@gyma.in", "ACCESSDEV1")
	device := enforcingDevice(t, server.URL, "ACCESSDEV1")
	device.OnCommand = func(id int64, command string) int { return -1002 }

	addMember(t, gym.owner, "Rohan Naik", "9822011101", planIdForMonths(t, gym.owner, 1))
	for i := 0; i < services.MAX_ACCESS_FAILURES+2; i++ {
		poll(t, device)
		sweep()
	}

	assert.Equal(t, "error", accessOf(t, gym.owner, "Rohan Naik"))
	assert.Equal(t, services.MAX_ACCESS_FAILURES, countRows(t, `SELECT COUNT(*) FROM device_commands WHERE "kind"='sync_user' AND "status"='failed'`), "retries stop at the limit")
}

func TestMemberNamesCannotInjectDeviceCommands(t *testing.T) {
	requireDb(t)
	server := newLiveServer(t)
	admin := createSuperadmin(t, "admin@najah.in", "admin-password-1")
	gym := setupGymWithDevice(t, admin, "Gym A", "owner@gyma.in", "ACCESSDEV1")
	device := enforcingDevice(t, server.URL, "ACCESSDEV1")

	addMember(t, gym.owner, "Evil\nC:999:REBOOT", "9822011101", planIdForMonths(t, gym.owner, 1))
	received := poll(t, device)

	require.Len(t, received, 1, "one command, not two")
	assert.Contains(t, received[0], "Name=Evil C:999:REBOOT\t")
	assert.NotContains(t, device.Executed(), "REBOOT")
}

func TestMembersOnlyReachTheirOwnGymsDevices(t *testing.T) {
	requireDb(t)
	server := newLiveServer(t)
	admin := createSuperadmin(t, "admin@najah.in", "admin-password-1")
	gymA := setupGymWithDevice(t, admin, "Gym A", "owner@gyma.in", "ACCESSDEVA")
	gymB := setupGymWithDevice(t, admin, "Gym B", "owner@gymb.in", "ACCESSDEVB")
	deviceB := enforcingDevice(t, server.URL, "ACCESSDEVB")

	addMember(t, gymA.owner, "Rohan Naik", "9822011101", planIdForMonths(t, gymA.owner, 1))
	sweep()

	assert.Empty(t, poll(t, deviceB))
	assert.Empty(t, deviceB.Users())
	assert.Empty(t, call(t, "GET", "/v1/members", nil, withCookie(gymB.owner)).messageList())
}
