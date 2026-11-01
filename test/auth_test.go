package test

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestInternalRoutesNeedApiKey(t *testing.T) {
	requireDb(t)
	setApiKey(testApiKey)

	body := map[string]any{"name": "x", "email": "x@najah.in", "password": "password-123"}

	assert.Equal(t, http.StatusUnauthorized, call(t, "POST", "/internal/v1/superadmins", body).Code)
	assert.Equal(t, http.StatusUnauthorized, call(t, "POST", "/internal/v1/superadmins", body, withApiKey("wrong")).Code)
}

func TestLoginSetsHttpOnlySessionAndMeWorks(t *testing.T) {
	requireDb(t)

	admin := createSuperadmin(t, "Admin@Najah.in", "admin-password-1")

	assert.True(t, admin.HttpOnly, "session cookie must be HttpOnly")
	assert.Equal(t, http.SameSiteLaxMode, admin.SameSite)

	me := call(t, "GET", "/v1/me", nil, withCookie(admin))
	assert.Equal(t, http.StatusOK, me.Code)
	assert.Equal(t, "superadmin", me.messageMap()["role"])
	assert.Equal(t, "admin@najah.in", me.messageMap()["email"], "email is stored lower-cased")
	assert.Nil(t, me.messageMap()["gymId"])

	// Email matching is case-insensitive.
	login(t, "ADMIN@najah.in", "admin-password-1")
}

func TestLoginFailuresAreUniform(t *testing.T) {
	requireDb(t)
	createSuperadmin(t, "admin@najah.in", "admin-password-1")

	wrongPassword := call(t, "POST", "/v1/auth/login", map[string]any{"email": "admin@najah.in", "password": "nope"})
	unknownEmail := call(t, "POST", "/v1/auth/login", map[string]any{"email": "ghost@najah.in", "password": "nope"})

	assert.Equal(t, http.StatusUnauthorized, wrongPassword.Code)
	assert.Equal(t, http.StatusUnauthorized, unknownEmail.Code)
	assert.Equal(t,
		wrongPassword.Body["message"].(map[string]any)["displayMessage"],
		unknownEmail.Body["message"].(map[string]any)["displayMessage"],
		"an attacker must not learn which emails exist")
}

func TestRepeatedFailedLoginsAreThrottled(t *testing.T) {
	requireDb(t)
	createSuperadmin(t, "throttle@najah.in", "admin-password-1")

	for i := 0; i < 10; i++ {
		call(t, "POST", "/v1/auth/login", map[string]any{"email": "throttle@najah.in", "password": "wrong"})
	}

	res := call(t, "POST", "/v1/auth/login", map[string]any{"email": "throttle@najah.in", "password": "admin-password-1"})
	assert.Equal(t, http.StatusTooManyRequests, res.Code, "even the right password is refused while throttled")
}

func TestRequestsWithoutValidSessionAreRejected(t *testing.T) {
	requireDb(t)

	assert.Equal(t, http.StatusUnauthorized, call(t, "GET", "/v1/members", nil).Code)

	forged := &http.Cookie{Name: "najah_session", Value: "eyJhbGciOiJub25lIn0.eyJzdWIiOiIxIiwicm9sZSI6Im93bmVyIn0."}
	assert.Equal(t, http.StatusUnauthorized, call(t, "GET", "/v1/members", nil, withCookie(forged)).Code, "alg=none token")
}

func TestLogoutClearsCookie(t *testing.T) {
	requireDb(t)
	admin := createSuperadmin(t, "admin@najah.in", "admin-password-1")

	res := call(t, "POST", "/v1/auth/logout", nil, withCookie(admin))
	cleared := sessionCookie(res.Cookies)

	assert.Equal(t, http.StatusOK, res.Code)
	assert.NotNil(t, cleared)
	assert.True(t, cleared.MaxAge < 0)
}

func TestRolesAreSeparated(t *testing.T) {
	requireDb(t)
	admin := createSuperadmin(t, "admin@najah.in", "admin-password-1")
	owner, _ := createGym(t, admin, "Panaji Iron Works", "owner@panaji.in")

	assert.Equal(t, http.StatusForbidden, call(t, "GET", "/v1/members", nil, withCookie(admin)).Code, "superadmin has no gym to act on")
	assert.Equal(t, http.StatusForbidden, call(t, "GET", "/admin/v1/gyms", nil, withCookie(owner)).Code, "owner is not an admin")
}

func TestPasswordsNeverReachTheRequestLog(t *testing.T) {
	requireDb(t)
	createSuperadmin(t, "admin@najah.in", "admin-password-1")

	var leaked int
	err := testDb.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM debug_activity_logs_api WHERE "requestBody"::text LIKE '%admin-password-1%' OR "headers"::text LIKE '%najah_session=%'`).Scan(&leaked)

	assert.NoError(t, err)
	assert.Equal(t, 0, leaked)

	var logged int
	testDb.QueryRow(context.Background(), `SELECT COUNT(*) FROM debug_activity_logs_api WHERE "path"='/v1/auth/login' AND "requestBody"->>'password'='***'`).Scan(&logged)
	assert.Equal(t, 1, logged, "the login is still logged, with the password masked")
}
