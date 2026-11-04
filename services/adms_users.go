package services

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"time"
	"unicode"

	"essl_integration_service/models"
)

// Keeping members on devices. ADMS has no "disabled user" that all firmwares honour, so the
// default block is removing the user: a device cannot match a face it does not have. Unblocking
// re-creates the user and replays the face/fingerprint templates the device uploaded when the
// member enrolled. All command forms here are ZKTeco PUSH and ASSUMED for the AI Face Magnum
// until the device spike (docs/adms-protocol.md).

// DEVICE_NAME_MAX is the name length ZKTeco terminals display.
const DEVICE_NAME_MAX = 24

// ADMS_SEND_VALIDITY adds the term's start/end to the user record so the device can refuse an
// expired member on its own, even while offline. Off by default: a firmware that rejects the
// unknown fields would refuse the whole user. Enable after the spike confirms the field names.
var ADMS_SEND_VALIDITY = os.Getenv("ADMS_SEND_VALIDITY") == "true"

// deviceText makes a value safe inside a tab-separated ADMS command: commands are separated by
// newlines and fields by tabs, so either one in a member's name could inject a command.
func deviceText(s string, max int) string {
	cleaned := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
	cleaned = strings.Join(strings.Fields(cleaned), " ")
	if runes := []rune(cleaned); len(runes) > max {
		cleaned = strings.TrimSpace(string(runes[:max]))
	}
	return cleaned
}

// userInfoCommand creates or updates a member as an ordinary user (Pri=0, never a device admin).
// With validity, the term's window in the device's local time is included (see
// ADMS_SEND_VALIDITY).
func userInfoCommand(pin int, name string, validity *[2]time.Time) string {
	cmd := fmt.Sprintf("DATA UPDATE USERINFO PIN=%d\tName=%s\tPri=0\tPasswd=\tCard=\tGrp=1\tTZ=0000000000000000", pin, deviceText(name, DEVICE_NAME_MAX))
	if validity != nil {
		cmd += fmt.Sprintf("\tStartDatetime=%s\tEndDatetime=%s", validity[0].Format(ADMS_DEVICE_TIME_LAYOUT), validity[1].Format(ADMS_DEVICE_TIME_LAYOUT))
	}
	return cmd
}

func deleteUserCommand(pin int) string {
	return fmt.Sprintf("DATA DELETE USERINFO PIN=%d", pin)
}

// templateRestoreCommand sends an uploaded template back. The OPERLOG line already has the
// device's own field layout, so it is replayed as-is (ASSUMED).
func templateRestoreCommand(line string) string {
	return "DATA UPDATE " + line
}

func payloadHash(command string) string {
	sum := sha256.Sum256([]byte(command))
	return hex.EncodeToString(sum[:16])
}

// BiometricTemplate is one template line from an OPERLOG upload.
type BiometricTemplate struct {
	Kind string
	Pin  string
	Slot string
	Line string
}

// templateKinds are the OPERLOG record types that carry biometrics: FP (fingerprint), FACE
// (infrared face), BIODATA (the unified format newer firmware uses), BIOPHOTO (the enrolment
// photo visible-light face devices learn from). USERPIC, a display photo, is not kept.
var templateKinds = map[string]bool{"FP": true, "FACE": true, "BIODATA": true, "BIOPHOTO": true}

func parseFields(rest string) map[string]string {
	fields := map[string]string{}
	for _, field := range strings.Split(rest, "\t") {
		key, value, ok := strings.Cut(field, "=")
		if ok {
			fields[strings.ToUpper(strings.TrimSpace(key))] = value
		}
	}
	return fields
}

// parseOperLogTemplates picks the biometric records out of an OPERLOG upload. Other lines
// (USER records, OPLOG operations) are ignored here; they stay in the raw log.
func parseOperLogTemplates(body string) []BiometricTemplate {

	var templates []BiometricTemplate
	for _, line := range splitLines(body) {
		kind, rest, ok := strings.Cut(line, " ")
		if !ok || !templateKinds[kind] {
			continue
		}
		fields := parseFields(rest)
		pin := strings.TrimSpace(fields["PIN"])
		if pin == "" {
			continue
		}

		var slot string
		switch kind {
		case "FP", "FACE":
			slot = fields["FID"]
		case "BIODATA":
			slot = fields["TYPE"] + "-" + fields["NO"] + "-" + fields["INDEX"]
		case "BIOPHOTO":
			slot = fields["TYPE"]
		}
		if slot == "" {
			slot = "0"
		}

		templates = append(templates, BiometricTemplate{Kind: kind, Pin: pin, Slot: slot, Line: line})
	}
	return templates
}

