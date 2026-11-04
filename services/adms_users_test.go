package services

import (
	"strings"
	"testing"

	"essl_integration_service/models"
	"essl_integration_service/utils"

	"github.com/stretchr/testify/assert"
)

func TestDeviceTextCannotInjectCommands(t *testing.T) {
	assert.Equal(t, "Rohan Naik C:99:REBOOT", deviceText("Rohan\tNaik\nC:99:REBOOT", 40))
	assert.Equal(t, "Anjali Gaonkar", deviceText("  Anjali   Gaonkar \r\n", 40))
	assert.Equal(t, "Joaquim D'Souza Fernande", deviceText("Joaquim D'Souza Fernandes-Pereira", DEVICE_NAME_MAX), "cut to what the device shows")
	assert.Equal(t, "ಕನ್ನಡ ಹೆಸರು", deviceText("ಕನ್ನಡ ಹೆಸರು", DEVICE_NAME_MAX), "non-Latin names are cut by character, not byte")
}

func TestUserInfoCommandIsAnOrdinaryUser(t *testing.T) {
	cmd := userInfoCommand(5, "Anjali\nGaonkar", nil)

	assert.Equal(t, "DATA UPDATE USERINFO PIN=5\tName=Anjali Gaonkar\tPri=0\tPasswd=\tCard=\tGrp=1\tTZ=0000000000000000", cmd)
	assert.NotContains(t, cmd, "\n")
	assert.Equal(t, "DATA DELETE USERINFO PIN=5", deleteUserCommand(5))
}

func TestParseOperLogTemplates(t *testing.T) {
	body := "USER PIN=5\tName=Anjali\tPri=0\n" +
		"FACE PIN=5\tFID=0\tSIZE=1648\tVALID=1\tTMP=AAAA\n" +
		"FP PIN=5\tFID=6\tSize=512\tValid=1\tTMP=BBBB\r\n" +
		"BIODATA Pin=7\tNo=0\tIndex=0\tValid=1\tDuress=0\tType=9\tMajorVer=58\tMinorVer=12\tFormat=0\tTmp=CCCC\n" +
		"BIOPHOTO PIN=7\tType=9\tSize=100\tContent=DDDD\n" +
		"USERPIC PIN=7\tSize=100\tContent=EEEE\n" +
		"OPLOG 4\t0\t2026-11-04 06:00:00\t0\t0\t0\t0\n" +
		"FACE FID=0\tTMP=no-pin\n"

	templates := parseOperLogTemplates(body)

	assert.Equal(t, []BiometricTemplate{
		{Kind: "FACE", Pin: "5", Slot: "0", Line: "FACE PIN=5\tFID=0\tSIZE=1648\tVALID=1\tTMP=AAAA"},
		{Kind: "FP", Pin: "5", Slot: "6", Line: "FP PIN=5\tFID=6\tSize=512\tValid=1\tTMP=BBBB"},
		{Kind: "BIODATA", Pin: "7", Slot: "9-0-0", Line: "BIODATA Pin=7\tNo=0\tIndex=0\tValid=1\tDuress=0\tType=9\tMajorVer=58\tMinorVer=12\tFormat=0\tTmp=CCCC"},
		{Kind: "BIOPHOTO", Pin: "7", Slot: "9", Line: "BIOPHOTO PIN=7\tType=9\tSize=100\tContent=DDDD"},
	}, templates, "USER, USERPIC, OPLOG and a template without a pin are not kept")
}

func gymWith(block bool, grace int) models.Gym {
	return models.Gym{Timezone: "Asia/Kolkata", BlockOnExpiry: block, GraceDays: grace}
}

func TestDesiredAccess(t *testing.T) {
	today := d("2026-11-10")
	member := func(end *string) models.AccessMember {
		return models.AccessMember{Id: 1, DevicePin: 1, Name: "x", EndDate: end}
	}

	assert.Equal(t, "active", desiredAccess(member(utils.Ptr("2026-11-10")), gymWith(true, 0), today), "last covered day")
	assert.Equal(t, "blocked", desiredAccess(member(utils.Ptr("2026-11-09")), gymWith(true, 0), today), "expired yesterday")
	assert.Equal(t, "active", desiredAccess(member(utils.Ptr("2026-11-08")), gymWith(true, 2), today), "inside grace")
	assert.Equal(t, "blocked", desiredAccess(member(utils.Ptr("2026-11-07")), gymWith(true, 2), today), "past grace")
	assert.Equal(t, "active", desiredAccess(member(utils.Ptr("2026-01-01")), gymWith(false, 0), today), "gym does not block")
	assert.Equal(t, "blocked", desiredAccess(member(nil), gymWith(true, 0), today), "no term")
	assert.Equal(t, "absent", desiredAccess(models.AccessMember{Deleted: true, EndDate: utils.Ptr("2027-01-01")}, gymWith(true, 0), today), "deleted")
}

