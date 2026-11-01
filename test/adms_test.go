package test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"essl_integration_service/app"

	"github.com/stretchr/testify/assert"
)

// deviceCall sends a request the way an ADMS device does: plain text, serial in ?SN=.
func deviceCall(t *testing.T, method string, path string, query string, body string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(method, path+"?"+query, strings.NewReader(body))
	req.Header.Set("Content-Type", "text/plain")
	rec := httptest.NewRecorder()
	app.Router.ServeHTTP(rec, req)
	text, _ := io.ReadAll(rec.Body)
	return rec.Code, string(text)
}

func istPunch(daysFromToday int, clock string) string {
	return istDaysFromToday(daysFromToday) + " " + clock
}

type gymWithDevice struct {
	owner    *http.Cookie
	gymId    int
	deviceId int
	serial   string
}

func setupGymWithDevice(t *testing.T, admin *http.Cookie, gymName string, ownerEmail string, serial string) gymWithDevice {
	t.Helper()
	owner, gymId := createGym(t, admin, gymName, ownerEmail)
	res := call(t, "POST", "/v1/devices", map[string]any{"serialNumber": serial, "name": "Front door"}, withCookie(owner))
	if res.Code != http.StatusCreated {
		t.Fatalf("create device: %d %v", res.Code, res.Body)
	}
	return gymWithDevice{owner: owner, gymId: gymId, deviceId: int(res.messageMap()["id"].(float64)), serial: serial}
}

func countRows(t *testing.T, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := testDb.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		t.Fatalf("count %q: %v", sql, err)
	}
	return n
}

func TestUnknownDeviceIsRefusedAndLogged(t *testing.T) {
	requireDb(t)

	code, _ := deviceCall(t, "GET", "/iclock/cdata", "SN=NOTREGISTERED1&options=all&pushver=2.4.1", "")

	assert.Equal(t, http.StatusUnauthorized, code)
	assert.Equal(t, 1, countRows(t, `SELECT COUNT(*) FROM device_raw_messages WHERE "serialNumber"='NOTREGISTERED1' AND "deviceId" IS NULL AND "status"=401`),
		"unknown serials are logged so an installer's typo can be spotted")

	// Registering a device with that serial then shows when it was last seen trying.
	admin := createSuperadmin(t, "admin@najah.in", "admin-password-1")
	owner, _ := createGym(t, admin, "Gym A", "owner@gyma.in")
	res := call(t, "POST", "/v1/devices", map[string]any{"serialNumber": " notregistered1 ", "name": "Front door"}, withCookie(owner))
	assert.Equal(t, http.StatusCreated, res.Code)
	assert.Equal(t, "NOTREGISTERED1", res.messageMap()["serialNumber"], "serials are normalised")
	assert.NotNil(t, res.messageMap()["serialLastAttemptAt"])
	assert.Equal(t, false, res.messageMap()["online"])
}

func TestHandshakeHeartbeatAndOnlineStatus(t *testing.T) {
	requireDb(t)
	admin := createSuperadmin(t, "admin@najah.in", "admin-password-1")
	gym := setupGymWithDevice(t, admin, "Gym A", "owner@gyma.in", "CQZ7224460014")

	code, options := deviceCall(t, "GET", "/iclock/cdata", "SN=cqz7224460014&options=all&pushver=2.4.1", "")
	assert.Equal(t, http.StatusOK, code)
	assert.Contains(t, options, "GET OPTION FROM: CQZ7224460014")
	assert.Contains(t, options, "ATTLOGStamp=None")

	code, reply := deviceCall(t, "GET", "/iclock/getrequest", "SN=CQZ7224460014", "")
	assert.Equal(t, http.StatusOK, code)
	assert.Equal(t, "OK", reply)

	device := call(t, "GET", path("/v1/devices/%d", gym.deviceId), nil, withCookie(gym.owner)).messageMap()
	assert.Equal(t, true, device["online"])
	assert.Equal(t, "2.4.1", device["pushVersion"])
	assert.Nil(t, device["serialLastAttemptAt"], "only shown for devices that never connected")

	// Empty polls are not kept in the raw log by default; the handshake is.
	assert.Equal(t, 0, countRows(t, `SELECT COUNT(*) FROM device_raw_messages WHERE "path"='/iclock/getrequest'`))
	assert.Equal(t, 1, countRows(t, `SELECT COUNT(*) FROM device_raw_messages WHERE "path"='/iclock/cdata'`))

	exec(t, `UPDATE devices SET "lastSeenAt"=NOW() - INTERVAL '5 minutes' WHERE id=$1`, gym.deviceId)
	assert.Equal(t, false, call(t, "GET", path("/v1/devices/%d", gym.deviceId), nil, withCookie(gym.owner)).messageMap()["online"])
}

