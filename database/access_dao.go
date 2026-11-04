package database

import (
	"context"

	"essl_integration_service/models"
	"essl_integration_service/utils"

	"github.com/jackc/pgx/v5"
)

type accessDb struct{}

// AccessState is everything one reconcile of a gym needs, loaded together.
type AccessState struct {
	Members   []models.AccessMember
	DeviceIds []int
	Access    map[[2]int]models.DeviceAccess // keyed by {memberId, deviceId}
	Templates map[int][]string               // memberId → template lines
}

type accessDbInterface interface {
	LoadAccessState(ctx context.Context, gymId int, memberId int) (state *AccessState, appError *models.ApplicationError)
	ApplyAccessPlan(ctx context.Context, gymId int, memberId int, deviceId int, desired string, payloadHash string, commands []models.PlannedCommand) (appError *models.ApplicationError)
	SaveTemplates(ctx context.Context, gymId int, deviceId int, templates map[string][]models.BiometricTemplateRow) (saved int, appError *models.ApplicationError)
	AccessSummaries(ctx context.Context, gymId int) (rows []models.DeviceAccess, devices int, appError *models.ApplicationError)
	ListGymIds(ctx context.Context) (gymIds []int, appError *models.ApplicationError)
	ResetDeviceHashes(ctx context.Context, gymId int, deviceId int) (appError *models.ApplicationError)
}

var AccDb accessDbInterface

const (
	ACCESS_DAO_BYTE3_VALUE = 0x003d0000 //3 = dao, d = device access
)

var (
	ACCESS_DAO_ERROR_BASE_CODE int
)

func init() {
	AccDb = &accessDb{}
	ACCESS_DAO_ERROR_BASE_CODE = utils.ServiceErrorBaseCode + ACCESS_DAO_BYTE3_VALUE
}

// LoadAccessState reads a gym's members (or one member when memberId > 0), its live devices,
// the current access rows and saved templates. Deleted members are included only while some
// device may still have them, so they can be removed.
func (d *accessDb) LoadAccessState(ctx context.Context, gymId int, memberId int) (state *AccessState, appError *models.ApplicationError) {

	state = &AccessState{Access: map[[2]int]models.DeviceAccess{}, Templates: map[int][]string{}}

	fail := func(n int, err error) (*AccessState, *models.ApplicationError) {
		return nil, renderDaoError(ctx, ACCESS_DAO_ERROR_BASE_CODE+n, "LoadAccessState", err, "Could not read device access")
	}

	rows, err := dbPool.Query(ctx, `
SELECT m.id, m."devicePin", m."name", last."startDate"::text, last."endDate"::text, m."deletedAt" IS NOT NULL
  FROM members m
  LEFT JOIN LATERAL (SELECT ms."startDate", ms."endDate" FROM memberships ms WHERE ms."memberId"=m.id
                     ORDER BY ms."endDate" DESC, ms.id DESC LIMIT 1) last ON TRUE
 WHERE m."gymId"=$1 AND ($2 = 0 OR m.id = $2)
   AND (m."deletedAt" IS NULL OR EXISTS (SELECT 1 FROM member_device_access a WHERE a."memberId"=m.id AND a."actualState"='active'))`, gymId, memberId)
	if err != nil {
		return fail(1, err)
	}
	state.Members, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (models.AccessMember, error) {
		var m models.AccessMember
		err := row.Scan(&m.Id, &m.DevicePin, &m.Name, &m.StartDate, &m.EndDate, &m.Deleted)
		return m, err
	})
	if err != nil {
		return fail(2, err)
	}

	rows, err = dbPool.Query(ctx, `SELECT id FROM devices WHERE "gymId"=$1 AND "deletedAt" IS NULL ORDER BY id`, gymId)
	if err != nil {
		return fail(3, err)
	}
	state.DeviceIds, err = pgx.CollectRows(rows, pgx.RowTo[int])
	if err != nil {
		return fail(4, err)
	}

	rows, err = dbPool.Query(ctx, `
SELECT a."memberId", a."deviceId", a."desiredState", a."actualState", a."payloadHash", a."lastCommandId", a."failedAttempts", a."lastError", c."status"::text
  FROM member_device_access a LEFT JOIN device_commands c ON c.id = a."lastCommandId"
 WHERE a."gymId"=$1 AND ($2 = 0 OR a."memberId" = $2)`, gymId, memberId)
	if err != nil {
		return fail(5, err)
	}
	access, err := pgx.CollectRows(rows, scanDeviceAccess)
	if err != nil {
		return fail(6, err)
	}
	for _, a := range access {
		state.Access[[2]int{a.MemberId, a.DeviceId}] = a
	}

	rows, err = dbPool.Query(ctx, `SELECT "memberId", "line" FROM biometric_templates WHERE "gymId"=$1 AND ($2 = 0 OR "memberId" = $2) ORDER BY id`, gymId, memberId)
	if err != nil {
		return fail(7, err)
	}
	defer rows.Close()
	for rows.Next() {
		var id int
		var line string
		if err := rows.Scan(&id, &line); err != nil {
			return fail(8, err)
		}
		state.Templates[id] = append(state.Templates[id], line)
	}
	if err := rows.Err(); err != nil {
		return fail(9, err)
	}

	return state, nil
}

