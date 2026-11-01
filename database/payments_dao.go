package database

import (
	"context"
	"time"

	"essl_integration_service/models"
	"essl_integration_service/utils"

	"github.com/jackc/pgx/v5"
)

type paymentDb struct{}

type paymentDbInterface interface {
	ListPayments(ctx context.Context, gymId int, from time.Time, to time.Time) (payments []models.Payment, appError *models.ApplicationError)
	RecentPayments(ctx context.Context, gymId int, limit int) (payments []models.Payment, appError *models.ApplicationError)
	ListMemberPayments(ctx context.Context, gymId int, memberId int) (payments []models.Payment, appError *models.ApplicationError)
	SumPayments(ctx context.Context, gymId int, from time.Time, to time.Time) (totalPaise int64, appError *models.ApplicationError)
}

var PayDb paymentDbInterface

const (
	PAYMENTS_DAO_BYTE3_VALUE = 0x00360000 //3 = dao, 6 = payments
)

var (
	PAYMENTS_DAO_ERROR_BASE_CODE int
)

func init() {
	PayDb = &paymentDb{}
	PAYMENTS_DAO_ERROR_BASE_CODE = utils.ServiceErrorBaseCode + PAYMENTS_DAO_BYTE3_VALUE
}

const paymentSelect = `
SELECT p.id, p."memberId", m."name", p."membershipId", p."amountPaise", p."method", p."paidAt"
  FROM payments p JOIN members m ON m.id = p."memberId"`

func (d *paymentDb) queryPayments(ctx context.Context, errorCode int, operation string, sqlStatement string, args ...any) ([]models.Payment, *models.ApplicationError) {

	rows, err := dbPool.Query(ctx, sqlStatement, args...)
	if err != nil {
		return nil, renderDaoError(ctx, errorCode, operation, err, "Could not list payments")
	}

	payments, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (models.Payment, error) {
		var p models.Payment
		err := row.Scan(&p.Id, &p.MemberId, &p.MemberName, &p.MembershipId, &p.AmountPaise, &p.Method, &p.PaidAt)
		return p, err
	})
	if err != nil {
		return nil, renderDaoError(ctx, errorCode+1, operation, err, "Could not read payments")
	}

	return payments, nil
}

// ListPayments returns payments in [from, to), newest first.
func (d *paymentDb) ListPayments(ctx context.Context, gymId int, from time.Time, to time.Time) (payments []models.Payment, appError *models.ApplicationError) {

	return d.queryPayments(ctx, PAYMENTS_DAO_ERROR_BASE_CODE+1, "ListPayments",
		paymentSelect+` WHERE p."gymId"=$1 AND p."paidAt" >= $2 AND p."paidAt" < $3 ORDER BY p."paidAt" DESC, p.id DESC`, gymId, from, to)
}

func (d *paymentDb) RecentPayments(ctx context.Context, gymId int, limit int) (payments []models.Payment, appError *models.ApplicationError) {

	return d.queryPayments(ctx, PAYMENTS_DAO_ERROR_BASE_CODE+3, "RecentPayments",
		paymentSelect+` WHERE p."gymId"=$1 ORDER BY p."paidAt" DESC, p.id DESC LIMIT $2`, gymId, limit)
}

func (d *paymentDb) ListMemberPayments(ctx context.Context, gymId int, memberId int) (payments []models.Payment, appError *models.ApplicationError) {

	return d.queryPayments(ctx, PAYMENTS_DAO_ERROR_BASE_CODE+5, "ListMemberPayments",
		paymentSelect+` WHERE p."gymId"=$1 AND p."memberId"=$2 ORDER BY p."paidAt" DESC, p.id DESC`, gymId, memberId)
}

func (d *paymentDb) SumPayments(ctx context.Context, gymId int, from time.Time, to time.Time) (totalPaise int64, appError *models.ApplicationError) {

	err := dbPool.QueryRow(ctx, `SELECT COALESCE(SUM("amountPaise"),0) FROM payments WHERE "gymId"=$1 AND "paidAt" >= $2 AND "paidAt" < $3`, gymId, from, to).Scan(&totalPaise)
	if err != nil {
		return 0, renderDaoError(ctx, PAYMENTS_DAO_ERROR_BASE_CODE+7, "SumPayments", err, "Could not total payments")
	}

	return totalPaise, nil
}
