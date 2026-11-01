package controllers

import (
	"context"
	"fmt"
	"net/http"

	"essl_integration_service/logger"
	"essl_integration_service/misc"
	"essl_integration_service/models"
	"essl_integration_service/utils"

	"github.com/gin-gonic/gin"
)

const (
	COMMON_CONTROLLER_BYTE3_VALUE = 0x001c0000 //1 = controller, c = common
)

var (
	COMMON_CONTROLLER_ERROR_BASE_CODE int
)

func init() {
	COMMON_CONTROLLER_ERROR_BASE_CODE = utils.ServiceErrorBaseCode + COMMON_CONTROLLER_BYTE3_VALUE
}

func getContextFromGinContext(c *gin.Context) (ctx context.Context) {

	ctx = c.Request.Context()
	requestId, _ := c.Get("requestId")
	requestIdString, _ := requestId.(string)
	ctx = context.WithValue(ctx, models.CONTEXT_REQUEST_ID_KEY, requestIdString)

	return ctx

}

// getSession returns the session AuthorizeSession stored. Routes using it are always behind that
// middleware, so a missing session means a route was registered in the wrong group.
func getSession(c *gin.Context) models.Session {
	session, _ := c.MustGet("session").(models.Session)
	return session
}

// respondBindError answers a request whose path, query or body failed validation, logging and
// recording it as every template controller does inline.
func respondBindError(c *gin.Context, ctx context.Context, errorCode int, operation string, what string, input interface{}, err error) {

	errMsg := fmt.Sprintf("%s: %s validation fail.Input:%s.Error:%s", operation, what, utils.ConvertStructToString(input), err.Error())
	logger.Log.Info(errMsg)
	apiError := utils.RenderApiErrorV2(ctx, http.StatusBadRequest, errorCode, errMsg, what+" validation failed", nil)
	misc.ProcessError(ctx, models.API_ERROR_NO_INTERVENTION_REQUIRED, errMsg, apiError)
	c.JSON(apiError.StatusCode, apiError.ApplicationError)
}

func respondApiError(c *gin.Context, apiError *models.ApiError) {
	c.JSON(apiError.StatusCode, apiError.ApplicationError)
}

func respondSuccess(c *gin.Context, statusCode int, message interface{}) {
	c.JSON(statusCode, models.SuccessResponse{Type: "success", Message: message})
}
