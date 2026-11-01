package controllers

import (
	"net/http"

	"essl_integration_service/models"
	"essl_integration_service/services"
	"essl_integration_service/utils"

	"github.com/gin-gonic/gin"
)

const (
	PLAN_CONTROLLER_BYTE3_VALUE = 0x00130000 //1 = controller, 3 = plans
)

var (
	PLAN_CONTROLLER_ERROR_BASE_CODE int
)

func init() {
	PLAN_CONTROLLER_ERROR_BASE_CODE = utils.ServiceErrorBaseCode + PLAN_CONTROLLER_BYTE3_VALUE
}

func ListPlans(c *gin.Context) {

	ctx := getContextFromGinContext(c)

	apiError, plans := services.ListPlans(ctx, getSession(c))
	if apiError != nil {
		respondApiError(c, apiError)
		return
	}

	respondSuccess(c, http.StatusOK, plans)
}

func CreatePlan(c *gin.Context) {

	ctx := getContextFromGinContext(c)

	var input models.CreatePlanRequestBody
	if err := c.ShouldBindJSON(&input); err != nil {
		respondBindError(c, ctx, PLAN_CONTROLLER_ERROR_BASE_CODE+1, "CreatePlan", "Request body", input, err)
		return
	}

	apiError, plan := services.CreatePlan(ctx, getSession(c), input)
	if apiError != nil {
		respondApiError(c, apiError)
		return
	}

	respondSuccess(c, http.StatusCreated, plan)
}

func UpdatePlan(c *gin.Context) {

	ctx := getContextFromGinContext(c)

	var params models.PlanPathParam
	if err := c.ShouldBindUri(&params); err != nil {
		respondBindError(c, ctx, PLAN_CONTROLLER_ERROR_BASE_CODE+2, "UpdatePlan", "Path parameter", params, err)
		return
	}

	var input models.UpdatePlanRequestBody
	if err := c.ShouldBindJSON(&input); err != nil {
		respondBindError(c, ctx, PLAN_CONTROLLER_ERROR_BASE_CODE+3, "UpdatePlan", "Request body", input, err)
		return
	}

	apiError, plan := services.UpdatePlan(ctx, getSession(c), params.PlanId, input)
	if apiError != nil {
		respondApiError(c, apiError)
		return
	}

	respondSuccess(c, http.StatusOK, plan)
}
