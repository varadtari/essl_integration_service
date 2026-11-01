package database

import (
	"context"
	"strings"

	"essl_integration_service/models"
	"essl_integration_service/utils"

	"github.com/jackc/pgx/v5"
)

type memberDb struct{}

type memberDbInterface interface {
	ListMembers(ctx context.Context, gymId int, today string, search string, status string) (members []models.MemberRow, appError *models.ApplicationError)
	GetMember(ctx context.Context, gymId int, memberId int) (member *models.MemberRow, appError *models.ApplicationError)
	CreateMemberWithTerm(ctx context.Context, gymId int, name string, phone string, term models.NewTerm) (memberId int, devicePin int, appError *models.ApplicationError)
	RenewMember(ctx context.Context, gymId int, memberId int, expectedEndDate *string, term models.NewTerm) (membershipId int, appError *models.ApplicationError)
	UpdateMember(ctx context.Context, gymId int, memberId int, name *string, phone *string) (appError *models.ApplicationError)
	SoftDeleteMember(ctx context.Context, gymId int, memberId int) (appError *models.ApplicationError)
	ListMemberships(ctx context.Context, gymId int, memberId int) (memberships []models.Membership, appError *models.ApplicationError)
	CountMembersByStatus(ctx context.Context, gymId int, today string) (active int, overdue int, appError *models.ApplicationError)
}

var MemDb memberDbInterface

const (
	MEMBERS_DAO_BYTE3_VALUE = 0x00350000 //3 = dao, 5 = members
)

var (
	MEMBERS_DAO_ERROR_BASE_CODE int
)

func init() {
	MemDb = &memberDb{}
	MEMBERS_DAO_ERROR_BASE_CODE = utils.ServiceErrorBaseCode + MEMBERS_DAO_BYTE3_VALUE
}

// latestTermJoin attaches each member's most recent term. Dates are selected as text so they
// travel as plain YYYY-MM-DD calendar days, never as timestamps that a timezone could shift.
const latestTermJoin = `
LEFT JOIN LATERAL (
    SELECT ms."startDate", ms."endDate" FROM memberships ms
     WHERE ms."memberId" = m.id
     ORDER BY ms."endDate" DESC, ms.id DESC LIMIT 1
) last ON TRUE`

const memberRowColumns = `m.id, m."devicePin", m."name", m."phone", last."startDate"::text, last."endDate"::text`

func scanMemberRow(row pgx.Row) (models.MemberRow, error) {
	var r models.MemberRow
	err := row.Scan(&r.Id, &r.DevicePin, &r.Name, &r.Phone, &r.StartDate, &r.EndDate)
	return r, err
}

// escapeLike makes a search term match literally inside ILIKE.
func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

func (d *memberDb) ListMembers(ctx context.Context, gymId int, today string, search string, status string) (members []models.MemberRow, appError *models.ApplicationError) {

	sqlStatement := `
SELECT ` + memberRowColumns + `
  FROM members m` + latestTermJoin + `
 WHERE m."gymId" = $1 AND m."deletedAt" IS NULL
   AND ($3 = '' OR m."name" ILIKE '%' || $3 || '%' OR m."phone" LIKE '%' || $3 || '%')
   AND ($4 = ''
        OR ($4 = 'active'  AND last."endDate" >= $2::date)
        OR ($4 = 'overdue' AND last."endDate" <  $2::date))
 ORDER BY CASE WHEN $4 = 'overdue' THEN last."endDate" END ASC NULLS LAST, m."name"`

	rows, err := dbPool.Query(ctx, sqlStatement, gymId, today, escapeLike(search), status)
	if err != nil {
		return nil, renderDaoError(ctx, MEMBERS_DAO_ERROR_BASE_CODE+1, "ListMembers", err, "Could not list members")
	}

	members, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (models.MemberRow, error) { return scanMemberRow(row) })
	if err != nil {
		return nil, renderDaoError(ctx, MEMBERS_DAO_ERROR_BASE_CODE+2, "ListMembers", err, "Could not read members")
	}

	return members, nil
}

