package test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"essl_integration_service/database"
	"essl_integration_service/models"

	"github.com/stretchr/testify/assert"
)

func istToday() string {
	ist, _ := time.LoadLocation("Asia/Kolkata")
	return time.Now().In(ist).Format("2006-01-02")
}

func istDaysFromToday(days int) string {
	ist, _ := time.LoadLocation("Asia/Kolkata")
	return time.Now().In(ist).AddDate(0, 0, days).Format("2006-01-02")
}

func TestNewGymGetsDefaultPlans(t *testing.T) {
	requireDb(t)
	admin := createSuperadmin(t, "admin@najah.in", "admin-password-1")
	owner, _ := createGym(t, admin, "Panaji Iron Works", "owner@panaji.in")

	plans := call(t, "GET", "/v1/plans", nil, withCookie(owner)).messageList()

	assert.Len(t, plans, 4)
	first := plans[0].(map[string]any)
	assert.Equal(t, float64(1), first["months"])
	assert.Equal(t, float64(120000), first["pricePaise"], "₹1,200 stored as paise")
}

func TestAddMemberAllocatesPinsAndNormalisesPhone(t *testing.T) {
	requireDb(t)
	admin := createSuperadmin(t, "admin@najah.in", "admin-password-1")
	owner, _ := createGym(t, admin, "Panaji Iron Works", "owner@panaji.in")
	oneMonth := planIdForMonths(t, owner, 1)

	rohan := addMember(t, owner, "Rohan Naik", "+91 98220 11101", oneMonth)
	priya := addMember(t, owner, "Priya Kamat", "09545033303", oneMonth)

	assert.Equal(t, float64(1), rohan["devicePin"])
	assert.Equal(t, float64(2), priya["devicePin"])
	assert.Equal(t, "9822011101", rohan["phone"])
	assert.Equal(t, istToday(), rohan["startDate"])
	assert.Equal(t, "active", rohan["status"])

	bad := call(t, "POST", "/v1/members", map[string]any{"name": "X", "phone": "12345", "planId": oneMonth, "method": "cash"}, withCookie(owner))
	assert.Equal(t, http.StatusBadRequest, bad.Code)

	badMethod := call(t, "POST", "/v1/members", map[string]any{"name": "X", "phone": "9822011101", "planId": oneMonth, "method": "card"}, withCookie(owner))
	assert.Equal(t, http.StatusBadRequest, badMethod.Code)
}

func TestDeletedMembersPinIsNeverReused(t *testing.T) {
	requireDb(t)
	admin := createSuperadmin(t, "admin@najah.in", "admin-password-1")
	owner, _ := createGym(t, admin, "Panaji Iron Works", "owner@panaji.in")
	oneMonth := planIdForMonths(t, owner, 1)

	first := addMember(t, owner, "Rohan Naik", "9822011101", oneMonth)
	assert.Equal(t, http.StatusOK, call(t, "DELETE", path("/v1/members/%v", first["id"]), nil, withCookie(owner)).Code)

	next := addMember(t, owner, "Savio Fernandes", "9923045502", oneMonth)
	assert.Equal(t, float64(2), next["devicePin"])

	assert.Equal(t, http.StatusNotFound, call(t, "GET", path("/v1/members/%v", first["id"]), nil, withCookie(owner)).Code)
}

func TestRenewActiveMemberExtendsFromCurrentExpiry(t *testing.T) {
	requireDb(t)
	admin := createSuperadmin(t, "admin@najah.in", "admin-password-1")
	owner, _ := createGym(t, admin, "Panaji Iron Works", "owner@panaji.in")

	member := addMember(t, owner, "Rohan Naik", "9822011101", planIdForMonths(t, owner, 1))
	firstEnd := member["endDate"]

	res := call(t, "POST", path("/v1/members/%v/renew", member["id"]), map[string]any{"planId": planIdForMonths(t, owner, 3), "method": "cash"}, withCookie(owner))
	assert.Equal(t, http.StatusCreated, res.Code)
	assert.Equal(t, firstEnd, res.messageMap()["startDate"], "new term starts where the old one ends")

	detail := call(t, "GET", path("/v1/members/%v", member["id"]), nil, withCookie(owner)).messageMap()
	assert.Len(t, detail["memberships"], 2)
	assert.Len(t, detail["payments"], 2)
}

