package database

import (
	"context"

	"essl_integration_service/models"
	"essl_integration_service/utils"

	"github.com/jackc/pgx/v5"
)

type userDb struct{}

type userDbInterface interface {
	GetUserByEmail(ctx context.Context, email string) (user *models.User, appError *models.ApplicationError)
	GetMe(ctx context.Context, userId int) (me *models.MeResponse, appError *models.ApplicationError)
	GetSessionUser(ctx context.Context, userId int) (user *models.User, appError *models.ApplicationError)
	CreateUser(ctx context.Context, user models.User) (userId int, appError *models.ApplicationError)
	ListGymUsers(ctx context.Context, gymId int) (users []models.UserResponse, appError *models.ApplicationError)
	SetGymUserActive(ctx context.Context, gymId int, userId int, active bool) (appError *models.ApplicationError)
}

var UserDb userDbInterface

const (
	USERS_DAO_BYTE3_VALUE = 0x00320000 //3 = dao, 2 = users
)

var (
	USERS_DAO_ERROR_BASE_CODE int
)

func init() {
	UserDb = &userDb{}
	USERS_DAO_ERROR_BASE_CODE = utils.ServiceErrorBaseCode + USERS_DAO_BYTE3_VALUE
}

func (d *userDb) GetUserByEmail(ctx context.Context, email string) (user *models.User, appError *models.ApplicationError) {

	sqlStatement := `SELECT id, "gymId", "name", "email", "passwordHash", "role", "active" FROM users WHERE LOWER("email")=LOWER($1)`

	var u models.User
	err := dbPool.QueryRow(ctx, sqlStatement, email).Scan(&u.Id, &u.GymId, &u.Name, &u.Email, &u.PasswordHash, &u.Role, &u.Active)
	if err != nil {
		return nil, renderDaoError(ctx, USERS_DAO_ERROR_BASE_CODE+1, "GetUserByEmail", err, "Could not read user")
	}

	return &u, nil
}

// GetMe also re-checks that the user is still active, so a deactivated user's unexpired token
// stops working on the next request rather than at token expiry.
func (d *userDb) GetMe(ctx context.Context, userId int) (me *models.MeResponse, appError *models.ApplicationError) {

	sqlStatement := `
SELECT u.id, u."name", u."email", u."role", u."gymId", g."name"
  FROM users u LEFT JOIN gyms g ON g.id = u."gymId"
 WHERE u.id=$1 AND u."active"`

	var m models.MeResponse
	err := dbPool.QueryRow(ctx, sqlStatement, userId).Scan(&m.UserId, &m.Name, &m.Email, &m.Role, &m.GymId, &m.GymName)
	if err != nil {
		return nil, renderDaoError(ctx, USERS_DAO_ERROR_BASE_CODE+2, "GetMe", err, "Could not read user")
	}

	return &m, nil
}

// GetSessionUser is the per-request check behind AuthorizeSession: just the fields a session's
// claims must still agree with.
func (d *userDb) GetSessionUser(ctx context.Context, userId int) (user *models.User, appError *models.ApplicationError) {

	var u models.User
	err := dbPool.QueryRow(ctx, `SELECT id, "gymId", "role", "active" FROM users WHERE id=$1`, userId).Scan(&u.Id, &u.GymId, &u.Role, &u.Active)
	if err != nil {
		return nil, renderDaoError(ctx, USERS_DAO_ERROR_BASE_CODE+7, "GetSessionUser", err, "Could not read user")
	}

	return &u, nil
}

func (d *userDb) CreateUser(ctx context.Context, user models.User) (userId int, appError *models.ApplicationError) {

	sqlStatement := `INSERT INTO users ("gymId","name","email","passwordHash","role") VALUES ($1,$2,$3,$4,$5) RETURNING id`

	err := dbPool.QueryRow(ctx, sqlStatement, user.GymId, user.Name, user.Email, user.PasswordHash, user.Role).Scan(&userId)
	if err != nil {
		return 0, renderDaoError(ctx, USERS_DAO_ERROR_BASE_CODE+3, "CreateUser", err, "Could not create user")
	}

	return userId, nil
}

func (d *userDb) ListGymUsers(ctx context.Context, gymId int) (users []models.UserResponse, appError *models.ApplicationError) {

	rows, err := dbPool.Query(ctx, `SELECT id, "name", "email", "role", "active" FROM users WHERE "gymId"=$1 ORDER BY "role", "name"`, gymId)
	if err != nil {
		return nil, renderDaoError(ctx, USERS_DAO_ERROR_BASE_CODE+4, "ListGymUsers", err, "Could not list users")
	}

	users, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (models.UserResponse, error) {
		var u models.UserResponse
		err := row.Scan(&u.Id, &u.Name, &u.Email, &u.Role, &u.Active)
		return u, err
	})
	if err != nil {
		return nil, renderDaoError(ctx, USERS_DAO_ERROR_BASE_CODE+5, "ListGymUsers", err, "Could not read users")
	}

	return users, nil
}

// SetGymUserActive only touches staff: an owner cannot lock themselves or another owner out.
func (d *userDb) SetGymUserActive(ctx context.Context, gymId int, userId int, active bool) (appError *models.ApplicationError) {

	tag, err := dbPool.Exec(ctx, `UPDATE users SET "active"=$3 WHERE id=$2 AND "gymId"=$1 AND "role"='staff'`, gymId, userId, active)
	if err == nil && tag.RowsAffected() == 0 {
		err = ErrNotFound
	}
	if err != nil {
		return renderDaoError(ctx, USERS_DAO_ERROR_BASE_CODE+6, "SetGymUserActive", err, "Could not update user")
	}

	return nil
}
