package test

import "essl_integration_service/middleware"

// setApiKey points the internal route group at a known key for the duration of the tests.
func setApiKey(key string) { middleware.SetApiKeyForTests(middleware.API_KEY, key) }
