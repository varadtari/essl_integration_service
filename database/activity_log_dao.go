package database

import (
	"context"
	"fmt"

	"essl_integration_service/logger"
	"essl_integration_service/models"
	"essl_integration_service/utils"
)

type alDb struct{}

type alDbInterface interface {
	DeleteDebugApiActivityLogs(ctx context.Context) *models.ApplicationError
	SaveDebugApiActivityLog(ctx context.Context, xRequestId string, method string, path string, headers interface{}, requestBody interface{}, status int, response interface{}, latency int64, userAgent string) *models.ApplicationError
}

var AlDb alDbInterface

const (
	ACTIVITY_LOG_DAO_BYTE3_VALUE = 0x00380000 //3 = dao, 8 = activity log
)

var (
	ACTIVITY_LOG_DAO_ERROR_BASE_CODE int
)

func init() {
	AlDb = &alDb{}
	ACTIVITY_LOG_DAO_ERROR_BASE_CODE = utils.ServiceErrorBaseCode + ACTIVITY_LOG_DAO_BYTE3_VALUE
}

func (d *alDb) DeleteDebugApiActivityLogs(ctx context.Context) *models.ApplicationError {

	sqlStatement := `DELETE FROM debug_activity_logs_api where "createdAt" < NOW() - INTERVAL '30 days'`

	_, err := dbPool.Exec(ctx, sqlStatement)

	if err != nil {
		errMsg := fmt.Sprintf("DeleteDebugApiActivityLogs:Could not delete from debug api activity logs database table.Error:%s!", err.Error())
		displayMsg := "Could not delete from debug api activity logs database table!"
		logger.Log.Error(errMsg)
		appError := utils.RenderAppErrorV2(ctx, ACTIVITY_LOG_DAO_ERROR_BASE_CODE+4, errMsg, displayMsg, nil)
		return appError
	}

	return nil

}

func (d *alDb) SaveDebugApiActivityLog(ctx context.Context, xRequestId string, method string, path string, headers interface{}, requestBody interface{}, status int, response interface{}, latency int64, userAgent string) *models.ApplicationError {

	sqlStatement := `insert into debug_activity_logs_api
("xRequestId","method","path","headers","requestBody","status","response","latency","userAgent")
values ($1,$2,$3,$4,$5,$6,$7,$8,$9)`

	_, err := dbPool.Exec(ctx, sqlStatement, xRequestId, method, path, headers, requestBody, status, response, latency, userAgent)

	if err != nil {
		errMsg := fmt.Sprintf("SaveDebugApiActivityLog:Could not write to debug api activity logs database table.Error:%s!", err.Error())
		displayMsg := "Could not write to debug api activity logs database table!"
		logger.Log.Error(errMsg)
		appError := utils.RenderAppErrorV2(ctx, ACTIVITY_LOG_DAO_ERROR_BASE_CODE+5, errMsg, displayMsg, nil)
		return appError
	}

	return nil
}