func TestPlanAccess(t *testing.T) {
	member := models.AccessMember{Id: 1, DevicePin: 5, Name: "Anjali"}
	templates := []string{"FACE PIN=5\tFID=0\tTMP=AAAA"}

	// New member, never on the device: create them (and restore any saved templates).
	plan := planAccess(member, "active", nil, templates, nil)
	assert.Equal(t, "active", plan.TargetState)
	assert.Len(t, plan.Commands, 2)
	assert.Equal(t, "sync_user", plan.Commands[0].Kind)
	assert.Equal(t, "active", plan.Commands[0].TargetState)
	assert.Equal(t, "DATA UPDATE FACE PIN=5\tFID=0\tTMP=AAAA", plan.Commands[1].Command)
	assert.Empty(t, plan.Commands[1].TargetState)

	onDevice := &models.DeviceAccess{DesiredState: "active", ActualState: "active", PayloadHash: utils.Ptr(plan.PayloadHash)}

	// Already there with the same record: nothing to send.
	assert.Nil(t, planAccess(member, "active", onDevice, templates, nil))

	// Renamed: the user record is resent, templates are not (the device still has them).
	renamed := planAccess(models.AccessMember{Id: 1, DevicePin: 5, Name: "Anjali G"}, "active", onDevice, templates, nil)
	assert.Len(t, renamed.Commands, 1)
	assert.Equal(t, "sync_user", renamed.Commands[0].Kind)

	// Expired: removed from the device.
	blocked := planAccess(member, "blocked", onDevice, templates, nil)
	assert.Equal(t, "blocked", blocked.TargetState)
	assert.Equal(t, []models.PlannedCommand{{Kind: "delete_user", Command: "DATA DELETE USERINFO PIN=5", TargetState: "blocked"}}, blocked.Commands)

	// Expired and never on the device: record the state, send nothing.
	neverOn := planAccess(member, "blocked", nil, templates, nil)
	assert.Equal(t, "blocked", neverOn.TargetState)
	assert.Empty(t, neverOn.Commands)
	assert.Nil(t, planAccess(member, "blocked", &models.DeviceAccess{DesiredState: "blocked", ActualState: "blocked"}, templates, nil))

	// Deleted after being blocked: already off the device.
	gone := planAccess(member, "absent", &models.DeviceAccess{DesiredState: "blocked", ActualState: "blocked"}, templates, nil)
	assert.Empty(t, gone.Commands)

	// Expired while the device was offline (delete queued, not acknowledged), then renewed:
	// the user is sent again with templates, which supersedes the stale delete.
	pendingBlock := &models.DeviceAccess{DesiredState: "blocked", ActualState: "active", PayloadHash: utils.Ptr(plan.PayloadHash)}
	rescued := planAccess(member, "active", pendingBlock, templates, nil)
	assert.NotNil(t, rescued, "a renewal must not be skipped while a delete is pending")
	assert.Len(t, rescued.Commands, 2)

	// Renewed after a block: back on, templates replayed.
	renewed := planAccess(member, "active", &models.DeviceAccess{DesiredState: "blocked", ActualState: "blocked"}, templates, nil)
	assert.Len(t, renewed.Commands, 2)
}

func TestTermValidityCoversGraceInLocalTime(t *testing.T) {
	ADMS_SEND_VALIDITY = true
	defer func() { ADMS_SEND_VALIDITY = false }()

	window := termValidity(models.AccessMember{StartDate: utils.Ptr("2026-11-01"), EndDate: utils.Ptr("2026-12-01")}, gymWith(true, 2))
	cmd := userInfoCommand(5, "Anjali", window)

	assert.True(t, strings.HasSuffix(cmd, "\tStartDatetime=2026-11-01 00:00:00\tEndDatetime=2026-12-03 23:59:59"))
	assert.Nil(t, termValidity(models.AccessMember{}, gymWith(true, 0)))
}
