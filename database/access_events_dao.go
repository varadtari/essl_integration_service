package database

import (
	"context"
	"time"

	"essl_integration_service/models"
	"essl_integration_service/utils"

	"github.com/jackc/pgx/v5"
)

type accessEventDb struct{}

type accessEventDbInterface interface {
	MemberTermsForPins(ctx context.Context, gymId int, pins []int) (terms map[int]models.MemberTerms, appError *models.ApplicationError)
	InsertEvents(ctx context.Context, gymId int, deviceId int, events []models.NewAccessEvent) (insertedIds []int64, appError *models.ApplicationError)
	ListCheckins(ctx context.Context, gymId int, filter models.CheckinFilter) (checkins []models.Checkin, appError *models.ApplicationError)
	CountCheckins(ctx context.Context, gymId int, from time.Time, to time.Time) (count int, appError *models.ApplicationError)
}

var EvDb accessEventDbInterface

const (
	ACCESS_EVENTS_DAO_BYTE3_VALUE = 0x003a0000 //3 = dao, a = access events
)

var (
	ACCESS_EVENTS_DAO_ERROR_BASE_CODE int
)

func init() {
	EvDb = &accessEventDb{}
	ACCESS_EVENTS_DAO_ERROR_BASE_CODE = utils.ServiceErrorBaseCode + ACCESS_EVENTS_DAO_BYTE3_VALUE
}

// MemberTermsForPins loads every term of the gym's members with these device pins, keyed by pin,
// so a batch of punches is judged with one query. Deleted members are included: a punch from
// before the deletion still belongs to them.
func (d *accessEventDb) MemberTermsForPins(ctx context.Context, gymId int, pins []int) (terms map[int]models.MemberTerms, appError *models.ApplicationError) {

	terms = map[int]models.MemberTerms{}
	if len(pins) == 0 {
		return terms, nil
	}

	rows, err := dbPool.Query(ctx, `
SELECT m."devicePin", m.id, ms."startDate"::text, ms."endDate"::text
  FROM members m LEFT JOIN memberships ms ON ms."memberId" = m.id
 WHERE m."gymId"=$1 AND m."devicePin" = ANY($2)`, gymId, pins)
	if err != nil {
		return nil, renderDaoError(ctx, ACCESS_EVENTS_DAO_ERROR_BASE_CODE+1, "MemberTermsForPins", err, "Could not read members")
	}
	defer rows.Close()

	for rows.Next() {
		var pin, memberId int
		var start, end *string
		if err := rows.Scan(&pin, &memberId, &start, &end); err != nil {
			return nil, renderDaoError(ctx, ACCESS_EVENTS_DAO_ERROR_BASE_CODE+2, "MemberTermsForPins", err, "Could not read members")
		}
		entry := terms[pin]
		entry.MemberId = memberId
		if start != nil && end != nil {
			entry.Terms = append(entry.Terms, [2]string{*start, *end})
		}
		terms[pin] = entry
	}

	if err := rows.Err(); err != nil {
		return nil, renderDaoError(ctx, ACCESS_EVENTS_DAO_ERROR_BASE_CODE+3, "MemberTermsForPins", err, "Could not read members")
	}

	return terms, nil
}

// InsertEvents stores a batch, skipping punches already stored, and returns the ids of the rows
// actually inserted so only new check-ins are announced.
func (d *accessEventDb) InsertEvents(ctx context.Context, gymId int, deviceId int, events []models.NewAccessEvent) (insertedIds []int64, appError *models.ApplicationError) {

	if len(events) == 0 {
		return nil, nil
	}

	err := withTx(ctx, func(tx pgx.Tx) error {
		batch := &pgx.Batch{}
		for _, e := range events {
			batch.Queue(`
INSERT INTO access_events ("gymId","deviceId","memberId","pin","punchedAt","punchStatus","verifyCode","verifyMethod","verdict","raw")
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
ON CONFLICT ("deviceId","pin","punchedAt") DO NOTHING
RETURNING id`, gymId, deviceId, e.MemberId, e.Pin, e.PunchedAt, e.PunchStatus, e.VerifyCode, e.VerifyMethod, e.Verdict, e.Raw)
		}

		results := tx.SendBatch(ctx, batch)
		for range events {
			var id int64
			err := results.QueryRow().Scan(&id)
			if err == pgx.ErrNoRows {
				continue
			}
			if err != nil {
				results.Close()
				return err
			}
			insertedIds = append(insertedIds, id)
		}
		return results.Close()
	})

	if err != nil {
		return nil, renderDaoError(ctx, ACCESS_EVENTS_DAO_ERROR_BASE_CODE+4, "InsertEvents", err, "Could not store check-ins")
	}

	return insertedIds, nil
}

// ListCheckins returns check-ins newest first. Paging uses (punchedAt, id) rather than id alone
// because a device that was offline uploads old punches late, so ids are not in time order.
func (d *accessEventDb) ListCheckins(ctx context.Context, gymId int, filter models.CheckinFilter) (checkins []models.Checkin, appError *models.ApplicationError) {

	sqlStatement := `
SELECT e.id, e."deviceId", dv."name", e."memberId", m."name", e."pin", e."punchedAt", e."verifyMethod", e."verdict"
  FROM access_events e
  JOIN devices dv ON dv.id = e."deviceId"
  LEFT JOIN members m ON m.id = e."memberId"
 WHERE e."gymId" = $1
   AND ($2::timestamptz IS NULL OR e."punchedAt" >= $2)
   AND ($3::timestamptz IS NULL OR e."punchedAt" <  $3)
   AND ($4 = 0 OR e."memberId" = $4)
   AND ($5 = 0 OR e."deviceId" = $5)
   AND ($6 = '' OR e."verdict" = $6::access_verdict_enum)
   AND ($7::timestamptz IS NULL OR (e."punchedAt", e.id) < ($7, $8))
 ORDER BY e."punchedAt" DESC, e.id DESC
 LIMIT $9`

	rows, err := dbPool.Query(ctx, sqlStatement, gymId, filter.From, filter.To, filter.MemberId, filter.DeviceId, filter.Verdict, filter.BeforeAt, filter.BeforeId, filter.Limit)
	if err != nil {
		return nil, renderDaoError(ctx, ACCESS_EVENTS_DAO_ERROR_BASE_CODE+5, "ListCheckins", err, "Could not list check-ins")
	}

	checkins, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (models.Checkin, error) {
		var c models.Checkin
		err := row.Scan(&c.Id, &c.DeviceId, &c.DeviceName, &c.MemberId, &c.MemberName, &c.Pin, &c.PunchedAt, &c.VerifyMethod, &c.Verdict)
		return c, err
	})
	if err != nil {
		return nil, renderDaoError(ctx, ACCESS_EVENTS_DAO_ERROR_BASE_CODE+6, "ListCheckins", err, "Could not read check-ins")
	}

	return checkins, nil
}

func (d *accessEventDb) CountCheckins(ctx context.Context, gymId int, from time.Time, to time.Time) (count int, appError *models.ApplicationError) {

	err := dbPool.QueryRow(ctx, `SELECT COUNT(*) FROM access_events WHERE "gymId"=$1 AND "punchedAt" >= $2 AND "punchedAt" < $3`, gymId, from, to).Scan(&count)
	if err != nil {
		return 0, renderDaoError(ctx, ACCESS_EVENTS_DAO_ERROR_BASE_CODE+7, "CountCheckins", err, "Could not count check-ins")
	}

	return count, nil
}
