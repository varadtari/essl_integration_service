package database

import (
	"context"

	"essl_integration_service/models"
	"essl_integration_service/utils"

	"github.com/jackc/pgx/v5"
)

type gymDb struct{}

type gymDbInterface interface {
	CreateGymWithOwner(ctx context.Context, name string, timezone string, owner models.User, plans []models.CreatePlanRequestBody) (gym *models.Gym, ownerId int, appError *models.ApplicationError)
	GetGym(ctx context.Context, gymId int) (gym *models.Gym, appError *models.ApplicationError)
	ListGymsForAdmin(ctx context.Context) (gyms []models.AdminGymListItem, appError *models.ApplicationError)
	UpdateGym(ctx context.Context, gymId int, input models.UpdateGymAdminRequestBody) (gym *models.Gym, appError *models.ApplicationError)
}

var GymDb gymDbInterface

const (
	GYMS_DAO_BYTE3_VALUE = 0x00310000 //3 = dao, 1 = gyms
)

var (
	GYMS_DAO_ERROR_BASE_CODE int
)

func init() {
	GymDb = &gymDb{}
	GYMS_DAO_ERROR_BASE_CODE = utils.ServiceErrorBaseCode + GYMS_DAO_BYTE3_VALUE
}

const gymColumns = `id, "name", "timezone", "graceDays", "blockOnExpiry", "billingStatus"`

func scanGym(row pgx.Row) (*models.Gym, error) {
	var gym models.Gym
	err := row.Scan(&gym.Id, &gym.Name, &gym.Timezone, &gym.GraceDays, &gym.BlockOnExpiry, &gym.BillingStatus)
	if err != nil {
		return nil, err
	}
	return &gym, nil
}

// CreateGymWithOwner onboards a gym in one transaction: the gym, its owner login and the default
// plans. A duplicate owner email rolls the whole thing back, so no orphan gym is left behind.
func (d *gymDb) CreateGymWithOwner(ctx context.Context, name string, timezone string, owner models.User, plans []models.CreatePlanRequestBody) (gym *models.Gym, ownerId int, appError *models.ApplicationError) {

	err := withTx(ctx, func(tx pgx.Tx) error {

		var err error
		gym, err = scanGym(tx.QueryRow(ctx, `INSERT INTO gyms ("name","timezone") VALUES ($1,$2) RETURNING `+gymColumns, name, timezone))
		if err != nil {
			return err
		}

		err = tx.QueryRow(ctx, `INSERT INTO users ("gymId","name","email","passwordHash","role") VALUES ($1,$2,$3,$4,'owner') RETURNING id`,
			gym.Id, owner.Name, owner.Email, owner.PasswordHash).Scan(&ownerId)
		if err != nil {
			return err
		}

		for _, plan := range plans {
			_, err = tx.Exec(ctx, `INSERT INTO plans ("gymId","name","months","pricePaise") VALUES ($1,$2,$3,$4)`, gym.Id, plan.Name, plan.Months, plan.PricePaise)
			if err != nil {
				return err
			}
		}

		return nil
	})

	if err != nil {
		return nil, 0, renderDaoError(ctx, GYMS_DAO_ERROR_BASE_CODE+1, "CreateGymWithOwner", err, "Could not create gym")
	}

	return gym, ownerId, nil
}

func (d *gymDb) GetGym(ctx context.Context, gymId int) (gym *models.Gym, appError *models.ApplicationError) {

	gym, err := scanGym(dbPool.QueryRow(ctx, `SELECT `+gymColumns+` FROM gyms WHERE id=$1`, gymId))
	if err != nil {
		return nil, renderDaoError(ctx, GYMS_DAO_ERROR_BASE_CODE+2, "GetGym", err, "Could not read gym")
	}

	return gym, nil
}

// ListGymsForAdmin counts each gym's active members against that gym's own calendar day.
func (d *gymDb) ListGymsForAdmin(ctx context.Context) (gyms []models.AdminGymListItem, appError *models.ApplicationError) {

	sqlStatement := `
SELECT g.id, g."name", g."timezone", g."graceDays", g."blockOnExpiry", g."billingStatus",
       (SELECT u."email" FROM users u WHERE u."gymId"=g.id AND u."role"='owner' ORDER BY u.id LIMIT 1),
       (SELECT COUNT(*) FROM members m
          JOIN LATERAL (SELECT MAX(ms."endDate") AS "endDate" FROM memberships ms WHERE ms."memberId"=m.id) last ON TRUE
         WHERE m."gymId"=g.id AND m."deletedAt" IS NULL
           AND last."endDate" >= (NOW() AT TIME ZONE g."timezone")::date)
  FROM gyms g
 ORDER BY g."name"`

	rows, err := dbPool.Query(ctx, sqlStatement)
	if err != nil {
		return nil, renderDaoError(ctx, GYMS_DAO_ERROR_BASE_CODE+3, "ListGymsForAdmin", err, "Could not list gyms")
	}
	defer rows.Close()

	gyms = []models.AdminGymListItem{}
	for rows.Next() {
		var item models.AdminGymListItem
		if err := rows.Scan(&item.Id, &item.Name, &item.Timezone, &item.GraceDays, &item.BlockOnExpiry, &item.BillingStatus, &item.OwnerEmail, &item.ActiveMembers); err != nil {
			return nil, renderDaoError(ctx, GYMS_DAO_ERROR_BASE_CODE+4, "ListGymsForAdmin", err, "Could not read gyms")
		}
		gyms = append(gyms, item)
	}

	if err := rows.Err(); err != nil {
		return nil, renderDaoError(ctx, GYMS_DAO_ERROR_BASE_CODE+5, "ListGymsForAdmin", err, "Could not read gyms")
	}

	return gyms, nil
}

// UpdateGym changes only the fields that are set; COALESCE keeps the rest.
func (d *gymDb) UpdateGym(ctx context.Context, gymId int, input models.UpdateGymAdminRequestBody) (gym *models.Gym, appError *models.ApplicationError) {

	sqlStatement := `
UPDATE gyms SET
  "name"          = COALESCE($2, "name"),
  "timezone"      = COALESCE($3, "timezone"),
  "graceDays"     = COALESCE($4, "graceDays"),
  "blockOnExpiry" = COALESCE($5, "blockOnExpiry"),
  "billingStatus" = COALESCE($6::gym_billing_status_enum, "billingStatus")
WHERE id=$1
RETURNING ` + gymColumns

	gym, err := scanGym(dbPool.QueryRow(ctx, sqlStatement, gymId, input.Name, input.Timezone, input.GraceDays, input.BlockOnExpiry, input.BillingStatus))
	if err != nil {
		return nil, renderDaoError(ctx, GYMS_DAO_ERROR_BASE_CODE+6, "UpdateGym", err, "Could not update gym")
	}

	return gym, nil
}
