package controllers

import (
	"net/http"

	"essl_integration_service/models"
	"essl_integration_service/services"
	"essl_integration_service/utils"

	"github.com/gin-gonic/gin"
)

const (
	DEVICE_CONTROLLER_BYTE3_VALUE = 0x00180000 //1 = controller, 8 = devices
)

var (
	DEVICE_CONTROLLER_ERROR_BASE_CODE int
)

func init() {
	DEVICE_CONTROLLER_ERROR_BASE_CODE = utils.ServiceErrorBaseCode + DEVICE_CONTROLLER_BYTE3_VALUE
}

func bindDevicePath(c *gin.Context, operation string, code int) (models.DevicePathParam, bool) {
	var params models.DevicePathParam
	if err := c.ShouldBindUri(&params); err != nil {
		respondBindError(c, getContextFromGinContext(c), code, operation, "Path parameter", params, err)
		return params, false
	}
	return params, true
}

func ListDevices(c *gin.Context) {

	apiError, devices := services.ListDevices(getContextFromGinContext(c), getSession(c))
	if apiError != nil {
		respondApiError(c, apiError)
		return
	}

	respondSuccess(c, http.StatusOK, devices)
}

func GetDevice(c *gin.Context) {

	params, ok := bindDevicePath(c, "GetDevice", DEVICE_CONTROLLER_ERROR_BASE_CODE+1)
	if !ok {
		return
	}

	apiError, device := services.GetDevice(getContextFromGinContext(c), getSession(c), params.DeviceId)
	if apiError != nil {
		respondApiError(c, apiError)
		return
	}

	respondSuccess(c, http.StatusOK, device)
}

func CreateDevice(c *gin.Context) {

	ctx := getContextFromGinContext(c)

	var input models.CreateDeviceRequestBody
	if err := c.ShouldBindJSON(&input); err != nil {
		respondBindError(c, ctx, DEVICE_CONTROLLER_ERROR_BASE_CODE+2, "CreateDevice", "Request body", input, err)
		return
	}

	apiError, device := services.CreateDevice(ctx, getSession(c), input)
	if apiError != nil {
		respondApiError(c, apiError)
		return
	}

	respondSuccess(c, http.StatusCreated, device)
}

func UpdateDevice(c *gin.Context) {

	ctx := getContextFromGinContext(c)
	params, ok := bindDevicePath(c, "UpdateDevice", DEVICE_CONTROLLER_ERROR_BASE_CODE+3)
	if !ok {
		return
	}

	var input models.UpdateDeviceRequestBody
	if err := c.ShouldBindJSON(&input); err != nil {
		respondBindError(c, ctx, DEVICE_CONTROLLER_ERROR_BASE_CODE+4, "UpdateDevice", "Request body", input, err)
		return
	}

	apiError, device := services.UpdateDevice(ctx, getSession(c), params.DeviceId, input)
	if apiError != nil {
		respondApiError(c, apiError)
		return
	}

	respondSuccess(c, http.StatusOK, device)
}

func DeleteDevice(c *gin.Context) {

	params, ok := bindDevicePath(c, "DeleteDevice", DEVICE_CONTROLLER_ERROR_BASE_CODE+5)
	if !ok {
		return
	}

	if apiError := services.DeleteDevice(getContextFromGinContext(c), getSession(c), params.DeviceId); apiError != nil {
		respondApiError(c, apiError)
		return
	}

	respondSuccess(c, http.StatusOK, "removed")
}

func CreateDeviceCommand(c *gin.Context) {

	ctx := getContextFromGinContext(c)
	params, ok := bindDevicePath(c, "CreateDeviceCommand", DEVICE_CONTROLLER_ERROR_BASE_CODE+6)
	if !ok {
		return
	}

	var input models.CreateDeviceCommandRequestBody
	if err := c.ShouldBindJSON(&input); err != nil {
		respondBindError(c, ctx, DEVICE_CONTROLLER_ERROR_BASE_CODE+7, "CreateDeviceCommand", "Request body", input, err)
		return
	}

	apiError, command := services.QueueDeviceCommand(ctx, getSession(c), params.DeviceId, input)
	if apiError != nil {
		respondApiError(c, apiError)
		return
	}

	respondSuccess(c, http.StatusCreated, command)
}

func ListDeviceCommands(c *gin.Context) {

	params, ok := bindDevicePath(c, "ListDeviceCommands", DEVICE_CONTROLLER_ERROR_BASE_CODE+8)
	if !ok {
		return
	}

	apiError, commands := services.ListDeviceCommands(getContextFromGinContext(c), getSession(c), params.DeviceId)
	if apiError != nil {
		respondApiError(c, apiError)
		return
	}

	respondSuccess(c, http.StatusOK, commands)
}

func SyncDeviceMembers(c *gin.Context) {

	params, ok := bindDevicePath(c, "SyncDeviceMembers", DEVICE_CONTROLLER_ERROR_BASE_CODE+10)
	if !ok {
		return
	}

	apiError, queued := services.SyncDeviceMembers(getContextFromGinContext(c), getSession(c), params.DeviceId)
	if apiError != nil {
		respondApiError(c, apiError)
		return
	}

	respondSuccess(c, http.StatusOK, map[string]int{"commandsQueued": queued})
}

func ListCheckins(c *gin.Context) {

	ctx := getContextFromGinContext(c)

	var query models.CheckinListQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		respondBindError(c, ctx, DEVICE_CONTROLLER_ERROR_BASE_CODE+9, "ListCheckins", "Query parameter", query, err)
		return
	}

	apiError, page := services.ListCheckins(ctx, getSession(c), query)
	if apiError != nil {
		respondApiError(c, apiError)
		return
	}

	respondSuccess(c, http.StatusOK, page)
}
