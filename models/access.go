package models

const (
	ACCESS_ACTIVE  = "active"
	ACCESS_BLOCKED = "blocked"
	ACCESS_ABSENT  = "absent"
)

const (
	COMMAND_SYNC_USER        = "sync_user"
	COMMAND_DELETE_USER      = "delete_user"
	COMMAND_RESTORE_TEMPLATE = "restore_template"

	COMMAND_STATUS_SUPERSEDED = "superseded"
)

// AccessMember is a member as the access reconciler needs them: pin, name, and the latest term.
type AccessMember struct {
	Id        int
	DevicePin int
	Name      string
	StartDate *string
	EndDate   *string
	Deleted   bool
}

// DeviceAccess is one member_device_access row.
type DeviceAccess struct {
	MemberId       int
	DeviceId       int
	DesiredState   string
	ActualState    string
	PayloadHash    *string
	LastCommandId  *int64
	FailedAttempts int
	LastError      *string
	// Status of the last command sent for this pair: queued/sent means in flight.
	LastCommandStatus *string
}

// PlannedCommand is one command the reconciler wants queued for a (member, device) pair.
type PlannedCommand struct {
	Kind        string
	Command     string
	TargetState string // set on the command whose ack changes actualState
}

// MemberAccessSummary is a member's access across the gym's devices, for the members list.
type MemberAccessSummary struct {
	State   string  `json:"state"` // on_device | blocked | syncing | not_on_device | error | no_devices
	Devices int     `json:"devices"`
	Error   *string `json:"error,omitempty"`
}

const (
	ACCESS_SUMMARY_ON_DEVICE  = "on_device"
	ACCESS_SUMMARY_BLOCKED    = "blocked"
	ACCESS_SUMMARY_SYNCING    = "syncing"
	ACCESS_SUMMARY_NOT_ON     = "not_on_device"
	ACCESS_SUMMARY_ERROR      = "error"
	ACCESS_SUMMARY_NO_DEVICES = "no_devices"
)

// BiometricTemplateRow is a template ready to store.
type BiometricTemplateRow struct {
	Kind string
	Slot string
	Line string
}
