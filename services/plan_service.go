package services

import (
	"context"
	"strings"

	"essl_integration_service/database"
	"essl_integration_service/models"
)

func ListPlans(ctx context.Context, session models.Session) (*models.ApiError, []models.Plan) {

	gymId, apiError := requireGym(ctx, session)
	if apiError != nil {
		return apiError, nil
	}

	plans, appError := database.PlanDb.ListPlans(ctx, gymId)
	if appError != nil {
		return fromDaoError(ctx, "ListPlans", appError), nil
	}

	return nil, plans
}

func CreatePlan(ctx context.Context, session models.Session, input models.CreatePlanRequestBody) (*models.ApiError, *models.Plan) {

	gymId, apiError := requireGym(ctx, session)
	if apiError != nil {
		return apiError, nil
	}
	if apiError := requireOwner(ctx, session); apiError != nil {
		return apiError, nil
	}

	input.Name = strings.TrimSpace(input.Name)
	plan, appError := database.PlanDb.CreatePlan(ctx, gymId, input)
	if appError != nil {
		if database.IsConflict(appError) {
			appError.Message.DisplayMessage = "A plan with this name already exists"
		}
		return fromDaoError(ctx, "CreatePlan", appError), nil
	}

	return nil, plan
}

func UpdatePlan(ctx context.Context, session models.Session, planId int, input models.UpdatePlanRequestBody) (*models.ApiError, *models.Plan) {

	gymId, apiError := requireGym(ctx, session)
	if apiError != nil {
		return apiError, nil
	}
	if apiError := requireOwner(ctx, session); apiError != nil {
		return apiError, nil
	}

	plan, appError := database.PlanDb.UpdatePlan(ctx, gymId, planId, input)
	if appError != nil {
		if database.IsConflict(appError) {
			appError.Message.DisplayMessage = "A plan with this name already exists"
		}
		return fromDaoError(ctx, "UpdatePlan", appError), nil
	}

	return nil, plan
}
