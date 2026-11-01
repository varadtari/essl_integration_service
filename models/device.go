package models

import "time"

type Device struct {
	Id              int        `json:"id"`
	SerialNumber    string     `json:"serialNumber"`
	Name            string     `json:"name"`
	Firmware        *string    `json:"firmware"`
	PushVersion     *string    `json:"pushVersion"`
	LastSeenAt      *time.Time `json:"lastSeenAt"`
	LastIp          *string    `json:"lastIp"`
	Online          bool       `json:"online"`
	PendingCommands int        `json:"pendingCommands"`
	LastPunchAt     *time.Time `json:"lastPunchAt"`
	// SerialLastAttemptAt is when an unregistered device with this serial last tried to connect:
	// set only on a device that has never connected, to tell a typo from a device not set up yet.
	SerialLastAttemptAt *time.Time `json:"serialLastAttemptAt,omitempty"`
	CreatedAt           time.Time  `json:"createdAt"`
}

type DevicePathParam struct {
	DeviceId int `uri:"deviceId" binding:"required,min=1"`
}

type CreateDeviceRequestBody struct {
	SerialNumber string `json:"serialNumber" binding:"required,min=4,max=64"`
	Name         string `json:"name" binding:"required,min=1,max=80"`
}

type UpdateDeviceRequestBody struct {
	Name *string `json:"name" binding:"omitempty,min=1,max=80"`
}

type CreateDeviceCommandRequestBody struct {
	Kind string `json:"kind" binding:"required,oneof=open_door reboot info repull_logs check"`
	// From is where a repull_logs starts; it ends now.
	From *time.Time `json:"from"`
}

type DeviceCommand struct {
	Id          int64      `json:"id"`
	Kind        string     `json:"kind"`
	Command     string     `json:"command"`
	Status      string     `json:"status"`
	Attempts    int        `json:"attempts"`
	ReturnCode  *int       `json:"returnCode"`
	SentAt      *time.Time `json:"sentAt"`
	FinishedAt  *time.Time `json:"finishedAt"`
	CreatedAt   time.Time  `json:"createdAt"`
	RequestedBy *int       `json:"requestedBy"`
}

// MemberTerms is a member's terms, for judging punches against the day they happened.
type MemberTerms struct {
	MemberId int
	Terms    [][2]string // [startDate, endDate] calendar days
}

type CheckinListQuery struct {
	From     string `form:"from" binding:"omitempty,datetime=2006-01-02"`
	To       string `form:"to" binding:"omitempty,datetime=2006-01-02"`
	MemberId int    `form:"memberId" binding:"omitempty,min=1"`
	DeviceId int    `form:"deviceId" binding:"omitempty,min=1"`
	Verdict  string `form:"verdict" binding:"omitempty,oneof=allowed allowed_but_expired unknown denied_expired denied_unknown"`
	Limit    int    `form:"limit" binding:"omitempty,min=1,max=200"`
	// Before continues a listing: the nextCursor of the previous page.
	Before string `form:"before" binding:"omitempty,max=64"`
}

type Checkin struct {
	Id           int64     `json:"id"`
	DeviceId     int       `json:"deviceId"`
	DeviceName   string    `json:"deviceName"`
	MemberId     *int      `json:"memberId"`
	MemberName   *string   `json:"memberName"`
	Pin          string    `json:"pin"`
	PunchedAt    time.Time `json:"punchedAt"`
	VerifyMethod string    `json:"verifyMethod"`
	Verdict      string    `json:"verdict"`
}

type CheckinPage struct {
	Checkins   []Checkin `json:"checkins"`
	NextCursor *string   `json:"nextCursor"`
}

type CheckinFilter struct {
	From     *time.Time
	To       *time.Time
	MemberId int
	DeviceId int
	Verdict  string
	Limit    int
	// Cursor position: events strictly before (PunchedAt, Id) in newest-first order.
	BeforeAt *time.Time
	BeforeId int64
}

type RawDeviceMessage struct {
	DeviceId     *int
	SerialNumber string
	Method       string
	Path         string
	Query        string
	Body         string
	BodyBytes    int
	Status       int
	Response     string
	RemoteIp     string
}