func TestAttLogIngestIsIdempotentAndJudged(t *testing.T) {
	requireDb(t)
	admin := createSuperadmin(t, "admin@najah.in", "admin-password-1")
	gym := setupGymWithDevice(t, admin, "Gym A", "owner@gyma.in", "CQZ7224460014")
	oneMonth := planIdForMonths(t, gym.owner, 1)

	active := addMember(t, gym.owner, "Rohan Naik", "9822011101", oneMonth)
	lapsed := addMember(t, gym.owner, "Priya Kamat", "9545033303", oneMonth)
	exec(t, `UPDATE memberships SET "startDate"=$2::date - 30, "endDate"=$2::date WHERE "memberId"=$1`, lapsed["id"], istDaysFromToday(-5))

	body := fmt.Sprintf("%v\t%s\t0\t15\t0\t0\t0\n%v\t%s\t0\t1\t0\t0\t0\n999\t%s\t0\t2\t0\t0\t0\nbroken line\n",
		active["devicePin"], istPunch(0, "06:15:02"), lapsed["devicePin"], istPunch(0, "06:16:40"), istPunch(0, "06:17:00"))

	code, reply := deviceCall(t, "POST", "/iclock/cdata", "SN=CQZ7224460014&table=ATTLOG&Stamp=101", body)
	assert.Equal(t, http.StatusOK, code)
	assert.Equal(t, "OK: 4", reply, "every line is acknowledged, the broken one included")

	// The device did not see our OK and sends the same batch again.
	_, reply = deviceCall(t, "POST", "/iclock/cdata", "SN=CQZ7224460014&table=ATTLOG&Stamp=101", body)
	assert.Equal(t, "OK: 4", reply)
	assert.Equal(t, 3, countRows(t, `SELECT COUNT(*) FROM access_events`), "the resend adds nothing")

	page := call(t, "GET", "/v1/checkins", nil, withCookie(gym.owner)).messageMap()
	checkins := page["checkins"].([]any)
	assert.Len(t, checkins, 3)

	byPin := map[string]map[string]any{}
	for _, c := range checkins {
		row := c.(map[string]any)
		byPin[row["pin"].(string)] = row
	}
	activePin, lapsedPin := fmt.Sprint(active["devicePin"]), fmt.Sprint(lapsed["devicePin"])
	assert.Equal(t, "allowed", byPin[activePin]["verdict"])
	assert.Equal(t, "face", byPin[activePin]["verifyMethod"])
	assert.Equal(t, "Rohan Naik", byPin[activePin]["memberName"])
	assert.Equal(t, "allowed_but_expired", byPin[lapsedPin]["verdict"], "the device let in an expired member")
	assert.Equal(t, "fingerprint", byPin[lapsedPin]["verifyMethod"])
	assert.Equal(t, "unknown", byPin["999"]["verdict"])
	assert.Nil(t, byPin["999"]["memberName"])

	// 06:15:02 IST is 00:45:02 UTC.
	punched, _ := time.Parse(time.RFC3339, byPin[activePin]["punchedAt"].(string))
	assert.Equal(t, "00:45:02", punched.UTC().Format("15:04:05"))

	// The stamp is handed back on the next handshake so the device resumes after batch 101.
	_, options := deviceCall(t, "GET", "/iclock/cdata", "SN=CQZ7224460014&options=all", "")
	assert.Contains(t, options, "ATTLOGStamp=101")

	assert.Equal(t, 2, countRows(t, `SELECT COUNT(*) FROM internal_errors WHERE "errorMessage" LIKE '%unparseable ATTLOG%'`), "recorded once per upload: the batch was sent twice")

	summary := call(t, "GET", "/v1/dashboard/summary", nil, withCookie(gym.owner)).messageMap()
	assert.Equal(t, float64(3), summary["checkinsToday"])

	filtered := call(t, "GET", "/v1/checkins?verdict=allowed_but_expired", nil, withCookie(gym.owner)).messageMap()
	assert.Len(t, filtered["checkins"], 1)
}