// AccessPlan is what one (member, device) pair needs: the commands to queue, in order, and the
// state they lead to once the first one is acknowledged.
type AccessPlan struct {
	Commands    []models.PlannedCommand
	TargetState string
	PayloadHash string
}

// desiredAccess decides whether a member should be on the gym's devices today. A term covers
// its endDate plus graceDays; a gym with blockOnExpiry off keeps everyone on.
func desiredAccess(member models.AccessMember, gym models.Gym, today time.Time) string {

	if member.Deleted {
		return models.ACCESS_ABSENT
	}
	if !gym.BlockOnExpiry {
		return models.ACCESS_ACTIVE
	}
	if member.EndDate == nil {
		return models.ACCESS_BLOCKED
	}
	end, err := parseDate(*member.EndDate)
	if err != nil || end.AddDate(0, 0, gym.GraceDays).Before(today) {
		return models.ACCESS_BLOCKED
	}
	return models.ACCESS_ACTIVE
}

// planAccess compares what a device should have with what it has and returns the commands to
// close the gap, or nil when it is already right.
//
// blocked and absent both mean "not on the device" with the delete method, so moving between
// them sends nothing. Restoring a member also replays their saved templates.
func planAccess(member models.AccessMember, desired string, current *models.DeviceAccess, templates []string, validity *[2]time.Time) *AccessPlan {

	actual := models.ACCESS_ABSENT
	wasWanted := models.ACCESS_ABSENT
	var lastHash string
	if current != nil {
		actual = current.ActualState
		wasWanted = current.DesiredState
		if current.PayloadHash != nil {
			lastHash = *current.PayloadHash
		}
	}

	if desired == models.ACCESS_ACTIVE {
		command := userInfoCommand(member.DevicePin, member.Name, validity)
		hash := payloadHash(command)
		// "Already there" only counts when nothing else was asked for since: a block queued for
		// an offline device and then a renewal must not leave the stale delete to run.
		if actual == models.ACCESS_ACTIVE && wasWanted == models.ACCESS_ACTIVE && hash == lastHash {
			return nil
		}

		plan := &AccessPlan{TargetState: models.ACCESS_ACTIVE, PayloadHash: hash}
		plan.Commands = append(plan.Commands, models.PlannedCommand{Kind: models.COMMAND_SYNC_USER, Command: command, TargetState: models.ACCESS_ACTIVE})
		// Templates are replayed whenever the user is or may be gone: removed, never added, or
		// with a delete already sent that the device may run before this. A rename keeps them.
		if actual != models.ACCESS_ACTIVE || wasWanted != models.ACCESS_ACTIVE {
			for _, line := range templates {
				plan.Commands = append(plan.Commands, models.PlannedCommand{Kind: models.COMMAND_RESTORE_TEMPLATE, Command: templateRestoreCommand(line)})
			}
		}
		return plan
	}

	if actual == models.ACCESS_ACTIVE {
		return &AccessPlan{
			TargetState: desired,
			Commands:    []models.PlannedCommand{{Kind: models.COMMAND_DELETE_USER, Command: deleteUserCommand(member.DevicePin), TargetState: desired}},
		}
	}

	// Already off the device. Record the new desired state without sending anything.
	if current == nil || current.DesiredState != desired {
		return &AccessPlan{TargetState: desired}
	}
	return nil
}

// termValidity is the window a device should accept the member in, in the gym's local time:
// from the term's start to the end of its last covered day, grace included.
func termValidity(member models.AccessMember, gym models.Gym) *[2]time.Time {
	if !ADMS_SEND_VALIDITY || member.StartDate == nil || member.EndDate == nil {
		return nil
	}
	loc := gymLocation(gym.Timezone)
	start, err1 := time.ParseInLocation(models.DATE_FORMAT, *member.StartDate, loc)
	end, err2 := time.ParseInLocation(models.DATE_FORMAT, *member.EndDate, loc)
	if err1 != nil || err2 != nil {
		return nil
	}
	last := end.AddDate(0, 0, gym.GraceDays).Add(24*time.Hour - time.Second)
	return &[2]time.Time{start, last}
}
