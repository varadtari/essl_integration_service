package controllers

import (
	"net/http"

	"essl_integration_service/models"
	"essl_integration_service/services"
	"essl_integration_service/utils"

	"github.com/gin-gonic/gin"
)

const (
	PAYMENT_CONTROLLER_BYTE3_VALUE = 0x00160000 //1 = controller, 6 = payments
)

var (
	PAYMENT_CONTROLLER_ERROR_BASE_CODE int
)

func init() {
	PAYMENT_CONTROLLER_ERROR_BASE_CODE = utils.ServiceErrorBaseCode + PAYMENT_CONTROLLER_BYTE3_VALUE
}

func ListPayments(c *gin.Context) {

	ctx := getContextFromGinContext(c)

	var query models.PaymentListQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		respondBindError(c, ctx, PAYMENT_CONTROLLER_ERROR_BASE_CODE+1, "ListPayments", "Query parameter", query, err)
		return
	}

	apiError, payments := services.ListPayments(ctx, getSession(c), query)
	if apiError != nil {
		respondApiError(c, apiError)
		return
	}

	respondSuccess(c, http.StatusOK, payments)
}

func GetDashboardSummary(c *gin.Context) {

	ctx := getContextFromGinContext(c)

	apiError, summary := services.GetDashboardSummary(ctx, getSession(c))
	if apiError != nil {
		respondApiError(c, apiError)
		return
	}

	respondSuccess(c, http.StatusOK, summary)
}
