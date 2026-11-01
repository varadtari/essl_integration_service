package test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"essl_integration_service/app"
	"essl_integration_service/database"
	"essl_integration_service/models"

	"github.com/jackc/pgx/v5"
)

// testDb is a direct connection for arranging and inspecting state; nil when no test database
// is reachable, in which case database-backed tests skip and the unit tests still run.
var testDb *pgx.Conn

const testApiKey = "test-api-key-for-internal-routes"

func TestMain(m *testing.M) {

	if len(models.JWT_SECRET) < models.MIN_JWT_SECRET_BYTES {
		models.JWT_SECRET = []byte("test-secret-that-is-long-enough-for-hs256")
	}
	models.COOKIE_SECURE = false

	// Tests TRUNCATE everything, so they only ever run against TEST_DB_NAME, never DB_NAME.
	if testDbName := os.Getenv("TEST_DB_NAME"); testDbName != "" {
		os.Setenv("DB_NAME", testDbName)
		if err := database.InitializeDatabasePool(); err == nil {
			if conn, err := pgx.Connect(context.Background(), database.BuildDatabaseUrl()); err == nil && database.PingDatabasePool() == nil {
				testDb = conn
			}
		}
	}

	app.SetupAllRoutes()

	code := m.Run()

	if testDb != nil {
		testDb.Close(context.Background())
	}
	database.CloseDatabasePool()
	os.Exit(code)
}

// requireDb skips a test when no test database is configured, and otherwise empties it.
func requireDb(t *testing.T) {
	t.Helper()
	if testDb == nil {
		t.Skip("TEST_DB_NAME not set or test database unreachable")
	}
	_, err := testDb.Exec(context.Background(), `TRUNCATE payments, memberships, members, plans, users, gyms, internal_errors, debug_activity_logs_api RESTART IDENTITY CASCADE`)
	if err != nil {
		t.Fatalf("truncate: %v", err)
	}
}

func exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	if _, err := testDb.Exec(context.Background(), sql, args...); err != nil {
		t.Fatalf("exec %q: %v", sql, err)
	}
}

type response struct {
	Code    int
	Body    map[string]any
	Cookies []*http.Cookie
}

// message returns the "message" field of a success response.
func (r response) message() any { return r.Body["message"] }

func (r response) messageMap() map[string]any {
	m, _ := r.Body["message"].(map[string]any)
	return m
}

func (r response) messageList() []any {
	l, _ := r.Body["message"].([]any)
	return l
}

type requestOption func(*http.Request)

func withCookie(cookie *http.Cookie) requestOption {
	return func(r *http.Request) {
		if cookie != nil {
			r.AddCookie(cookie)
		}
	}
}

func withApiKey(key string) requestOption {
	return func(r *http.Request) { r.Header.Set("x-api-key", key) }
}

func call(t *testing.T, method string, path string, body any, options ...requestOption) response {
	t.Helper()

	var reader *bytes.Reader
	if body == nil {
		reader = bytes.NewReader(nil)
	} else {
		raw, _ := json.Marshal(body)
		reader = bytes.NewReader(raw)
	}

	req := httptest.NewRequest(method, app.SERVICE_BASE_PATH+path, reader)
	req.Header.Set("Content-Type", "application/json")
	for _, option := range options {
		option(req)
	}

	rec := httptest.NewRecorder()
	app.Router.ServeHTTP(rec, req)

	parsed := map[string]any{}
	json.Unmarshal(rec.Body.Bytes(), &parsed)

	return response{Code: rec.Code, Body: parsed, Cookies: rec.Result().Cookies()}
}

func sessionCookie(cookies []*http.Cookie) *http.Cookie {
	for _, c := range cookies {
		if c.Name == models.SESSION_COOKIE_NAME {
			return c
		}
	}
	return nil
}

func login(t *testing.T, email string, password string) *http.Cookie {
	t.Helper()
	res := call(t, "POST", "/v1/auth/login", map[string]any{"email": email, "password": password})
	if res.Code != http.StatusOK {
		t.Fatalf("login %s: %d %v", email, res.Code, res.Body)
	}
	cookie := sessionCookie(res.Cookies)
	if cookie == nil {
		t.Fatalf("login %s: no session cookie", email)
	}
	return cookie
}

// createSuperadmin goes through the real internal route with a known key.
func createSuperadmin(t *testing.T, email string, password string) *http.Cookie {
	t.Helper()
	setApiKey(testApiKey)
	res := call(t, "POST", "/internal/v1/superadmins", map[string]any{"name": "Client Admin", "email": email, "password": password}, withApiKey(testApiKey))
	if res.Code != http.StatusCreated {
		t.Fatalf("create superadmin: %d %v", res.Code, res.Body)
	}
	return login(t, email, password)
}

// createGym onboards a gym as the superadmin and returns the owner's session and gym id.
func createGym(t *testing.T, admin *http.Cookie, name string, ownerEmail string) (*http.Cookie, int) {
	t.Helper()
	res := call(t, "POST", "/admin/v1/gyms", map[string]any{
		"name":  name,
		"owner": map[string]any{"name": name + " Owner", "email": ownerEmail, "password": "owner-password-1"},
	}, withCookie(admin))
	if res.Code != http.StatusCreated {
		t.Fatalf("create gym %s: %d %v", name, res.Code, res.Body)
	}
	gym := res.messageMap()["gym"].(map[string]any)
	return login(t, ownerEmail, "owner-password-1"), int(gym["id"].(float64))
}

// firstPlanId returns the id of the gym's plan with the given months (default plans exist).
func planIdForMonths(t *testing.T, owner *http.Cookie, months int) int {
	t.Helper()
	res := call(t, "GET", "/v1/plans", nil, withCookie(owner))
	for _, p := range res.messageList() {
		plan := p.(map[string]any)
		if int(plan["months"].(float64)) == months {
			return int(plan["id"].(float64))
		}
	}
	t.Fatalf("no %d-month plan", months)
	return 0
}

func addMember(t *testing.T, owner *http.Cookie, name string, phone string, planId int) map[string]any {
	t.Helper()
	res := call(t, "POST", "/v1/members", map[string]any{"name": name, "phone": phone, "planId": planId, "method": "upi"}, withCookie(owner))
	if res.Code != http.StatusCreated {
		t.Fatalf("add member %s: %d %v", name, res.Code, res.Body)
	}
	return res.messageMap()
}

func path(format string, args ...any) string { return fmt.Sprintf(format, args...) }
