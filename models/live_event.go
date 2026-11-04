package models

// LiveEvent travels from the database (pg_notify on LIVE_EVENTS_CHANNEL) to every portal tab of
// one gym. It names what changed; the stream then loads the rows itself, scoped to the gym.
type LiveEvent struct {
	Type     string  `json:"type"`
	GymId    int     `json:"gymId"`
	Ids      []int64 `json:"ids,omitempty"`
	DeviceId int     `json:"deviceId,omitempty"`
}

const LIVE_EVENTS_CHANNEL = "najah_events"

const (
	// LIVE_EVENT_CHECKINS: new access_events rows (Ids).
	LIVE_EVENT_CHECKINS = "checkins"
	// LIVE_EVENT_DEVICE: a device came online (DeviceId).
	LIVE_EVENT_DEVICE = "device"
	// LIVE_EVENT_REFRESH: events may have been missed; clients reload what they show. GymId 0
	// means every gym.
	LIVE_EVENT_REFRESH = "refresh"
)

// MAX_IDS_PER_NOTIFY keeps a notification well under Postgres' 8000-byte payload limit. A
// bigger batch (a device uploading its offline backlog) is announced as a refresh instead.
const MAX_IDS_PER_NOTIFY = 200
