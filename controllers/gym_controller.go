package controllers

import (
	"net/http"

	"essl_integration_service/models"
	"essl_integration_service/services"
	"essl_integration_service/utils"

	"github.com/gin-gonic/gin"
)

const (
	GYM_CONTROLLER_BYTE3_VALUE = 0x00120000 //1 = controller, 2 = gyms
)

var (
	GYM_CONTROLLER_ERROR_BASE_CODE int
)

func init() {
	GYM_CONTROLLER_ERROR_BASE_CODE = utils.ServiceErrorBaseCode + GYM_CONTROLLER_BYTE3_VALUE
}

// ---- superadmin ----

func AdminCreateGym(c *gin.Context) {

	ctx := getContextFromGinContext(c)

	var input models.CreateGymRequestBody
	if err := c.ShouldBindJSON(&input); err != nil {
		respondBindError(c, ctx, GYM_CONTROLLER_ERROR_BASE_CODE+1, "AdminCreateGym", "Request body", nil, err)
		return
	}

	apiError, response := services.CreateGym(ctx, input)
	if apiError != nil {
		respondApiError(c, apiError)
		return
	}

	respondSuccess(c, http.StatusCreated, response)
}

func AdminListGyms(c *gin.Context) {

	ctx := getContextFromGinContext(c)

	apiError, gyms := services.ListGymsForAdmin(ctx)
	if apiError != nil {
		respondApiError(c, apiError)
		return
	}

	respondSuccess(c, http.StatusOK, gyms)
}

func AdminUpdateGym(c *gin.Context) {

	ctx := getContextFromGinContext(c)

	var params models.GymPathParam
	if err := c.ShouldBindUri(&params); err != nil {
		respondBindError(c, ctx, GYM_CONTROLLER_ERROR_BASE_CODE+2, "AdminUpdateGym", "Path parameter", params, err)
		return
	}

	var input models.UpdateGymAdminRequestBody
	if err := c.ShouldBindJSON(&input); err != nil {
		respondBindError(c, ctx, GYM_CONTROLLER_ERROR_BASE_CODE+3, "AdminUpdateGym", "Request body", input, err)
		return
	}

	apiError, gym := services.UpdateGymAsAdmin(ctx, params.GymId, input)
	if apiError != nil {
		respondApiError(c, apiError)
		return
	}

	respondSuccess(c, http.StatusOK, gym)
}

// ---- gym owner / staff ----

func GetMyGym(c *gin.Context) {

	ctx := getContextFromGinContext(c)

	apiError, gym := services.GetMyGym(ctx, getSession(c))
	if apiError != nil {
		respondApiError(c, apiError)
		return
	}

	respondSuccess(c, http.StatusOK, gym)
}

func UpdateMyGym(c *gin.Context) {

	ctx := getContextFromGinContext(c)

	var input models.UpdateGymSettingsRequestBody
	if err := c.ShouldBindJSON(&input); err != nil {
		respondBindError(c, ctx, GYM_CONTROLLER_ERROR_BASE_CODE+4, "UpdateMyGym", "Request body", input, err)
		return
	}

	apiError, gym := services.UpdateMyGym(ctx, getSession(c), input)
	if apiError != nil {
		respondApiError(c, apiError)
		return
	}

	respondSuccess(c, http.StatusOK, gym)
}
