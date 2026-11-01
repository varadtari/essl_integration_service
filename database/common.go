package database

import (
	"context"
	"errors"
	"fmt"

	"essl_integration_service/logger"
	"essl_integration_service/models"
	"essl_integration_service/utils"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const (
	PG_UNIQUE_VIOLATION = "23505"
)

// ErrConflict is returned (wrapped in an ApplicationError's AdditionalInfo) when a write lost a
// race or hit a unique constraint, so services can answer 409 instead of 500.
var ErrConflict = errors.New("conflict")

// ErrNotFound marks a lookup that matched no row in the caller's gym.
var ErrNotFound = errors.New("not found")

// ErrCanceled marks a query cut short because the caller went away (the browser aborted the
// request, e.g. a search that changed mid-type). It is not a server fault.
var ErrCanceled = errors.New("canceled")

// renderDaoError logs a failed query and wraps it the way every DAO in the template does,
// tagging not-found and unique violations so the service layer can pick the HTTP status.
func renderDaoError(ctx context.Context, errorCode int, operation string, err error, displayMsg string) *models.ApplicationError {

	var kind error
	var pgErr *pgconn.PgError
	switch {
	case errors.Is(err, context.Canceled):
		kind = ErrCanceled
	case errors.Is(err, pgx.ErrNoRows), errors.Is(err, ErrNotFound):
		kind = ErrNotFound
	case errors.Is(err, ErrConflict):
		kind = ErrConflict
	case errors.As(err, &pgErr) && pgErr.Code == PG_UNIQUE_VIOLATION:
		kind = ErrConflict
	}

	errMsg := fmt.Sprintf("%s:%s.Error:%s", operation, displayMsg, err.Error())
	if kind == nil {
		logger.Log.Error(errMsg)
	} else {
		logger.Log.Info(errMsg)
	}

	return utils.RenderAppErrorV2(ctx, errorCode, errMsg, displayMsg, kind)
}

// IsNotFound and IsConflict let services branch on a DAO error's kind.
func IsNotFound(appError *models.ApplicationError) bool {
	return appError != nil && appError.Message.AdditionalInfo == ErrNotFound
}

func IsConflict(appError *models.ApplicationError) bool {
	return appError != nil && appError.Message.AdditionalInfo == ErrConflict
}

func IsCanceled(appError *models.ApplicationError) bool {
	return appError != nil && appError.Message.AdditionalInfo == ErrCanceled
}

// withTx runs fn in a transaction, rolling back on error or panic.
func withTx(ctx context.Context, fn func(tx pgx.Tx) error) error {

	tx, err := dbPool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if err := fn(tx); err != nil {
		return err
	}

	return tx.Commit(ctx)
}
