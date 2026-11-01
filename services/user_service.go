package services

import (
	"context"
	"net/http"
	"strings"

	"essl_integration_service/database"
	"essl_integration_service/models"
	"essl_integration_service/utils"
)

const (
	USER_SERVICE_BYTE3_VALUE = 0x00230000 //2 = services, 3 = users
)

var (
	USER_SERVICE_ERROR_BASE_CODE int
)

func init() {
	USER_SERVICE_ERROR_BASE_CODE = utils.ServiceErrorBaseCode + USER_SERVICE_BYTE3_VALUE
}

func ListStaff(ctx context.Context, session models.Session) (*models.ApiError, []models.UserResponse) {

	gymId, apiError := requireGym(ctx, session)
	if apiError != nil {
		return apiError, nil
	}
	if apiError := requireOwner(ctx, session); apiError != nil {
		return apiError, nil
	}

	users, appError := database.UserDb.ListGymUsers(ctx, gymId)
	if appError != nil {
		return fromDaoError(ctx, "ListStaff", appError), nil
	}

	return nil, users
}

// CreateStaff gives the front desk its own login, so payments are recorded against who took them.
func CreateStaff(ctx context.Context, session models.Session, input models.CreateUserRequestBody) (*models.ApiError, *models.UserResponse) {

	gymId, apiError := requireGym(ctx, session)
	if apiError != nil {
		return apiError, nil
	}
	if apiError := requireOwner(ctx, session); apiError != nil {
		return apiError, nil
	}

	hash, err := HashPassword(input.Password)
	if err != nil {
		return utils.RenderApiErrorV2(ctx, http.StatusInternalServerError, USER_SERVICE_ERROR_BASE_CODE+1, "hash failed: "+err.Error(), "Could not create user", nil), nil
	}

	email := strings.ToLower(strings.TrimSpace(input.Email))
	name := strings.TrimSpace(input.Name)

	userId, appError := database.UserDb.CreateUser(ctx, models.User{GymId: &gymId, Name: name, Email: email, PasswordHash: hash, Role: models.ROLE_STAFF})
	if appError != nil {
		if database.IsConflict(appError) {
			appError.Message.DisplayMessage = "A user with this email already exists"
		}
		return fromDaoError(ctx, "CreateStaff", appError), nil
	}

	return nil, &models.UserResponse{Id: userId, Name: name, Email: email, Role: models.ROLE_STAFF, Active: true}
}

func SetStaffActive(ctx context.Context, session models.Session, userId int, active bool) *models.ApiError {

	gymId, apiError := requireGym(ctx, session)
	if apiError != nil {
		return apiError
	}
	if apiError := requireOwner(ctx, session); apiError != nil {
		return apiError
	}

	if appError := database.UserDb.SetGymUserActive(ctx, gymId, userId, active); appError != nil {
		return fromDaoError(ctx, "SetStaffActive", appError)
	}

	return nil
}