func TestGraceDaysCountAsCovered(t *testing.T) {
	requireDb(t)
	admin := createSuperadmin(t, "admin@najah.in", "admin-password-1")
	gym := setupGymWithDevice(t, admin, "Gym A", "owner@gyma.in", "CQZ7224460014")
	call(t, "PATCH", "/v1/gym", map[string]any{"graceDays": 3}, withCookie(gym.owner))

	lapsed := addMember(t, gym.owner, "Priya Kamat", "9545033303", planIdForMonths(t, gym.owner, 1))
	exec(t, `UPDATE memberships SET "startDate"=$2::date - 30, "endDate"=$2::date WHERE "memberId"=$1`, lapsed["id"], istDaysFromToday(-2))

	deviceCall(t, "POST", "/iclock/cdata", "SN=CQZ7224460014&table=ATTLOG&Stamp=1", fmt.Sprintf("%v\t%s\t0\t15\n", lapsed["devicePin"], istPunch(0, "07:00:00")))

	checkins := call(t, "GET", "/v1/checkins", nil, withCookie(gym.owner)).messageMap()["checkins"].([]any)
	assert.Equal(t, "allowed", checkins[0].(map[string]any)["verdict"], "expired two days ago, inside a three-day grace")
}

func TestCommandRoundTrip(t *testing.T) {
	requireDb(t)
	admin := createSuperadmin(t, "admin@najah.in", "admin-password-1")
	gym := setupGymWithDevice(t, admin, "Gym A", "owner@gyma.in", "CQZ7224460014")
	commandsPath := path("/v1/devices/%d/commands", gym.deviceId)

	queued := call(t, "POST", commandsPath, map[string]any{"kind": "open_door"}, withCookie(gym.owner))
	assert.Equal(t, http.StatusCreated, queued.Code)
	commandId := int64(queued.messageMap()["id"].(float64))
	call(t, "POST", commandsPath, map[string]any{"kind": "info"}, withCookie(gym.owner))

	_, reply := deviceCall(t, "GET", "/iclock/getrequest", "SN=CQZ7224460014", "")
	assert.Equal(t, fmt.Sprintf("C:%d:CONTROL DEVICE 01010105\nC:%d:INFO\n", commandId, commandId+1), reply, "oldest first")

	_, reply = deviceCall(t, "GET", "/iclock/getrequest", "SN=CQZ7224460014", "")
	assert.Equal(t, "OK", reply, "a sent command is not handed out again straight away")

	_, reply = deviceCall(t, "POST", "/iclock/devicecmd", "SN=CQZ7224460014", fmt.Sprintf("ID=%d&Return=0&CMD=CONTROL\nID=%d&Return=-1002&CMD=INFO\n", commandId, commandId+1))
	assert.Equal(t, "OK", reply)

	history := call(t, "GET", commandsPath, nil, withCookie(gym.owner)).messageList()
	statuses := map[float64]string{}
	for _, h := range history {
		row := h.(map[string]any)
		statuses[row["id"].(float64)] = row["status"].(string)
	}
	assert.Equal(t, "acked", statuses[float64(commandId)])
	assert.Equal(t, "failed", statuses[float64(commandId+1)])

	// An unanswered command goes out again after the timeout.
	retry := int64(call(t, "POST", commandsPath, map[string]any{"kind": "check"}, withCookie(gym.owner)).messageMap()["id"].(float64))
	deviceCall(t, "GET", "/iclock/getrequest", "SN=CQZ7224460014", "")
	exec(t, `UPDATE device_commands SET "sentAt"=NOW() - INTERVAL '3 minutes' WHERE id=$1`, retry)
	_, reply = deviceCall(t, "GET", "/iclock/getrequest", "SN=CQZ7224460014", "")
	assert.Equal(t, fmt.Sprintf("C:%d:CHECK\n", retry), reply)
}

