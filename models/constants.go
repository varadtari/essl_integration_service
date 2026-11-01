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

}

type contextKey string

const (
	CONTEXT_REQUEST_ID_KEY contextKey = "requestId"
)
