package controllers

import (
	"context"

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