func scanDeviceAccess(row pgx.CollectableRow) (models.DeviceAccess, error) {
	var a models.DeviceAccess
	err := row.Scan(&a.MemberId, &a.DeviceId, &a.DesiredState, &a.ActualState, &a.PayloadHash, &a.LastCommandId, &a.FailedAttempts, &a.LastError, &a.LastCommandStatus)
	return a, err
}

// ApplyAccessPlan queues a pair's commands and records the new desired state in one
// transaction. Commands still waiting for this member on this device are superseded first, so
// a device that was offline through a block and a renewal only receives the renewal.
func (d *accessDb) ApplyAccessPlan(ctx context.Context, gymId int, memberId int, deviceId int, desired string, payloadHash string, commands []models.PlannedCommand) (appError *models.ApplicationError) {

	err := withTx(ctx, func(tx pgx.Tx) error {

		var firstId *int64
		if len(commands) > 0 {
			if _, err := tx.Exec(ctx, `
UPDATE device_commands SET "status"='superseded', "finishedAt"=NOW()
 WHERE "memberId"=$1 AND "deviceId"=$2 AND "status"='queued'
   AND "kind" IN ('sync_user','delete_user','restore_template')`, memberId, deviceId); err != nil {
				return err
			}

			for _, c := range commands {
				var target *string
				if c.TargetState != "" {
					target = &c.TargetState
				}
				var id int64
				if err := tx.QueryRow(ctx, `
INSERT INTO device_commands ("gymId","deviceId","kind","command","memberId","targetState")
VALUES ($1,$2,$3,$4,$5,$6) RETURNING id`, gymId, deviceId, c.Kind, c.Command, memberId, target).Scan(&id); err != nil {
					return err
				}
				if firstId == nil {
					firstId = &id
				}
			}
		}

		var hash *string
		if payloadHash != "" {
			hash = &payloadHash
		}

		// A new desired state resets the retry count; the same one keeps counting.
		_, err := tx.Exec(ctx, `
INSERT INTO member_device_access ("gymId","memberId","deviceId","desiredState","payloadHash","lastCommandId")
VALUES ($1,$2,$3,$4,$5,$6)
ON CONFLICT ("memberId","deviceId") DO UPDATE SET
  "failedAttempts" = CASE WHEN member_device_access."desiredState" = EXCLUDED."desiredState" THEN member_device_access."failedAttempts" ELSE 0 END,
  "lastError"      = CASE WHEN member_device_access."desiredState" = EXCLUDED."desiredState" THEN member_device_access."lastError" ELSE NULL END,
  "desiredState"   = EXCLUDED."desiredState",
  "payloadHash"    = COALESCE(EXCLUDED."payloadHash", member_device_access."payloadHash"),
  "lastCommandId"  = COALESCE(EXCLUDED."lastCommandId", member_device_access."lastCommandId"),
  -- Moving between blocked and absent sends nothing: the device already lacks the user.
  "actualState"    = CASE WHEN EXCLUDED."lastCommandId" IS NULL AND member_device_access."actualState" <> 'active'
                          THEN EXCLUDED."desiredState" ELSE member_device_access."actualState" END`,
			gymId, memberId, deviceId, desired, hash, firstId)
		if err != nil {
			return err
		}

		// A first row with no command (blocked and never on the device) is already right.
		if firstId == nil {
			_, err = tx.Exec(ctx, `UPDATE member_device_access SET "actualState"=$3 WHERE "memberId"=$1 AND "deviceId"=$2 AND "actualState"<>'active'`, memberId, deviceId, desired)
		}
		return err
	})

	if err != nil {
		return renderDaoError(ctx, ACCESS_DAO_ERROR_BASE_CODE+10, "ApplyAccessPlan", err, "Could not queue device access")
	}

	return nil
}

