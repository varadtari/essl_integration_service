package models

import "time"

// AttLogRecord is one punch line from an ADMS ATTLOG upload, before it is matched to a member.
type AttLogRecord struct {
	Pin        string
	LocalTime  string // as the device wrote it: "YYYY-MM-DD HH:MM:SS" in the device's local time
	Status     *int   // punch state (check-in/out keys), when the device sends one
	VerifyCode *int   // how the person was verified, see services.verifyMethod
	Raw        string
}

// DeviceCommandResult is one line of a /iclock/devicecmd report.
type DeviceCommandResult struct {
	Id     int64
	Return int
	Cmd    string
}

// AdmsDevice is the registered device an /iclock request was authenticated as.
type AdmsDevice struct {
	Id           int
	GymId        int
	SerialNumber string
	Timezone     string
	GraceDays    int
	AttStamp     *string
	OpStamp      *string
	PhotoStamp   *string
}

// PendingCommand is a queued command as handed to a device.
type PendingCommand struct {
	Id      int64
	Command string
}

// NewAccessEvent is a punch ready to store, with its member and verdict resolved.
type NewAccessEvent struct {
	MemberId     *int
	Pin          string
	PunchedAt    time.Time
	PunchStatus  *int
	VerifyCode   *int
	VerifyMethod string
	Verdict      string
	Raw          string
}

const (
	VERDICT_ALLOWED             = "allowed"
	VERDICT_ALLOWED_BUT_EXPIRED = "allowed_but_expired"
	VERDICT_UNKNOWN             = "unknown"
)

const (
	COMMAND_OPEN_DOOR   = "open_door"
	COMMAND_REBOOT      = "reboot"
	COMMAND_INFO        = "info"
	COMMAND_REPULL_LOGS = "repull_logs"
	COMMAND_CHECK       = "check"
)

const (
	COMMAND_STATUS_QUEUED = "queued"
	COMMAND_STATUS_SENT   = "sent"
	COMMAND_STATUS_ACKED  = "acked"
	COMMAND_STATUS_FAILED = "failed"
)
