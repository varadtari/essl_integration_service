package controllers

import (
	"net/http"

	"essl_integration_service/models"
	"essl_integration_service/services"
	"essl_integration_service/utils"

	"github.com/gin-gonic/gin"
)

const (
	MEMBER_CONTROLLER_BYTE3_VALUE = 0x00150000 //1 = controller, 5 = members
)

var (
	MEMBER_CONTROLLER_ERROR_BASE_CODE int
)

func init() {
	MEMBER_CONTROLLER_ERROR_BASE_CODE = utils.ServiceErrorBaseCode + MEMBER_CONTROLLER_BYTE3_VALUE
}

func ListMembers(c *gin.Context) {

	ctx := getContextFromGinContext(c)

	var query models.MemberListQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		respondBindError(c, ctx, MEMBER_CONTROLLER_ERROR_BASE_CODE+1, "ListMembers", "Query parameter", query, err)
		return
	}

	apiError, members := services.ListMembers(ctx, getSession(c), query)
	if apiError != nil {
		respondApiError(c, apiError)
		return
	}

	respondSuccess(c, http.StatusOK, members)
}

// ListOverdue is the prototype's Overdue tab: expired members, longest-expired first.
func ListOverdue(c *gin.Context) {

	ctx := getContextFromGinContext(c)

	apiError, members := services.ListMembers(ctx, getSession(c), models.MemberListQuery{Status: models.MEMBER_STATUS_OVERDUE})
	if apiError != nil {
		respondApiError(c, apiError)
		return
	}

	respondSuccess(c, http.StatusOK, members)
}

func GetMember(c *gin.Context) {

	ctx := getContextFromGinContext(c)

	var params models.MemberPathParam
	if err := c.ShouldBindUri(&params); err != nil {
		respondBindError(c, ctx, MEMBER_CONTROLLER_ERROR_BASE_CODE+2, "GetMember", "Path parameter", params, err)
		return
	}

	apiError, member := services.GetMemberDetail(ctx, getSession(c), params.MemberId)
	if apiError != nil {
		respondApiError(c, apiError)
		return
	}

	respondSuccess(c, http.StatusOK, member)
}

func CreateMember(c *gin.Context) {

	ctx := getContextFromGinContext(c)

	var input models.CreateMemberRequestBody
	if err := c.ShouldBindJSON(&input); err != nil {
		respondBindError(c, ctx, MEMBER_CONTROLLER_ERROR_BASE_CODE+3, "CreateMember", "Request body", input, err)
		return
	}

	apiError, member := services.CreateMember(ctx, getSession(c), input)
	if apiError != nil {
		respondApiError(c, apiError)
		return
	}

	respondSuccess(c, http.StatusCreated, member)
}

func UpdateMember(c *gin.Context) {

	ctx := getContextFromGinContext(c)

	var params models.MemberPathParam
	if err := c.ShouldBindUri(&params); err != nil {
		respondBindError(c, ctx, MEMBER_CONTROLLER_ERROR_BASE_CODE+4, "UpdateMember", "Path parameter", params, err)
		return
	}

	var input models.UpdateMemberRequestBody
	if err := c.ShouldBindJSON(&input); err != nil {
		respondBindError(c, ctx, MEMBER_CONTROLLER_ERROR_BASE_CODE+5, "UpdateMember", "Request body", input, err)
		return
	}

	apiError, member := services.UpdateMember(ctx, getSession(c), params.MemberId, input)
	if apiError != nil {
		respondApiError(c, apiError)
		return
	}

	respondSuccess(c, http.StatusOK, member)
}

func DeleteMember(c *gin.Context) {

	ctx := getContextFromGinContext(c)

	var params models.MemberPathParam
	if err := c.ShouldBindUri(&params); err != nil {
		respondBindError(c, ctx, MEMBER_CONTROLLER_ERROR_BASE_CODE+6, "DeleteMember", "Path parameter", params, err)
		return
	}

	if apiError := services.DeleteMember(ctx, getSession(c), params.MemberId); apiError != nil {
		respondApiError(c, apiError)
		return
	}

	respondSuccess(c, http.StatusOK, "deleted")
}

func RenewMember(c *gin.Context) {

	ctx := getContextFromGinContext(c)

	var params models.MemberPathParam
	if err := c.ShouldBindUri(&params); err != nil {
		respondBindError(c, ctx, MEMBER_CONTROLLER_ERROR_BASE_CODE+7, "RenewMember", "Path parameter", params, err)
		return
	}

	var input models.RenewMemberRequestBody
	if err := c.ShouldBindJSON(&input); err != nil {
		respondBindError(c, ctx, MEMBER_CONTROLLER_ERROR_BASE_CODE+8, "RenewMember", "Request body", input, err)
		return
	}

	apiError, member := services.RenewMember(ctx, getSession(c), params.MemberId, input)
	if apiError != nil {
		respondApiError(c, apiError)
		return
	}

	respondSuccess(c, http.StatusCreated, member)
}
