package misc

import (
	"context"
	"fmt"

	"essl_integration_service/database"
	"essl_integration_service/logger"
	"essl_integration_service/models"
)

func ProcessError(ctx context.Context, priority int, errorMessage string, additionalInfo interface{}) {

	var additionalInfoToWriteToDb string

	switch value := additionalInfo.(type) {

	case []byte:
		additionalInfoToWriteToDb = string(value)

	case *models.ApplicationError:
		additionalInfoToWriteToDb = fmt.Sprintf("ApplicationErrorMessage Details-ErrorCode:%d,ErrorMessage:%s", value.Message.ErrorCode, value.Message.ErrorMessage)

	case *models.ApiError:
		additionalInfoToWriteToDb = fmt.Sprintf("ApiErrorMessage Details-StatusCode:%d,ErrorCode:%d,ErrorMessage:%s", value.StatusCode, value.ApplicationError.Message.ErrorCode, value.ApplicationError.Message.ErrorMessage)

	case nil:
		additionalInfoToWriteToDb = ""

	default:
		errorMsg := fmt.Sprintf("Could not process ProcessError message as type of data interface is not supported.Data interface type:%T,Message priority:%d,Message error:%s,Message data:%v", value, priority, errorMessage, additionalInfo)
		logger.Log.Error(errorMsg)
		return

	}

	appError := database.ErDb.ProcessErrorMessages(ctx, priority, errorMessage, additionalInfoToWriteToDb)

	if appError != nil {
		errorMsg := fmt.Sprintf("Failed to write ProcessError message to database.Message priority:%d,Message error:%s,Message data:%v", priority, errorMessage, additionalInfo)
		logger.Log.Error(errorMsg)
	}

}
