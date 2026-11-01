package services

import (
	"context"
	"net/http"
	"time"

	"essl_integration_service/database"
	"essl_integration_service/misc"
	"essl_integration_service/models"
	"essl_integration_service/utils"
)

const (
	COMMON_SERVICE_BYTE3_VALUE = 0x002c0000 //2 = services, c = common
)

var (
	COMMON_SERVICE_ERROR_BASE_CODE int
)

func init() {
	COMMON_SERVICE_ERROR_BASE_CODE = utils.ServiceErrorBaseCode + COMMON_SERVICE_BYTE3_VALUE
}

// STATUS_CLIENT_CLOSED_REQUEST is nginx's 499: the client closed the connection first.
const STATUS_CLIENT_CLOSED_REQUEST = 499

// nowFunc is the clock services read; tests replace it to pin "today".
var nowFunc = time.Now

// fromDaoError turns a DAO error into the API error to return: not-found and conflict keep
// their display message with 404/409, anything else is a 500 that is also written to
// internal_errors, as the template does for unexpected failures.
func fromDaoError(ctx context.Context, operation string, appError *models.ApplicationError) *models.ApiError {

	switch {
	case database.IsCanceled(appError):
		// The client is gone and will never read this; don't record it as an internal error.
		return utils.RenderApiErrorFromAppError(STATUS_CLIENT_CLOSED_REQUEST, appError)
	case database.IsNotFound(appError):
		appError.Message.AdditionalInfo = nil
		return utils.RenderApiErrorFromAppError(http.StatusNotFound, appError)
	case database.IsConflict(appError):
		appError.Message.AdditionalInfo = nil
		return utils.RenderApiErrorFromAppError(http.StatusConflict, appError)
	}

	misc.ProcessError(ctx, models.API_ERROR_NO_INTERVENTION_REQUIRED, operation+": "+appError.Message.ErrorMessage, appError)
	return utils.RenderApiErrorFromAppError(http.StatusInternalServerError, appError)
}

// requireGym returns the caller's gym id. Gym-scoped services are only routed to owner/staff
// sessions, so a missing gym id here is a routing bug, reported as 403 rather than a panic.
func requireGym(ctx context.Context, session models.Session) (int, *models.ApiError) {
	if session.GymId == nil {
		return 0, utils.RenderApiErrorV2(ctx, http.StatusForbidden, COMMON_SERVICE_ERROR_BASE_CODE+1, "session has no gym", "This action needs a gym login", nil)
	}
	return *session.GymId, nil
}

// requireOwner allows only the gym owner through, for settings, plans and staff.
func requireOwner(ctx context.Context, session models.Session) *models.ApiError {
	if session.Role != models.ROLE_OWNER {
		return utils.RenderApiErrorV2(ctx, http.StatusForbidden, COMMON_SERVICE_ERROR_BASE_CODE+2, "role "+session.Role+" is not owner", "Only the gym owner can do this", nil)
	}
	return nil
}

func badRequest(ctx context.Context, errorCode int, message string) *models.ApiError {
	return utils.RenderApiErrorV2(ctx, http.StatusBadRequest, errorCode, message, message, nil)
}
