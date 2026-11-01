package database

import (
	"context"

	"fmt"

	"essl_integration_service/logger"
	"essl_integration_service/models"
	"essl_integration_service/utils"
)

type errorDb struct{}

type errorDbInterface interface {
	ProcessErrorMessages(ctx context.Context, priority int, errorMessage string, moreInfo string) *models.ApplicationError
}

var ErDb errorDbInterface

const (
	ERROR_DAO_BYTE3_VALUE = 0x00340000 //3 = dao, 4 = errors
)

var (
	ERROR_DAO_ERROR_BASE_CODE int
)

func init() {
	ErDb = &errorDb{}
	ERROR_DAO_ERROR_BASE_CODE = utils.ServiceErrorBaseCode + ERROR_DAO_BYTE3_VALUE

}

func (d *errorDb) ProcessErrorMessages(ctx context.Context, priority int, errorMessage string, additionalInfo string) *models.ApplicationError {

	sqlStatement := `insert into internal_errors ("priority","errorMessage","additionalInfo") values ($1,$2,$3)`
	_, err := dbPool.Exec(context.Background(), sqlStatement, priority, errorMessage, additionalInfo)

	if err != nil {
		errMsg := fmt.Sprintf("ProcessErrorMessages:Could not write to errors table.Error:%s", err.Error())
		displayMsg := "Could not write to errors table!"
		logger.Log.Error(err.Error())
		appError := utils.RenderAppErrorV2(ctx, ERROR_DAO_ERROR_BASE_CODE+2, errMsg, displayMsg, nil)
		return appError
	}

	return nil

}
