package services

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"essl_integration_service/database"
	"essl_integration_service/logger"
	"essl_integration_service/misc"
	"essl_integration_service/models"
	"essl_integration_service/utils"

	"go.uber.org/zap"
)

const (
	ADMS_SERVICE_BYTE3_VALUE = 0x00270000 //2 = services, 7 = adms
)

var (
	ADMS_SERVICE_ERROR_BASE_CODE int
)

func init() {
	ADMS_SERVICE_ERROR_BASE_CODE = utils.ServiceErrorBaseCode + ADMS_SERVICE_BYTE3_VALUE
}

const (
	// ADMS_HEARTBEAT_SECONDS is the Delay handed to devices: how often they poll for commands.
	// It bounds how long "open door" or a block takes to land.
	ADMS_HEARTBEAT_SECONDS = 10

	// A command sent but not answered is handed out again after this long, at most
	// COMMAND_MAX_ATTEMPTS times in all.
	COMMAND_RESEND_AFTER = 2 * time.Minute
	COMMAND_MAX_ATTEMPTS = 5

	COMMANDS_PER_POLL = 10
)

// AdmsHandshake answers GET /iclock/cdata?options=all.
func AdmsHandshake(ctx context.Context, device models.AdmsDevice) string {
	return renderHandshakeOptions(device, ADMS_HEARTBEAT_SECONDS)
}

// judgePunch decides a punch's verdict from the member's terms and the calendar day of the
// punch at the gym. A term covers its endDate plus the gym's grace days.
func judgePunch(member *models.MemberTerms, punchDay time.Time, graceDays int) string {

	if member == nil {
		return models.VERDICT_UNKNOWN
	}

	for _, term := range member.Terms {
		start, errStart := parseDate(term[0])
		end, errEnd := parseDate(term[1])
		if errStart != nil || errEnd != nil {
			continue
		}
		if !punchDay.Before(start) && !punchDay.After(end.AddDate(0, 0, graceDays)) {
			return models.VERDICT_ALLOWED
		}
	}

	// The device opened for someone with no covering term: it has not yet been told to block
	// them (or the punch predates their first term).
	return models.VERDICT_ALLOWED_BUT_EXPIRED
}

// AdmsIngestAttLog stores an ATTLOG upload and returns the reply the device expects. The stamp is
// saved only after the punches are stored, so a failure makes the device re-send the batch.
func AdmsIngestAttLog(ctx context.Context, device models.AdmsDevice, stamp string, body string) (string, *models.ApiError) {

	records, rejected := parseAttLog(body)

	if len(rejected) > 0 {
		errMsg := fmt.Sprintf("AdmsIngestAttLog: device %s sent %d unparseable ATTLOG lines", device.SerialNumber, len(rejected))
		logger.Log.Warn(errMsg, zap.Strings("lines", rejected))
		misc.ProcessError(ctx, models.DEVICE_ERROR_REQUIRE_INTERVENTION, errMsg, []byte(fmt.Sprint(rejected)))
	}

	pins := []int{}
	for _, record := range records {
		if pin, err := strconv.Atoi(record.Pin); err == nil {
			pins = append(pins, pin)
		}
	}

	terms, appError := database.EvDb.MemberTermsForPins(ctx, device.GymId, pins)
	if appError != nil {
		return "", fromDaoError(ctx, "AdmsIngestAttLog", appError)
	}

	events := make([]models.NewAccessEvent, 0, len(records))
	for _, record := range records {

		punchedAt, err := deviceLocalTime(record.LocalTime, device.Timezone)
		if err != nil {
			continue
		}
		punchDay := civilDate(punchedAt.In(gymLocation(device.Timezone)).Date())

		var member *models.MemberTerms
		if pin, err := strconv.Atoi(record.Pin); err == nil {
			if found, ok := terms[pin]; ok {
				member = &found
			}
		}

		event := models.NewAccessEvent{
			Pin:          record.Pin,
			PunchedAt:    punchedAt,
			PunchStatus:  record.Status,
			VerifyCode:   record.VerifyCode,
			VerifyMethod: verifyMethod(record.VerifyCode),
			Verdict:      judgePunch(member, punchDay, device.GraceDays),
			Raw:          record.Raw,
		}
		if member != nil {
			event.MemberId = utils.Ptr(member.MemberId)
		}
		events = append(events, event)
	}

	insertedIds, appError := database.EvDb.InsertEvents(ctx, device.GymId, device.Id, events)
	if appError != nil {
		return "", fromDaoError(ctx, "AdmsIngestAttLog", appError)
	}

	if appError := database.DevDb.SaveStamp(ctx, device.Id, "ATTLOG", stamp); appError != nil {
		return "", fromDaoError(ctx, "AdmsIngestAttLog", appError)
	}

	logger.Log.Info("adms_attlog", zap.String("serial", device.SerialNumber), zap.Int("lines", len(records)+len(rejected)),
		zap.Int("new", len(insertedIds)), zap.Int("rejected", len(rejected)))

	// The count acknowledges every line received, rejected ones included: they would fail the
	// same way on every resend, and they are recorded in internal_errors.
	return fmt.Sprintf("OK: %d", len(records)+len(rejected)), nil
}

// AdmsAcceptUpload acknowledges an upload this version only records in the raw log (OPERLOG
// user/template data, photos), advancing the stamp so the device does not resend it forever.
func AdmsAcceptUpload(ctx context.Context, device models.AdmsDevice, table string, stamp string, body string) (string, *models.ApiError) {

	if table == "OPERLOG" {
		storeUploadedTemplates(ctx, device, body)
	}

	if appError := database.DevDb.SaveStamp(ctx, device.Id, table, stamp); appError != nil {
		return "", fromDaoError(ctx, "AdmsAcceptUpload", appError)
	}

	return fmt.Sprintf("OK: %d", len(splitLines(body))), nil
}

// AdmsPoll answers a heartbeat with whatever is queued for the device.
func AdmsPoll(ctx context.Context, device models.AdmsDevice) (string, *models.ApiError) {

	commands, appError := database.CmdDb.TakePendingCommands(ctx, device.Id, COMMANDS_PER_POLL, COMMAND_RESEND_AFTER, COMMAND_MAX_ATTEMPTS)
	if appError != nil {
		return "", fromDaoError(ctx, "AdmsPoll", appError)
	}

	return renderCommands(commands), nil
}

// AdmsCommandResults records the device's answers to commands it ran.
func AdmsCommandResults(ctx context.Context, device models.AdmsDevice, body string) (string, *models.ApiError) {

	results := parseDeviceCmdResults(body)

	updated, appError := database.CmdDb.CompleteCommands(ctx, device.Id, results)
	if appError != nil {
		return "", fromDaoError(ctx, "AdmsCommandResults", appError)
	}

	if updated < len(results) {
		logger.Log.Warn("adms_devicecmd_unmatched", zap.String("serial", device.SerialNumber), zap.Int("results", len(results)), zap.Int("matched", updated))
	}

	return "OK", nil
}
