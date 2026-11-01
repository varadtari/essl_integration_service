package database

import (
	"context"

	"essl_integration_service/models"
	"essl_integration_service/utils"
)

type rawMessageDb struct{}

type rawMessageDbInterface interface {
	SaveRawMessage(ctx context.Context, message models.RawDeviceMessage) (appError *models.ApplicationError)
	PurgeRawMessages(ctx context.Context) (appError *models.ApplicationError)
}

var RawDb rawMessageDbInterface

const (
	RAW_MESSAGES_DAO_BYTE3_VALUE = 0x003b0000 //3 = dao, b = raw device messages
)

var (
	RAW_MESSAGES_DAO_ERROR_BASE_CODE int
)

func init() {
	RawDb = &rawMessageDb{}
	RAW_MESSAGES_DAO_ERROR_BASE_CODE = utils.ServiceErrorBaseCode + RAW_MESSAGES_DAO_BYTE3_VALUE
}

func (d *rawMessageDb) SaveRawMessage(ctx context.Context, m models.RawDeviceMessage) (appError *models.ApplicationError) {

	_, err := dbPool.Exec(ctx, `
INSERT INTO device_raw_messages ("deviceId","serialNumber","method","path","query","body","bodyBytes","status","response","remoteIp")
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		m.DeviceId, m.SerialNumber, m.Method, m.Path, m.Query, m.Body, m.BodyBytes, m.Status, m.Response, m.RemoteIp)
	if err != nil {
		return renderDaoError(ctx, RAW_MESSAGES_DAO_ERROR_BASE_CODE+1, "SaveRawMessage", err, "Could not store device message")
	}

	return nil
}

func (d *rawMessageDb) PurgeRawMessages(ctx context.Context) (appError *models.ApplicationError) {

	if _, err := dbPool.Exec(ctx, `DELETE FROM device_raw_messages WHERE "createdAt" < NOW() - INTERVAL '30 days'`); err != nil {
		return renderDaoError(ctx, RAW_MESSAGES_DAO_ERROR_BASE_CODE+2, "PurgeRawMessages", err, "Could not purge device messages")
	}

	return nil
}
