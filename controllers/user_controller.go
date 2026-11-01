package controllers

import (
	"net/http"

	"essl_integration_service/models"
	"essl_integration_service/services"
	"essl_integration_service/utils"

	"github.com/gin-gonic/gin"
)

const (
	USER_CONTROLLER_BYTE3_VALUE = 0x00170000 //1 = controller, 7 = users
)

var (
	USER_CONTROLLER_ERROR_BASE_CODE int
)

func init() {
	USER_CONTROLLER_ERROR_BASE_CODE = utils.ServiceErrorBaseCode + USER_CONTROLLER_BYTE3_VALUE
}

func ListStaff(c *gin.Context) {

	ctx := getContextFromGinContext(c)

	apiError, users := services.ListStaff(ctx, getSession(c))
	if apiError != nil {
		respondApiError(c, apiError)
		return
	}

	respondSuccess(c, http.StatusOK, users)
}

func CreateStaff(c *gin.Context) {

	ctx := getContextFromGinContext(c)

	var input models.CreateUserRequestBody
	if err := c.ShouldBindJSON(&input); err != nil {
		respondBindError(c, ctx, USER_CONTROLLER_ERROR_BASE_CODE+1, "CreateStaff", "Request body", nil, err)
		return
	}

	apiError, user := services.CreateStaff(ctx, getSession(c), input)
	if apiError != nil {
		respondApiError(c, apiError)
		return
	}

	respondSuccess(c, http.StatusCreated, user)
}

func SetStaffActive(c *gin.Context) {

	ctx := getContextFromGinContext(c)

	var params models.UserPathParam
	if err := c.ShouldBindUri(&params); err != nil {
		respondBindError(c, ctx, USER_CONTROLLER_ERROR_BASE_CODE+2, "SetStaffActive", "Path parameter", params, err)
		return
	}

	var input models.SetUserActiveRequestBody
	if err := c.ShouldBindJSON(&input); err != nil {
		respondBindError(c, ctx, USER_CONTROLLER_ERROR_BASE_CODE+3, "SetStaffActive", "Request body", input, err)
		return
	}

	if apiError := services.SetStaffActive(ctx, getSession(c), params.UserId, *input.Active); apiError != nil {
		respondApiError(c, apiError)
		return
	}

	respondSuccess(c, http.StatusOK, "updated")
}
