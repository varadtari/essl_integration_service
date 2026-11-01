package services

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"essl_integration_service/models"
)

// The ADMS ("PUSH") protocol as ZKTeco-family devices speak it; eSSL rebrands ZKTeco hardware
// and publishes no spec of its own. Everything here follows the widely implemented PUSH 2.x
// behaviour. Lines marked ASSUMED are not yet confirmed against an AI Face Magnum and are what
// the device spike checks (see docs/adms-protocol.md).

const ADMS_DEVICE_TIME_LAYOUT = "2006-01-02 15:04:05"

// splitLines handles \n and \r\n and drops blank lines.
func splitLines(body string) []string {
	lines := []string{}
	for _, line := range strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n") {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, strings.TrimRight(line, "\r"))
		}
	}
	return lines
}

func optionalInt(fields []string, index int) *int {
	if index >= len(fields) {
		return nil
	}
	value, err := strconv.Atoi(strings.TrimSpace(fields[index]))
	if err != nil {
		return nil
	}
	return &value
}

// parseAttLog reads an ATTLOG upload: one punch per line, tab-separated,
//
//	PIN \t YYYY-MM-DD HH:MM:SS \t status \t verify \t workcode \t reserved...
//
// Only PIN and time are required; trailing fields vary by firmware (ASSUMED: the Magnum sends
// at least status and verify). Lines that cannot be parsed are returned so they can be logged
// rather than silently lost.
func parseAttLog(body string) (records []models.AttLogRecord, rejected []string) {

	for _, line := range splitLines(body) {
		fields := strings.Split(line, "\t")
		if len(fields) < 2 {
			rejected = append(rejected, line)
			continue
		}

		pin := strings.TrimSpace(fields[0])
		localTime := strings.TrimSpace(fields[1])
		if pin == "" {
			rejected = append(rejected, line)
			continue
		}
		if _, err := time.Parse(ADMS_DEVICE_TIME_LAYOUT, localTime); err != nil {
			rejected = append(rejected, line)
			continue
		}

		records = append(records, models.AttLogRecord{
			Pin:        pin,
			LocalTime:  localTime,
			Status:     optionalInt(fields, 2),
			VerifyCode: optionalInt(fields, 3),
			Raw:        line,
		})
	}

	return records, rejected
}

// deviceLocalTime turns a device timestamp into an instant using the gym's timezone. Devices
// keep local wall-clock time with no offset (ASSUMED for the Magnum), so the gym's zone is the
// only way to place it.
func deviceLocalTime(localTime string, timezone string) (time.Time, error) {
	return time.ParseInLocation(ADMS_DEVICE_TIME_LAYOUT, localTime, gymLocation(timezone))
}

// verifyMethod names the verify code. ZKTeco's codes: 0 password, 1 fingerprint, 2 and 4 card,
// 15 face, 25 palm. ASSUMED for the Magnum; anything else is kept as "other" with the raw code
// stored next to it.
func verifyMethod(code *int) string {
	if code == nil {
		return "unknown"
	}
	switch *code {
	case 0, 3:
		return "password"
	case 1:
		return "fingerprint"
	case 2, 4:
		return "card"
	case 15:
		return "face"
	case 25:
		return "palm"
	}
	return "other"
}

// parseDeviceCmdResults reads a /iclock/devicecmd report: one result per line, each a URL-encoded
// query string such as "ID=12&Return=0&CMD=CONTROL". Return 0 means success, negative a failure.
func parseDeviceCmdResults(body string) []models.DeviceCommandResult {

	results := []models.DeviceCommandResult{}
	for _, line := range splitLines(body) {
		values, err := url.ParseQuery(strings.TrimSpace(line))
		if err != nil {
			continue
		}
		id, err := strconv.ParseInt(values.Get("ID"), 10, 64)
		if err != nil {
			continue
		}
		ret, err := strconv.Atoi(values.Get("Return"))
		if err != nil {
			continue
		}
		results = append(results, models.DeviceCommandResult{Id: id, Return: ret, Cmd: values.Get("CMD")})
	}

	return results
}

// stampOrNone is how a stamp goes back in the handshake: "None" asks for everything.
func stampOrNone(stamp *string) string {
	if stamp == nil || *stamp == "" {
		return "None"
	}
	return *stamp
}

// renderHandshakeOptions is the reply to GET /iclock/cdata?options=all. It tells the device
// where its uploads left off, how often to poll, and which data to push.
//
// TimeZone is deliberately not sent: ZKTeco firmwares disagree on whether it is hours or
// minutes, and India's +5:30 does not fit the hours form. The device keeps the clock the
// installer set (ASSUMED acceptable; revisit after the spike).
func renderHandshakeOptions(device models.AdmsDevice, heartbeatSeconds int) string {

	lines := []string{
		"GET OPTION FROM: " + device.SerialNumber,
		"ATTLOGStamp=" + stampOrNone(device.AttStamp),
		"OPERLOGStamp=" + stampOrNone(device.OpStamp),
		"ATTPHOTOStamp=" + stampOrNone(device.PhotoStamp),
		"ErrorDelay=30",
		fmt.Sprintf("Delay=%d", heartbeatSeconds),
		"TransTimes=00:00;14:05",
		"TransInterval=1",
		"TransFlag=TransData AttLog\tOpLog\tAttPhoto\tEnrollUser\tChgUser\tEnrollFP\tChgFP\tFACE\tUserPic",
		"Realtime=1",
		"Encrypt=None",
	}

	return strings.Join(lines, "\n") + "\n"
}

// renderCommands is the reply to GET /iclock/getrequest: one "C:<id>:<command>" per line, or
// "OK" when nothing is queued.
func renderCommands(commands []models.PendingCommand) string {

	if len(commands) == 0 {
		return "OK"
	}

	var b strings.Builder
	for _, cmd := range commands {
		fmt.Fprintf(&b, "C:%d:%s\n", cmd.Id, cmd.Command)
	}
	return b.String()
}

// Command text for each kind a gym can trigger. All ASSUMED until the spike: these are the
// ZKTeco PUSH forms.
func commandText(kind string, now time.Time, timezone string, repullFrom *time.Time) (string, error) {

	switch kind {
	case models.COMMAND_OPEN_DOOR:
		// Door 1, unlock, for 5 seconds.
		return "CONTROL DEVICE 01010105", nil
	case models.COMMAND_REBOOT:
		return "REBOOT", nil
	case models.COMMAND_INFO:
		return "INFO", nil
	case models.COMMAND_CHECK:
		return "CHECK", nil
	case models.COMMAND_REPULL_LOGS:
		if repullFrom == nil {
			return "", fmt.Errorf("repull_logs needs a start time")
		}
		loc := gymLocation(timezone)
		return fmt.Sprintf("DATA QUERY ATTLOG StartTime=%s\tEndTime=%s",
			repullFrom.In(loc).Format(ADMS_DEVICE_TIME_LAYOUT), now.In(loc).Format(ADMS_DEVICE_TIME_LAYOUT)), nil
	}

	return "", fmt.Errorf("unknown command kind %q", kind)
}