func TestStaffCanOpenTheDoorButNotReboot(t *testing.T) {
	requireDb(t)
	admin := createSuperadmin(t, "admin@najah.in", "admin-password-1")
	gym := setupGymWithDevice(t, admin, "Gym A", "owner@gyma.in", "CQZ7224460014")
	call(t, "POST", "/v1/staff", map[string]any{"name": "Desk", "email": "desk@gyma.in", "password": "desk-password-1"}, withCookie(gym.owner))
	staff := login(t, "desk@gyma.in", "desk-password-1")
	commandsPath := path("/v1/devices/%d/commands", gym.deviceId)

	assert.Equal(t, http.StatusCreated, call(t, "POST", commandsPath, map[string]any{"kind": "open_door"}, withCookie(staff)).Code)
	assert.Equal(t, http.StatusForbidden, call(t, "POST", commandsPath, map[string]any{"kind": "reboot"}, withCookie(staff)).Code)
	assert.Equal(t, http.StatusForbidden, call(t, "POST", "/v1/devices", map[string]any{"serialNumber": "ABCD1234", "name": "x"}, withCookie(staff)).Code)
	assert.Equal(t, http.StatusBadRequest, call(t, "POST", commandsPath, map[string]any{"kind": "repull_logs"}, withCookie(gym.owner)).Code, "repull needs a start time")
}

func TestDevicesAndCheckinsAreIsolatedBetweenGyms(t *testing.T) {
	requireDb(t)
	admin := createSuperadmin(t, "admin@najah.in", "admin-password-1")
	gymA := setupGymWithDevice(t, admin, "Gym A", "owner@gyma.in", "SERIALAAAA1")
	gymB := setupGymWithDevice(t, admin, "Gym B", "owner@gymb.in", "SERIALBBBB1")

	member := addMember(t, gymA.owner, "Rohan Naik", "9822011101", planIdForMonths(t, gymA.owner, 1))
	deviceCall(t, "POST", "/iclock/cdata", "SN=SERIALAAAA1&table=ATTLOG&Stamp=1", fmt.Sprintf("%v\t%s\t0\t15\n", member["devicePin"], istPunch(0, "06:00:00")))

	// Pin 1 at gym B's device is gym B's pin 1, never gym A's member.
	deviceCall(t, "POST", "/iclock/cdata", "SN=SERIALBBBB1&table=ATTLOG&Stamp=1", fmt.Sprintf("%v\t%s\t0\t15\n", member["devicePin"], istPunch(0, "06:05:00")))

	checkinsB := call(t, "GET", "/v1/checkins", nil, withCookie(gymB.owner)).messageMap()["checkins"].([]any)
	assert.Len(t, checkinsB, 1)
	assert.Equal(t, "unknown", checkinsB[0].(map[string]any)["verdict"])
	assert.Nil(t, checkinsB[0].(map[string]any)["memberName"])

	devicePathA := path("/v1/devices/%d", gymA.deviceId)
	assert.Equal(t, http.StatusNotFound, call(t, "GET", devicePathA, nil, withCookie(gymB.owner)).Code)
	assert.Equal(t, http.StatusNotFound, call(t, "PATCH", devicePathA, map[string]any{"name": "mine"}, withCookie(gymB.owner)).Code)
	assert.Equal(t, http.StatusNotFound, call(t, "DELETE", devicePathA, nil, withCookie(gymB.owner)).Code)
	assert.Equal(t, http.StatusNotFound, call(t, "POST", devicePathA+"/commands", map[string]any{"kind": "open_door"}, withCookie(gymB.owner)).Code)
	assert.Equal(t, http.StatusNotFound, call(t, "GET", devicePathA+"/commands", nil, withCookie(gymB.owner)).Code)
	assert.Len(t, call(t, "GET", "/v1/devices", nil, withCookie(gymB.owner)).messageList(), 1)

	// A serial already registered elsewhere cannot be claimed, whatever its case.
	dup := call(t, "POST", "/v1/devices", map[string]any{"serialNumber": "serialaaaa1", "name": "Stolen"}, withCookie(gymB.owner))
	assert.Equal(t, http.StatusConflict, dup.Code)
	assert.NotContains(t, dup.Body["message"].(map[string]any)["displayMessage"], "Gym A", "the other gym is not named")

	// A device cannot acknowledge another device's command.
	cmd := call(t, "POST", devicePathA+"/commands", map[string]any{"kind": "open_door"}, withCookie(gymA.owner)).messageMap()
	deviceCall(t, "POST", "/iclock/devicecmd", "SN=SERIALBBBB1", fmt.Sprintf("ID=%v&Return=0&CMD=CONTROL\n", cmd["id"]))
	assert.Equal(t, "queued", call(t, "GET", devicePathA+"/commands", nil, withCookie(gymA.owner)).messageList()[0].(map[string]any)["status"])
}