// SaveTemplates stores uploaded templates against the member whose pin they carry, replacing
// an older template in the same slot. Pins that are not this gym's members are skipped.
func (d *accessDb) SaveTemplates(ctx context.Context, gymId int, deviceId int, templates map[string][]models.BiometricTemplateRow) (saved int, appError *models.ApplicationError) {

	err := withTx(ctx, func(tx pgx.Tx) error {
		for pin, list := range templates {
			var memberId int
			err := tx.QueryRow(ctx, `SELECT id FROM members WHERE "gymId"=$1 AND "devicePin"::text=$2`, gymId, pin).Scan(&memberId)
			if err == pgx.ErrNoRows {
				continue
			}
			if err != nil {
				return err
			}
			for _, t := range list {
				if _, err := tx.Exec(ctx, `
INSERT INTO biometric_templates ("gymId","memberId","kind","slot","line","sourceDeviceId") VALUES ($1,$2,$3,$4,$5,$6)
ON CONFLICT ("memberId","kind","slot") DO UPDATE SET "line"=EXCLUDED."line", "sourceDeviceId"=EXCLUDED."sourceDeviceId"`,
					gymId, memberId, t.Kind, t.Slot, t.Line, deviceId); err != nil {
					return err
				}
				saved++
			}
		}
		return nil
	})

	if err != nil {
		return 0, renderDaoError(ctx, ACCESS_DAO_ERROR_BASE_CODE+11, "SaveTemplates", err, "Could not store templates")
	}

	return saved, nil
}

// AccessSummaries returns every access row of the gym's live devices, and how many there are.
func (d *accessDb) AccessSummaries(ctx context.Context, gymId int) (rows []models.DeviceAccess, devices int, appError *models.ApplicationError) {

	if err := dbPool.QueryRow(ctx, `SELECT COUNT(*) FROM devices WHERE "gymId"=$1 AND "deletedAt" IS NULL`, gymId).Scan(&devices); err != nil {
		return nil, 0, renderDaoError(ctx, ACCESS_DAO_ERROR_BASE_CODE+12, "AccessSummaries", err, "Could not read device access")
	}

	result, err := dbPool.Query(ctx, `
SELECT a."memberId", a."deviceId", a."desiredState", a."actualState", a."payloadHash", a."lastCommandId", a."failedAttempts", a."lastError", c."status"::text
  FROM member_device_access a
  JOIN devices dv ON dv.id = a."deviceId" AND dv."deletedAt" IS NULL
  LEFT JOIN device_commands c ON c.id = a."lastCommandId"
 WHERE a."gymId"=$1`, gymId)
	if err != nil {
		return nil, 0, renderDaoError(ctx, ACCESS_DAO_ERROR_BASE_CODE+13, "AccessSummaries", err, "Could not read device access")
	}
	rows, err = pgx.CollectRows(result, scanDeviceAccess)
	if err != nil {
		return nil, 0, renderDaoError(ctx, ACCESS_DAO_ERROR_BASE_CODE+14, "AccessSummaries", err, "Could not read device access")
	}

	return rows, devices, nil
}

func (d *accessDb) ListGymIds(ctx context.Context) (gymIds []int, appError *models.ApplicationError) {

	rows, err := dbPool.Query(ctx, `SELECT id FROM gyms ORDER BY id`)
	if err == nil {
		gymIds, err = pgx.CollectRows(rows, pgx.RowTo[int])
	}
	if err != nil {
		return nil, renderDaoError(ctx, ACCESS_DAO_ERROR_BASE_CODE+15, "ListGymIds", err, "Could not list gyms")
	}

	return gymIds, nil
}

// ResetDeviceHashes forgets what was pushed to a device, so the next reconcile sends every
// member again (the owner's "Sync members", e.g. after a device was factory reset).
func (d *accessDb) ResetDeviceHashes(ctx context.Context, gymId int, deviceId int) (appError *models.ApplicationError) {

	if _, err := dbPool.Exec(ctx, `
UPDATE member_device_access SET "payloadHash"=NULL, "actualState"='absent', "failedAttempts"=0, "lastError"=NULL
 WHERE "gymId"=$1 AND "deviceId"=$2`, gymId, deviceId); err != nil {
		return renderDaoError(ctx, ACCESS_DAO_ERROR_BASE_CODE+16, "ResetDeviceHashes", err, "Could not reset device access")
	}

	return nil
}
