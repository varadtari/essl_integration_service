package utils

import (
	"context"

	"essl_integration_service/models"
)

const (
	ServiceErrorBaseCode = 0x55000000
)

func RenderApiErrorFromAppError(statusCode int, appError *models.ApplicationError) *models.ApiError {

	apiError := models.ApiError{
		StatusCode:       statusCode,
		ApplicationError: *appError,
	}

	return &apiError
}

func RenderApiErrorV2(ctx context.Context, statusCode int, errorCode int, errorMessage string, displayMessage string, additonalInfo interface{}) *models.ApiError {

	requestId, _ := ctx.Value(models.CONTEXT_REQUEST_ID_KEY).(string)

	apiError := models.ApiError{
		StatusCode: statusCode,
		ApplicationError: models.ApplicationError{
			Type:      "error",
			RequestId: requestId,
			Message: models.ApplicationErrorMessage{
				ErrorCode:      errorCode,
				ErrorMessage:   errorMessage,
				DisplayMessage: displayMessage,
				AdditionalInfo: additonalInfo,
			},
		},
	}

	return &apiError
}

func RenderAppErrorV2(ctx context.Context, errorCode int, errorMessage string, displayMessage string, additonalInfo interface{}) *models.ApplicationError {

	requestId, _ := ctx.Value(models.CONTEXT_REQUEST_ID_KEY).(string)

	appError := models.ApplicationError{
		Type:      "error",
		RequestId: requestId,
		Message: models.ApplicationErrorMessage{
			ErrorCode:      errorCode,
			ErrorMessage:   errorMessage,
			DisplayMessage: displayMessage,
			AdditionalInfo: additonalInfo,
		},
	}

	return &appError
}
