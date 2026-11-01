package models

import (
	"os"
	"strconv"
	"time"

	_ "essl_integration_service/env"
)

const TIME_FORMAT_STR = "2006-01-02 15:04:05 -07:00"

const (
	MISC_ERROR                         = 1
	API_ERROR_NO_INTERVENTION_REQUIRED = 3
	ERROR_NO_INTERVENTION_REQUIRED     = 4
	ERROR_REQUIRE_INTERVENTION         = 5
	API_ERROR_REQUIRE_INTERVENTION     = 7
	DEVICE_ERROR_REQUIRE_INTERVENTION  = 10
)

const (
	CALLER_WEB_APP  = 1
	CALLER_DEVICE   = 2
	CALLER_INTERNAL = 3
)

var (
	ENV                         string
	WEB_ORIGIN                  string
	HTTP_CLIENT_TIMEOUT_SECONDS time.Duration

	// JWT_SECRET signs portal session tokens. StartApp refuses to run with a short one, so a
	// missing secret is a failed deploy rather than sessions anyone can forge.
	JWT_SECRET []byte

	// COOKIE_SECURE marks the session cookie Secure. Off only for local http development.
	COOKIE_SECURE bool

	// EXPOSE_ERROR_DETAILS adds errorMessage and additionalInfo to error responses. Local only.
	EXPOSE_ERROR_DETAILS bool
)

const DEFAULT_HTTP_CLIENT_TIMEOUT_SECONDS = 20

func init() {

	ENV = os.Getenv("ENV")

	// The portal is served from its own origin and calls the API with cookies, so CORS has
	// to name that origin - a wildcard is refused by browsers once credentials are included.
	WEB_ORIGIN = os.Getenv("WEB_ORIGIN")

	timeoutSecs, timeoutErr := strconv.Atoi(os.Getenv("HTTP_CLIENT_TIMEOUT_SECONDS"))
	if timeoutErr != nil || timeoutSecs <= 0 {
		timeoutSecs = DEFAULT_HTTP_CLIENT_TIMEOUT_SECONDS
	}
	HTTP_CLIENT_TIMEOUT_SECONDS = time.Duration(timeoutSecs)

	JWT_SECRET = []byte(os.Getenv("JWT_SECRET"))
	COOKIE_SECURE = os.Getenv("COOKIE_SECURE") != "false"
	EXPOSE_ERROR_DETAILS = os.Getenv("EXPOSE_ERROR_DETAILS") == "true"

}

const MIN_JWT_SECRET_BYTES = 32

const (
	SESSION_COOKIE_NAME = "najah_session"
	SESSION_TTL         = 7 * 24 * time.Hour
)

const (
	ROLE_SUPERADMIN = "superadmin"
	ROLE_OWNER      = "owner"
	ROLE_STAFF      = "staff"
)

const (
	PAYMENT_METHOD_CASH = "cash"
	PAYMENT_METHOD_UPI  = "upi"
)

const (
	MEMBER_STATUS_ACTIVE  = "active"
	MEMBER_STATUS_OVERDUE = "overdue"
)

const DEFAULT_GYM_TIMEZONE = "Asia/Kolkata"

const DATE_FORMAT = "2006-01-02"

type contextKey string

const (
	CONTEXT_REQUEST_ID_KEY contextKey = "requestId"
)