func (d *memberDb) GetMember(ctx context.Context, gymId int, memberId int) (member *models.MemberRow, appError *models.ApplicationError) {

	sqlStatement := `SELECT ` + memberRowColumns + ` FROM members m` + latestTermJoin + `
 WHERE m.id = $2 AND m."gymId" = $1 AND m."deletedAt" IS NULL`

	row, err := scanMemberRow(dbPool.QueryRow(ctx, sqlStatement, gymId, memberId))
	if err != nil {
		return nil, renderDaoError(ctx, MEMBERS_DAO_ERROR_BASE_CODE+3, "GetMember", err, "Member not found")
	}

	return &row, nil
}

// CreateMemberWithTerm writes the member, their first term and its payment together. The gym row
// is locked first so concurrent adds in the same gym take device pins one at a time.
func (d *memberDb) CreateMemberWithTerm(ctx context.Context, gymId int, name string, phone string, term models.NewTerm) (memberId int, devicePin int, appError *models.ApplicationError) {

	err := withTx(ctx, func(tx pgx.Tx) error {

		if _, err := tx.Exec(ctx, `SELECT id FROM gyms WHERE id=$1 FOR UPDATE`, gymId); err != nil {
			return err
		}

		// MAX over all members, deleted ones included: a pin is never handed out twice, so an
		// old punch carrying it can never be credited to someone new.
		if err := tx.QueryRow(ctx, `SELECT COALESCE(MAX("devicePin"),0)+1 FROM members WHERE "gymId"=$1`, gymId).Scan(&devicePin); err != nil {
			return err
		}

		if err := tx.QueryRow(ctx, `INSERT INTO members ("gymId","devicePin","name","phone") VALUES ($1,$2,$3,$4) RETURNING id`,
			gymId, devicePin, name, phone).Scan(&memberId); err != nil {
			return err
		}

		_, err := insertTerm(ctx, tx, gymId, memberId, term)
		return err
	})

	if err != nil {
		return 0, 0, renderDaoError(ctx, MEMBERS_DAO_ERROR_BASE_CODE+4, "CreateMemberWithTerm", err, "Could not add member")
	}

	return memberId, devicePin, nil
}

// RenewMember adds a term only if the member's latest end date is still the one the service
// computed the new term from. A double-submitted renewal sees a moved end date and gets
// ErrConflict instead of silently stacking a second term.
func (d *memberDb) RenewMember(ctx context.Context, gymId int, memberId int, expectedEndDate *string, term models.NewTerm) (membershipId int, appError *models.ApplicationError) {

	err := withTx(ctx, func(tx pgx.Tx) error {

		if _, err := tx.Exec(ctx, `SELECT id FROM members WHERE id=$2 AND "gymId"=$1 AND "deletedAt" IS NULL FOR UPDATE`, gymId, memberId); err != nil {
			return err
		}

		var currentEndDate *string
		if err := tx.QueryRow(ctx, `SELECT MAX("endDate")::text FROM memberships WHERE "memberId"=$1`, memberId).Scan(&currentEndDate); err != nil {
			return err
		}

		if !sameDate(currentEndDate, expectedEndDate) {
			return ErrConflict
		}

		var err error
		membershipId, err = insertTerm(ctx, tx, gymId, memberId, term)
		return err
	})

	if err != nil {
		return 0, renderDaoError(ctx, MEMBERS_DAO_ERROR_BASE_CODE+5, "RenewMember", err, "Could not renew membership")
	}

	return membershipId, nil
}