func TestRemovedDeviceIsRefusedAndItsQueueCancelled(t *testing.T) {
	requireDb(t)
	admin := createSuperadmin(t, "admin@najah.in", "admin-password-1")
	gym := setupGymWithDevice(t, admin, "Gym A", "owner@gyma.in", "CQZ7224460014")
	call(t, "POST", path("/v1/devices/%d/commands", gym.deviceId), map[string]any{"kind": "reboot"}, withCookie(gym.owner))

	assert.Equal(t, http.StatusOK, call(t, "DELETE", path("/v1/devices/%d", gym.deviceId), nil, withCookie(gym.owner)).Code)

	code, _ := deviceCall(t, "GET", "/iclock/getrequest", "SN=CQZ7224460014", "")
	assert.Equal(t, http.StatusUnauthorized, code)
	assert.Equal(t, 0, countRows(t, `SELECT COUNT(*) FROM device_commands WHERE "status" IN ('queued','sent')`))

	// The serial is free to register again, e.g. after moving the device.
	assert.Equal(t, http.StatusCreated, call(t, "POST", "/v1/devices", map[string]any{"serialNumber": "CQZ7224460014", "name": "Back door"}, withCookie(gym.owner)).Code)
}

func TestCheckinsPageWithCursor(t *testing.T) {
	requireDb(t)
	admin := createSuperadmin(t, "admin@najah.in", "admin-password-1")
	gym := setupGymWithDevice(t, admin, "Gym A", "owner@gyma.in", "CQZ7224460014")

	var lines []string
	for i := 0; i < 5; i++ {
		lines = append(lines, fmt.Sprintf("%d\t%s\t0\t15", 100+i, istPunch(0, fmt.Sprintf("06:0%d:00", i))))
	}
	deviceCall(t, "POST", "/iclock/cdata", "SN=CQZ7224460014&table=ATTLOG&Stamp=1", strings.Join(lines, "\n"))

	first := call(t, "GET", "/v1/checkins?limit=2", nil, withCookie(gym.owner)).messageMap()
	assert.Len(t, first["checkins"], 2)
	assert.Equal(t, "104", first["checkins"].([]any)[0].(map[string]any)["pin"], "newest first")

	second := call(t, "GET", "/v1/checkins?limit=2&before="+first["nextCursor"].(string), nil, withCookie(gym.owner)).messageMap()
	assert.Equal(t, "102", second["checkins"].([]any)[0].(map[string]any)["pin"])

	third := call(t, "GET", "/v1/checkins?limit=2&before="+second["nextCursor"].(string), nil, withCookie(gym.owner)).messageMap()
	assert.Len(t, third["checkins"], 1)
	assert.Nil(t, third["nextCursor"])

	assert.Equal(t, http.StatusBadRequest, call(t, "GET", "/v1/checkins?before=garbage", nil, withCookie(gym.owner)).Code)
}
