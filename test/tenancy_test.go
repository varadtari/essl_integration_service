package test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestGymsCannotSeeEachOther is the guard on multi-tenancy: every member and plan endpoint is
// tried with the other gym's session and must behave as if the record does not exist.
func TestGymsCannotSeeEachOther(t *testing.T) {
	requireDb(t)
	admin := createSuperadmin(t, "admin@najah.in", "admin-password-1")
	ownerA, _ := createGym(t, admin, "Gym A", "owner@gyma.in")
	ownerB, _ := createGym(t, admin, "Gym B", "owner@gymb.in")

	planA := planIdForMonths(t, ownerA, 1)
	memberA := addMember(t, ownerA, "Rohan Naik", "9822011101", planA)
	planB := planIdForMonths(t, ownerB, 1)
	addMember(t, ownerB, "Savio Fernandes", "9923045502", planB)

	memberPath := path("/v1/members/%v", memberA["id"])

	assert.Equal(t, http.StatusNotFound, call(t, "GET", memberPath, nil, withCookie(ownerB)).Code)
	assert.Equal(t, http.StatusNotFound, call(t, "PATCH", memberPath, map[string]any{"name": "Hijacked"}, withCookie(ownerB)).Code)
	assert.Equal(t, http.StatusNotFound, call(t, "DELETE", memberPath, nil, withCookie(ownerB)).Code)
	assert.Equal(t, http.StatusNotFound, call(t, "POST", memberPath+"/renew", map[string]any{"planId": planB, "method": "cash"}, withCookie(ownerB)).Code)

	// B cannot sell A's plan, even to its own new member.
	assert.Equal(t, http.StatusNotFound, call(t, "POST", "/v1/members", map[string]any{"name": "X", "phone": "9000000001", "planId": planA, "method": "cash"}, withCookie(ownerB)).Code)
	assert.Equal(t, http.StatusNotFound, call(t, "PATCH", path("/v1/plans/%d", planA), map[string]any{"pricePaise": 1}, withCookie(ownerB)).Code)

	listB := call(t, "GET", "/v1/members", nil, withCookie(ownerB)).messageList()
	assert.Len(t, listB, 1)
	assert.Equal(t, "Savio Fernandes", listB[0].(map[string]any)["name"])

	paymentsB := call(t, "GET", "/v1/payments", nil, withCookie(ownerB)).messageMap()
	assert.Len(t, paymentsB["payments"], 1)

	// Each gym numbers its own device pins from 1.
	assert.Equal(t, float64(1), memberA["devicePin"])

	// A's member is untouched.
	assert.Equal(t, "Rohan Naik", call(t, "GET", memberPath, nil, withCookie(ownerA)).messageMap()["name"])
}

func TestStaffPermissionsAndDeactivation(t *testing.T) {
	requireDb(t)
	admin := createSuperadmin(t, "admin@najah.in", "admin-password-1")
	owner, _ := createGym(t, admin, "Panaji Iron Works", "owner@panaji.in")

	created := call(t, "POST", "/v1/staff", map[string]any{"name": "Front Desk", "email": "desk@panaji.in", "password": "desk-password-1"}, withCookie(owner))
	assert.Equal(t, http.StatusCreated, created.Code)
	staff := login(t, "desk@panaji.in", "desk-password-1")

	member := addMember(t, staff, "Rohan Naik", "9822011101", planIdForMonths(t, staff, 1))

	assert.Equal(t, http.StatusForbidden, call(t, "POST", "/v1/plans", map[string]any{"name": "Day pass", "months": 1, "pricePaise": 100}, withCookie(staff)).Code)
	assert.Equal(t, http.StatusForbidden, call(t, "DELETE", path("/v1/members/%v", member["id"]), nil, withCookie(staff)).Code)
	assert.Equal(t, http.StatusForbidden, call(t, "PATCH", "/v1/gym", map[string]any{"graceDays": 3}, withCookie(staff)).Code)
	assert.Equal(t, http.StatusForbidden, call(t, "POST", "/v1/staff", map[string]any{"name": "x", "email": "x@x.in", "password": "password-123"}, withCookie(staff)).Code)

	staffId := created.messageMap()["id"]
	assert.Equal(t, http.StatusOK, call(t, "PATCH", path("/v1/staff/%v", staffId), map[string]any{"active": false}, withCookie(owner)).Code)

	assert.Equal(t, http.StatusUnauthorized, call(t, "GET", "/v1/members", nil, withCookie(staff)).Code, "a deactivated user's existing session stops working")
	assert.Equal(t, http.StatusUnauthorized, call(t, "POST", "/v1/auth/login", map[string]any{"email": "desk@panaji.in", "password": "desk-password-1"}).Code)
}

func TestOwnerSettingsAndAdminBilling(t *testing.T) {
	requireDb(t)
	admin := createSuperadmin(t, "admin@najah.in", "admin-password-1")
	owner, gymId := createGym(t, admin, "Panaji Iron Works", "owner@panaji.in")

	updated := call(t, "PATCH", "/v1/gym", map[string]any{"graceDays": 3, "blockOnExpiry": false}, withCookie(owner))
	assert.Equal(t, http.StatusOK, updated.Code)
	assert.Equal(t, float64(3), updated.messageMap()["graceDays"])
	assert.Equal(t, false, updated.messageMap()["blockOnExpiry"])

	assert.Equal(t, http.StatusBadRequest, call(t, "PATCH", "/v1/gym", map[string]any{"timezone": "Mars/Base"}, withCookie(owner)).Code)

	// billingStatus is not an owner field: it is ignored, not applied.
	call(t, "PATCH", "/v1/gym", map[string]any{"billingStatus": "paid"}, withCookie(owner))
	assert.Equal(t, "trial", call(t, "GET", "/v1/gym", nil, withCookie(owner)).messageMap()["billingStatus"])

	paid := call(t, "PATCH", path("/admin/v1/gyms/%d", gymId), map[string]any{"billingStatus": "paid"}, withCookie(admin))
	assert.Equal(t, "paid", paid.messageMap()["billingStatus"])

	gyms := call(t, "GET", "/admin/v1/gyms", nil, withCookie(admin)).messageList()
	assert.Len(t, gyms, 1)
	assert.Equal(t, "owner@panaji.in", gyms[0].(map[string]any)["ownerEmail"])

	dup := call(t, "POST", "/admin/v1/gyms", map[string]any{"name": "Copy", "owner": map[string]any{"name": "x", "email": "OWNER@panaji.in", "password": "password-123"}}, withCookie(admin))
	assert.Equal(t, http.StatusConflict, dup.Code, "owner email already in use")
	assert.Len(t, call(t, "GET", "/admin/v1/gyms", nil, withCookie(admin)).messageList(), 1, "the failed onboarding left no orphan gym")
}
