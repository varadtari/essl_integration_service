package database

import (
	"context"

	"essl_integration_service/models"
	"essl_integration_service/utils"

	"github.com/jackc/pgx/v5"
)

type planDb struct{}

type planDbInterface interface {
	ListPlans(ctx context.Context, gymId int) (plans []models.Plan, appError *models.ApplicationError)
	GetPlan(ctx context.Context, gymId int, planId int) (plan *models.Plan, appError *models.ApplicationError)
	CreatePlan(ctx context.Context, gymId int, input models.CreatePlanRequestBody) (plan *models.Plan, appError *models.ApplicationError)
	UpdatePlan(ctx context.Context, gymId int, planId int, input models.UpdatePlanRequestBody) (plan *models.Plan, appError *models.ApplicationError)
}

var PlanDb planDbInterface

const (
	PLANS_DAO_BYTE3_VALUE = 0x00330000 //3 = dao, 3 = plans
)

var (
	PLANS_DAO_ERROR_BASE_CODE int
)

func init() {
	PlanDb = &planDb{}
	PLANS_DAO_ERROR_BASE_CODE = utils.ServiceErrorBaseCode + PLANS_DAO_BYTE3_VALUE
}

const planColumns = `id, "name", "months", "pricePaise", "active"`

func scanPlan(row pgx.Row) (*models.Plan, error) {
	var p models.Plan
	if err := row.Scan(&p.Id, &p.Name, &p.Months, &p.PricePaise, &p.Active); err != nil {
		return nil, err
	}
	return &p, nil
}

func (d *planDb) ListPlans(ctx context.Context, gymId int) (plans []models.Plan, appError *models.ApplicationError) {

	rows, err := dbPool.Query(ctx, `SELECT `+planColumns+` FROM plans WHERE "gymId"=$1 ORDER BY "months", "name"`, gymId)
	if err != nil {
		return nil, renderDaoError(ctx, PLANS_DAO_ERROR_BASE_CODE+1, "ListPlans", err, "Could not list plans")
	}

	plans, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (models.Plan, error) {
		p, err := scanPlan(row)
		if err != nil {
			return models.Plan{}, err
		}
		return *p, nil
	})
	if err != nil {
		return nil, renderDaoError(ctx, PLANS_DAO_ERROR_BASE_CODE+2, "ListPlans", err, "Could not read plans")
	}

	return plans, nil
}

func (d *planDb) GetPlan(ctx context.Context, gymId int, planId int) (plan *models.Plan, appError *models.ApplicationError) {

	plan, err := scanPlan(dbPool.QueryRow(ctx, `SELECT `+planColumns+` FROM plans WHERE id=$2 AND "gymId"=$1`, gymId, planId))
	if err != nil {
		return nil, renderDaoError(ctx, PLANS_DAO_ERROR_BASE_CODE+3, "GetPlan", err, "Plan not found")
	}

	return plan, nil
}

func (d *planDb) CreatePlan(ctx context.Context, gymId int, input models.CreatePlanRequestBody) (plan *models.Plan, appError *models.ApplicationError) {

	plan, err := scanPlan(dbPool.QueryRow(ctx, `INSERT INTO plans ("gymId","name","months","pricePaise") VALUES ($1,$2,$3,$4) RETURNING `+planColumns,
		gymId, input.Name, input.Months, input.PricePaise))
	if err != nil {
		return nil, renderDaoError(ctx, PLANS_DAO_ERROR_BASE_CODE+4, "CreatePlan", err, "Could not create plan")
	}

	return plan, nil
}

// UpdatePlan never changes months: past memberships were sold on the old length, and the term
// dates already written for them are the record. A different length is a new plan.
func (d *planDb) UpdatePlan(ctx context.Context, gymId int, planId int, input models.UpdatePlanRequestBody) (plan *models.Plan, appError *models.ApplicationError) {

	sqlStatement := `
UPDATE plans SET
  "name"       = COALESCE($3, "name"),
  "pricePaise" = COALESCE($4, "pricePaise"),
  "active"     = COALESCE($5, "active")
WHERE id=$2 AND "gymId"=$1
RETURNING ` + planColumns

	plan, err := scanPlan(dbPool.QueryRow(ctx, sqlStatement, gymId, planId, input.Name, input.PricePaise, input.Active))
	if err != nil {
		return nil, renderDaoError(ctx, PLANS_DAO_ERROR_BASE_CODE+5, "UpdatePlan", err, "Could not update plan")
	}

	return plan, nil
}
