package services

import (
	"context"
	"net/http"
	"strings"
	"time"

	"essl_integration_service/database"
	"essl_integration_service/models"
	"essl_integration_service/utils"
)

const (
	GYM_SERVICE_BYTE3_VALUE = 0x00220000 //2 = services, 2 = gyms
)

var (
	GYM_SERVICE_ERROR_BASE_CODE int
)

func init() {
	GYM_SERVICE_ERROR_BASE_CODE = utils.ServiceErrorBaseCode + GYM_SERVICE_BYTE3_VALUE
}

func validateTimezone(ctx context.Context, timezone *string) *models.ApiError {
	if timezone == nil {
		return nil
	}
	if _, err := time.LoadLocation(*timezone); err != nil || *timezone == "" || strings.EqualFold(*timezone, "local") {
		return badRequest(ctx, GYM_SERVICE_ERROR_BASE_CODE+1, "Unknown timezone "+*timezone)
	}
	return nil
}

// CreateGym onboards a gym with its owner login and the default price list.
func CreateGym(ctx context.Context, input models.CreateGymRequestBody) (*models.ApiError, *models.CreateGymResponse) {

	timezone := input.Timezone
	if timezone == "" {
		timezone = models.DEFAULT_GYM_TIMEZONE
	}
	if apiError := validateTimezone(ctx, &timezone); apiError != nil {
		return apiError, nil
	}

	hash, err := HashPassword(input.Owner.Password)
	if err != nil {
		return utils.RenderApiErrorV2(ctx, http.StatusInternalServerError, GYM_SERVICE_ERROR_BASE_CODE+2, "hash failed: "+err.Error(), "Could not create gym", nil), nil
	}

	ownerEmail := strings.ToLower(strings.TrimSpace(input.Owner.Email))
	owner := models.User{Name: strings.TrimSpace(input.Owner.Name), Email: ownerEmail, PasswordHash: hash}

	gym, ownerId, appError := database.GymDb.CreateGymWithOwner(ctx, strings.TrimSpace(input.Name), timezone, owner, models.DefaultPlans)
	if appError != nil {
		if database.IsConflict(appError) {
			appError.Message.DisplayMessage = "A user with this email already exists"
		}
		return fromDaoError(ctx, "CreateGym", appError), nil
	}

	return nil, &models.CreateGymResponse{
		Gym:   *gym,
		Owner: models.UserResponse{Id: ownerId, Name: owner.Name, Email: ownerEmail, Role: models.ROLE_OWNER, Active: true},
	}
}

func ListGymsForAdmin(ctx context.Context) (*models.ApiError, []models.AdminGymListItem) {

	gyms, appError := database.GymDb.ListGymsForAdmin(ctx)
	if appError != nil {
		return fromDaoError(ctx, "ListGymsForAdmin", appError), nil
	}

	return nil, gyms
}

func UpdateGymAsAdmin(ctx context.Context, gymId int, input models.UpdateGymAdminRequestBody) (*models.ApiError, *models.Gym) {

	if apiError := validateTimezone(ctx, input.Timezone); apiError != nil {
		return apiError, nil
	}

	gym, appError := database.GymDb.UpdateGym(ctx, gymId, input)
	if appError != nil {
		return fromDaoError(ctx, "UpdateGymAsAdmin", appError), nil
	}

	return nil, gym
}

func GetMyGym(ctx context.Context, session models.Session) (*models.ApiError, *models.Gym) {

	gymId, apiError := requireGym(ctx, session)
	if apiError != nil {
		return apiError, nil
	}

	gym, appError := database.GymDb.GetGym(ctx, gymId)
	if appError != nil {
		return fromDaoError(ctx, "GetMyGym", appError), nil
	}

	return nil, gym
}

// UpdateMyGym lets an owner change their own gym's settings; billing status stays admin-only
// because the owner input type has no such field.
func UpdateMyGym(ctx context.Context, session models.Session, input models.UpdateGymSettingsRequestBody) (*models.ApiError, *models.Gym) {

	gymId, apiError := requireGym(ctx, session)
	if apiError != nil {
		return apiError, nil
	}
	if apiError := requireOwner(ctx, session); apiError != nil {
		return apiError, nil
	}

	return UpdateGymAsAdmin(ctx, gymId, models.UpdateGymAdminRequestBody{UpdateGymSettingsRequestBody: input})
}
