package database

import (
	"context"
	"encoding/json"

	"essl_integration_service/models"
	"essl_integration_service/utils"

	"github.com/jackc/pgx/v5"
)

type liveEventDb struct{}

type liveEventDbInterface interface {
	Notify(ctx context.Context, event models.LiveEvent) (appError *models.ApplicationError)
	GetCheckinsByIds(ctx context.Context, gymId int, ids []int64) (checkins []models.Checkin, appError *models.ApplicationError)
}

var LiveDb liveEventDbInterface

const (
	LIVE_EVENTS_DAO_BYTE3_VALUE = 0x003c0000 //3 = dao, c = live events
)

var (
	LIVE_EVENTS_DAO_ERROR_BASE_CODE int
)

func init() {
	LiveDb = &liveEventDb{}
	LIVE_EVENTS_DAO_ERROR_BASE_CODE = utils.ServiceErrorBaseCode + LIVE_EVENTS_DAO_BYTE3_VALUE
}

// checkinsEvent describes newly inserted check-ins, falling back to a refresh for batches too
// large for one notification.
func checkinsEvent(gymId int, ids []int64) models.LiveEvent {
	if len(ids) > models.MAX_IDS_PER_NOTIFY {
		return models.LiveEvent{Type: models.LIVE_EVENT_REFRESH, GymId: gymId}
	}
	return models.LiveEvent{Type: models.LIVE_EVENT_CHECKINS, GymId: gymId, Ids: ids}
}

// notifyTx queues a notification inside a transaction: Postgres delivers it only on commit, so
// listeners never hear about rows that were rolled back.
func notifyTx(ctx context.Context, tx pgx.Tx, event models.LiveEvent) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `SELECT pg_notify($1, $2)`, models.LIVE_EVENTS_CHANNEL, string(payload))
	return err
}

func (d *liveEventDb) Notify(ctx context.Context, event models.LiveEvent) (appError *models.ApplicationError) {

	payload, err := json.Marshal(event)
	if err == nil {
		_, err = dbPool.Exec(ctx, `SELECT pg_notify($1, $2)`, models.LIVE_EVENTS_CHANNEL, string(payload))
	}
	if err != nil {
		return renderDaoError(ctx, LIVE_EVENTS_DAO_ERROR_BASE_CODE+1, "Notify", err, "Could not publish event")
	}

	return nil
}

// GetCheckinsByIds loads announced check-ins. The gym condition means a stream can only ever
// render its own gym's rows, whatever ids a notification carried.
func (d *liveEventDb) GetCheckinsByIds(ctx context.Context, gymId int, ids []int64) (checkins []models.Checkin, appError *models.ApplicationError) {

	rows, err := dbPool.Query(ctx, `
SELECT e.id, e."deviceId", dv."name", e."memberId", m."name", e."pin", e."punchedAt", e."verifyMethod", e."verdict"
  FROM access_events e
  JOIN devices dv ON dv.id = e."deviceId"
  LEFT JOIN members m ON m.id = e."memberId"
 WHERE e."gymId" = $1 AND e.id = ANY($2)
 ORDER BY e."punchedAt", e.id`, gymId, ids)
	if err != nil {
		return nil, renderDaoError(ctx, LIVE_EVENTS_DAO_ERROR_BASE_CODE+2, "GetCheckinsByIds", err, "Could not read check-ins")
	}

	checkins, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (models.Checkin, error) {
		var c models.Checkin
		err := row.Scan(&c.Id, &c.DeviceId, &c.DeviceName, &c.MemberId, &c.MemberName, &c.Pin, &c.PunchedAt, &c.VerifyMethod, &c.Verdict)
		return c, err
	})
	if err != nil {
		return nil, renderDaoError(ctx, LIVE_EVENTS_DAO_ERROR_BASE_CODE+3, "GetCheckinsByIds", err, "Could not read check-ins")
	}

	return checkins, nil
}
