package services

import (
	"strings"
	"testing"
	"time"

	"essl_integration_service/models"
	"essl_integration_service/utils"

	"github.com/stretchr/testify/assert"
)

func TestParseAttLogFullAndShortLines(t *testing.T) {
	body := "1\t2026-11-01 06:15:02\t0\t15\t0\t0\t0\r\n" +
		"\n" +
		"42\t2026-11-01 06:16:40\n" +
		"7\t2026-11-01 06:17:00\t1\t1\n"

	records, rejected := parseAttLog(body)

	assert.Empty(t, rejected)
	assert.Len(t, records, 3)

	assert.Equal(t, "1", records[0].Pin)
	assert.Equal(t, "2026-11-01 06:15:02", records[0].LocalTime)
	assert.Equal(t, 0, *records[0].Status)
	assert.Equal(t, 15, *records[0].VerifyCode)
	assert.Equal(t, "1\t2026-11-01 06:15:02\t0\t15\t0\t0\t0", records[0].Raw, "CR is stripped from the stored line")

	assert.Nil(t, records[1].Status, "missing trailing fields are allowed")
	assert.Nil(t, records[1].VerifyCode)

	assert.Equal(t, 1, *records[2].VerifyCode)
}

func TestParseAttLogRejectsBadLines(t *testing.T) {
	body := "garbage-without-tabs\n" +
		"\t2026-11-01 06:15:02\t0\t15\n" +
		"5\t01/11/2026 06:15\t0\t15\n" +
		"5\t2026-11-01 06:15:02\t0\t15\n"

	records, rejected := parseAttLog(body)

	assert.Len(t, records, 1)
	assert.Len(t, rejected, 3)
}

func TestDeviceLocalTimeUsesGymZone(t *testing.T) {
	at, err := deviceLocalTime("2026-11-01 06:15:02", "Asia/Kolkata")

	assert.NoError(t, err)
	assert.Equal(t, "2026-11-01T00:45:02Z", at.UTC().Format(time.RFC3339))
}

func TestVerifyMethod(t *testing.T) {
	assert.Equal(t, "face", verifyMethod(utils.Ptr(15)))
	assert.Equal(t, "fingerprint", verifyMethod(utils.Ptr(1)))
	assert.Equal(t, "card", verifyMethod(utils.Ptr(2)))
	assert.Equal(t, "password", verifyMethod(utils.Ptr(0)))
	assert.Equal(t, "other", verifyMethod(utils.Ptr(99)))
	assert.Equal(t, "unknown", verifyMethod(nil))
}

func TestParseDeviceCmdResults(t *testing.T) {
	body := "ID=12&Return=0&CMD=CONTROL\r\nID=13&Return=-1002&CMD=DATA\nnonsense\nID=x&Return=0\n"

	results := parseDeviceCmdResults(body)

	assert.Equal(t, []models.DeviceCommandResult{{Id: 12, Return: 0, Cmd: "CONTROL"}, {Id: 13, Return: -1002, Cmd: "DATA"}}, results)
}

func TestHandshakeOptionsCarryStamps(t *testing.T) {
	fresh := renderHandshakeOptions(models.AdmsDevice{SerialNumber: "CQZ7224460014"}, 10)

	assert.True(t, strings.HasPrefix(fresh, "GET OPTION FROM: CQZ7224460014\n"))
	assert.Contains(t, fresh, "ATTLOGStamp=None\n")
	assert.Contains(t, fresh, "Delay=10\n")
	assert.Contains(t, fresh, "Realtime=1\n")
	assert.NotContains(t, fresh, "TimeZone=", "see the note on renderHandshakeOptions")

	resumed := renderHandshakeOptions(models.AdmsDevice{SerialNumber: "X", AttStamp: utils.Ptr("9876"), OpStamp: utils.Ptr("12")}, 10)
	assert.Contains(t, resumed, "ATTLOGStamp=9876\n")
	assert.Contains(t, resumed, "OPERLOGStamp=12\n")
}

func TestRenderCommands(t *testing.T) {
	assert.Equal(t, "OK", renderCommands(nil))
	assert.Equal(t, "C:5:REBOOT\nC:6:INFO\n", renderCommands([]models.PendingCommand{{Id: 5, Command: "REBOOT"}, {Id: 6, Command: "INFO"}}))
}

func TestCommandText(t *testing.T) {
	now := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)
	from := now.Add(-2 * time.Hour)

	repull, err := commandText(models.COMMAND_REPULL_LOGS, now, "Asia/Kolkata", &from)
	assert.NoError(t, err)
	assert.Equal(t, "DATA QUERY ATTLOG StartTime=2026-11-01 13:30:00\tEndTime=2026-11-01 15:30:00", repull, "times in the device's local zone")

	_, err = commandText(models.COMMAND_REPULL_LOGS, now, "Asia/Kolkata", nil)
	assert.Error(t, err)

	open, _ := commandText(models.COMMAND_OPEN_DOOR, now, "Asia/Kolkata", nil)
	assert.Equal(t, "CONTROL DEVICE 01010105", open)

	_, err = commandText("explode", now, "Asia/Kolkata", nil)
	assert.Error(t, err)
}