func TestOverdueMemberRenewalRestartsToday(t *testing.T) {
	requireDb(t)
	admin := createSuperadmin(t, "admin@najah.in", "admin-password-1")
	owner, _ := createGym(t, admin, "Panaji Iron Works", "owner@panaji.in")
	oneMonth := planIdForMonths(t, owner, 1)

	lapsed := addMember(t, owner, "Anjali Gaonkar", "9370012205", oneMonth)
	addMember(t, owner, "Tanvi Borkar", "9604055507", oneMonth)

	exec(t, `UPDATE memberships SET "startDate"=$2::date - 30, "endDate"=$2::date WHERE "memberId"=$1`, lapsed["id"], istDaysFromToday(-5))

	overdue := call(t, "GET", "/v1/overdue", nil, withCookie(owner)).messageList()
	assert.Len(t, overdue, 1)
	assert.Equal(t, "overdue", overdue[0].(map[string]any)["status"])
	assert.Equal(t, float64(-5), overdue[0].(map[string]any)["daysToExpiry"])

	summary := call(t, "GET", "/v1/dashboard/summary", nil, withCookie(owner)).messageMap()
	assert.Equal(t, float64(1), summary["activeMembers"])
	assert.Equal(t, float64(1), summary["overdueMembers"])
	assert.Equal(t, float64(240000), summary["revenueThisMonthPaise"])
	assert.Len(t, summary["recentPayments"], 2)

	renewed := call(t, "POST", path("/v1/members/%v/renew", lapsed["id"]), map[string]any{"planId": oneMonth, "method": "upi"}, withCookie(owner))
	assert.Equal(t, istToday(), renewed.messageMap()["startDate"], "an expired member restarts today, not from the old expiry")
	assert.Equal(t, "active", renewed.messageMap()["status"])

	assert.Len(t, call(t, "GET", "/v1/overdue", nil, withCookie(owner)).messageList(), 0)
}

func TestRenewalWithStaleExpiryIsRejected(t *testing.T) {
	requireDb(t)
	admin := createSuperadmin(t, "admin@najah.in", "admin-password-1")
	owner, gymId := createGym(t, admin, "Panaji Iron Works", "owner@panaji.in")
	oneMonth := planIdForMonths(t, owner, 1)
	member := addMember(t, owner, "Rohan Naik", "9822011101", oneMonth)

	stale := "2000-01-01"
	_, appError := database.MemDb.RenewMember(context.Background(), gymId, int(member["id"].(float64)), &stale,
		models.NewTerm{PlanId: oneMonth, StartDate: istToday(), EndDate: istDaysFromToday(30), AmountPaise: 1, Method: "cash"})

	assert.True(t, database.IsConflict(appError), "a renewal computed from an old expiry must not be applied")
}

func TestSearchAndPayments(t *testing.T) {
	requireDb(t)
	admin := createSuperadmin(t, "admin@najah.in", "admin-password-1")
	owner, _ := createGym(t, admin, "Panaji Iron Works", "owner@panaji.in")
	oneMonth := planIdForMonths(t, owner, 1)

	addMember(t, owner, "Rohan Naik", "9822011101", oneMonth)
	addMember(t, owner, "Priya Kamat", "9545033303", oneMonth)

	assert.Len(t, call(t, "GET", "/v1/members?search=priya", nil, withCookie(owner)).messageList(), 1)
	assert.Len(t, call(t, "GET", "/v1/members?search=98220", nil, withCookie(owner)).messageList(), 1)
	assert.Len(t, call(t, "GET", "/v1/members?search=%25", nil, withCookie(owner)).messageList(), 0, "% is matched literally")

	payments := call(t, "GET", "/v1/payments", nil, withCookie(owner)).messageMap()
	assert.Equal(t, float64(240000), payments["totalPaise"])
	assert.Len(t, payments["payments"], 2)

	assert.Equal(t, http.StatusBadRequest, call(t, "GET", "/v1/payments?month=2026-13", nil, withCookie(owner)).Code)
}