func sameDate(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func insertTerm(ctx context.Context, tx pgx.Tx, gymId int, memberId int, term models.NewTerm) (membershipId int, err error) {

	err = tx.QueryRow(ctx, `INSERT INTO memberships ("gymId","memberId","planId","startDate","endDate","createdBy") VALUES ($1,$2,$3,$4,$5,$6) RETURNING id`,
		gymId, memberId, term.PlanId, term.StartDate, term.EndDate, term.RecordedBy).Scan(&membershipId)
	if err != nil {
		return 0, err
	}

	_, err = tx.Exec(ctx, `INSERT INTO payments ("gymId","memberId","membershipId","amountPaise","method","recordedBy") VALUES ($1,$2,$3,$4,$5,$6)`,
		gymId, memberId, membershipId, term.AmountPaise, term.Method, term.RecordedBy)

	return membershipId, err
}

func (d *memberDb) UpdateMember(ctx context.Context, gymId int, memberId int, name *string, phone *string) (appError *models.ApplicationError) {

	tag, err := dbPool.Exec(ctx, `UPDATE members SET "name"=COALESCE($3,"name"), "phone"=COALESCE($4,"phone") WHERE id=$2 AND "gymId"=$1 AND "deletedAt" IS NULL`,
		gymId, memberId, name, phone)
	if err == nil && tag.RowsAffected() == 0 {
		err = ErrNotFound
	}
	if err != nil {
		return renderDaoError(ctx, MEMBERS_DAO_ERROR_BASE_CODE+6, "UpdateMember", err, "Could not update member")
	}

	return nil
}

func (d *memberDb) SoftDeleteMember(ctx context.Context, gymId int, memberId int) (appError *models.ApplicationError) {

	tag, err := dbPool.Exec(ctx, `UPDATE members SET "deletedAt"=NOW() WHERE id=$2 AND "gymId"=$1 AND "deletedAt" IS NULL`, gymId, memberId)
	if err == nil && tag.RowsAffected() == 0 {
		err = ErrNotFound
	}
	if err != nil {
		return renderDaoError(ctx, MEMBERS_DAO_ERROR_BASE_CODE+7, "SoftDeleteMember", err, "Could not delete member")
	}

	return nil
}

func (d *memberDb) ListMemberships(ctx context.Context, gymId int, memberId int) (memberships []models.Membership, appError *models.ApplicationError) {

	sqlStatement := `
SELECT ms.id, ms."planId", p."name", ms."startDate"::text, ms."endDate"::text
  FROM memberships ms JOIN plans p ON p.id = ms."planId"
 WHERE ms."gymId"=$1 AND ms."memberId"=$2
 ORDER BY ms."endDate" DESC, ms.id DESC`

	rows, err := dbPool.Query(ctx, sqlStatement, gymId, memberId)
	if err != nil {
		return nil, renderDaoError(ctx, MEMBERS_DAO_ERROR_BASE_CODE+8, "ListMemberships", err, "Could not list memberships")
	}

	memberships, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (models.Membership, error) {
		var m models.Membership
		err := row.Scan(&m.Id, &m.PlanId, &m.PlanName, &m.StartDate, &m.EndDate)
		return m, err
	})
	if err != nil {
		return nil, renderDaoError(ctx, MEMBERS_DAO_ERROR_BASE_CODE+9, "ListMemberships", err, "Could not read memberships")
	}

	return memberships, nil
}

// CountMembersByStatus splits live members by whether their latest term covers today.
// Members with no term at all (not possible through the API) count as neither.
func (d *memberDb) CountMembersByStatus(ctx context.Context, gymId int, today string) (active int, overdue int, appError *models.ApplicationError) {

	sqlStatement := `
SELECT COUNT(*) FILTER (WHERE last."endDate" >= $2::date),
       COUNT(*) FILTER (WHERE last."endDate" <  $2::date)
  FROM members m` + latestTermJoin + `
 WHERE m."gymId"=$1 AND m."deletedAt" IS NULL`

	if err := dbPool.QueryRow(ctx, sqlStatement, gymId, today).Scan(&active, &overdue); err != nil {
		return 0, 0, renderDaoError(ctx, MEMBERS_DAO_ERROR_BASE_CODE+10, "CountMembersByStatus", err, "Could not count members")
	}

	return active, overdue, nil
}
