package test

import (
	"net/http"
	"testing"

	"essl_integration_service/models"

	"github.com/stretchr/testify/assert"
)

func TestErrorResponsesHideInternalDetail(t *testing.T) {
	requireDb(t)
	admin := createSuperadmin(t, "admin@najah.in", "admin-password-1")
	createGym(t, admin, "Gym A", "owner@gyma.in")

	// A unique violation: the raw error names the constraint and table.
	res := call(t, "POST", "/admin/v1/gyms", map[string]any{"name": "Copy", "owner": map[string]any{"name": "x", "email": "owner@gyma.in", "password": "password-123"}}, withCookie(admin))

	assert.Equal(t, http.StatusConflict, res.Code)
	message := res.Body["message"].(map[string]any)
	assert.Equal(t, "A user with this email already exists", message["displayMessage"])
	assert.NotContains(t, message, "errorMessage")
	assert.NotContains(t, message, "additionalInfo")
	assert.NotEmpty(t, res.Body["requestId"], "requestId lets support find the full error in the logs")

	models.EXPOSE_ERROR_DETAILS = true
	defer func() { models.EXPOSE_ERROR_DETAILS = false }()

	res = call(t, "POST", "/admin/v1/gyms", map[string]any{"name": "Copy", "owner": map[string]any{"name": "x", "email": "owner@gyma.in", "password": "password-123"}}, withCookie(admin))
	assert.Contains(t, res.Body["message"].(map[string]any)["errorMessage"], "uq_users_email", "local development still sees the detail")
}
