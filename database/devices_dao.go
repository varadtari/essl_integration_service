package database

import (
	"context"
	"time"

	"essl_integration_service/models"
	"essl_integration_service/utils"

	"github.com/jackc/pgx/v5"
)

type deviceDb struct{}

type deviceDbInterface interface {
	CreateDevice(ctx context.Context, gymId int, serialNumber string, name string) (deviceId int, appError *models.ApplicationError)
	ListDevices(ctx context.Context, gymId int) (devices []models.Device, appError *models.ApplicationError)
	GetDevice(ctx context.Context, gymId int, deviceId int) (device *models.Device, appError *models.ApplicationError)
	UpdateDevice(ctx context.Context, gymId int, deviceId int, name *string) (appError *models.ApplicationError)
	SoftDeleteDevice(ctx context.Context, gymId int, deviceId int) (appError *models.ApplicationError)
	GetAdmsDevice(ctx context.Context, serialNumber string) (device *models.AdmsDevice, appError *models.ApplicationError)
	TouchDevice(ctx context.Context, gymId int, deviceId int, remoteIp string, pushVersion string, onlineWindow time.Duration) (cameOnline bool, appError *models.ApplicationError)
	SaveStamp(ctx context.Context, deviceId int, table string, stamp string) (appError *models.ApplicationError)
	SaveFirmware(ctx context.Context, deviceId int, firmware string) (appError *models.ApplicationError)
}

var DevDb deviceDbInterface

const (
	DEVICES_DAO_BYTE3_VALUE = 0x00370000 //3 = dao, 7 = devices
)

var (
	DEVICES_DAO_ERROR_BASE_CODE int
)

func init() {
	DevDb = &deviceDb{}
	DEVICES_DAO_ERROR_BASE_CODE = utils.ServiceErrorBaseCode + DEVICES_DAO_BYTE3_VALUE
}

func (d *deviceDb) CreateDevice(ctx context.Context, gymId int, serialNumber string, name string) (deviceId int, appError *models.ApplicationError) {

	err := dbPool.QueryRow(ctx, `INSERT INTO devices ("gymId","serialNumber","name") VALUES ($1,$2,$3) RETURNING id`, gymId, serialNumber, name).Scan(&deviceId)
	if err != nil {
		return 0, renderDaoError(ctx, DEVICES_DAO_ERROR_BASE_CODE+1, "CreateDevice", err, "Could not add device")
	}

	return deviceId, nil
}

// deviceSelect adds the queue depth, the latest punch, and — for a device that has never
// connected — when its serial last showed up unregistered in the raw log.
const deviceSelect = `
SELECT d.id, d."serialNumber", d."name", d."firmware", d."pushVersion", d."lastSeenAt", d."lastIp", d."createdAt",
       (SELECT COUNT(*) FROM device_commands c WHERE c."deviceId" = d.id AND c."status" IN ('queued','sent')),
       (SELECT MAX(e."punchedAt") FROM access_events e WHERE e."deviceId" = d.id),
       CASE WHEN d."lastSeenAt" IS NULL THEN
         (SELECT MAX(r."createdAt") FROM device_raw_messages r WHERE UPPER(r."serialNumber") = UPPER(d."serialNumber"))
       END
  FROM devices d`

func scanDevice(row pgx.Row) (models.Device, error) {
	var dev models.Device
	err := row.Scan(&dev.Id, &dev.SerialNumber, &dev.Name, &dev.Firmware, &dev.PushVersion, &dev.LastSeenAt, &dev.LastIp, &dev.CreatedAt,
		&dev.PendingCommands, &dev.LastPunchAt, &dev.SerialLastAttemptAt)
	return dev, err
}

func (d *deviceDb) ListDevices(ctx context.Context, gymId int) (devices []models.Device, appError *models.ApplicationError) {

	rows, err := dbPool.Query(ctx, deviceSelect+` WHERE d."gymId"=$1 AND d."deletedAt" IS NULL ORDER BY d."name", d.id`, gymId)
	if err != nil {
		return nil, renderDaoError(ctx, DEVICES_DAO_ERROR_BASE_CODE+2, "ListDevices", err, "Could not list devices")
	}

	devices, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (models.Device, error) { return scanDevice(row) })
	if err != nil {
		return nil, renderDaoError(ctx, DEVICES_DAO_ERROR_BASE_CODE+3, "ListDevices", err, "Could not read devices")
	}

	return devices, nil
}

func (d *deviceDb) GetDevice(ctx context.Context, gymId int, deviceId int) (device *models.Device, appError *models.ApplicationError) {

	dev, err := scanDevice(dbPool.QueryRow(ctx, deviceSelect+` WHERE d.id=$2 AND d."gymId"=$1 AND d."deletedAt" IS NULL`, gymId, deviceId))
	if err != nil {
		return nil, renderDaoError(ctx, DEVICES_DAO_ERROR_BASE_CODE+4, "GetDevice", err, "Device not found")
	}

	return &dev, nil
}

func (d *deviceDb) UpdateDevice(ctx context.Context, gymId int, deviceId int, name *string) (appError *models.ApplicationError) {

	tag, err := dbPool.Exec(ctx, `UPDATE devices SET "name"=COALESCE($3,"name") WHERE id=$2 AND "gymId"=$1 AND "deletedAt" IS NULL`, gymId, deviceId, name)
	if err == nil && tag.RowsAffected() == 0 {
		err = ErrNotFound
	}
	if err != nil {
		return renderDaoError(ctx, DEVICES_DAO_ERROR_BASE_CODE+5, "UpdateDevice", err, "Could not update device")
	}

	return nil
}

// SoftDeleteDevice retires a device and cancels whatever it had queued, so a serial moved to
// another gym never receives the old gym's commands.
func (d *deviceDb) SoftDeleteDevice(ctx context.Context, gymId int, deviceId int) (appError *models.ApplicationError) {

	err := withTx(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE devices SET "deletedAt"=NOW() WHERE id=$2 AND "gymId"=$1 AND "deletedAt" IS NULL`, gymId, deviceId)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		_, err = tx.Exec(ctx, `UPDATE device_commands SET "status"='failed', "finishedAt"=NOW() WHERE "deviceId"=$1 AND "status" IN ('queued','sent')`, deviceId)
		return err
	})
	if err != nil {
		return renderDaoError(ctx, DEVICES_DAO_ERROR_BASE_CODE+6, "SoftDeleteDevice", err, "Could not remove device")
	}

	return nil
}

// GetAdmsDevice resolves the serial an /iclock request carries to a live device and the gym
// settings ingest needs. Serial matching ignores case: installers type what is on the label.
func (d *deviceDb) GetAdmsDevice(ctx context.Context, serialNumber string) (device *models.AdmsDevice, appError *models.ApplicationError) {

	sqlStatement := `
SELECT d.id, d."gymId", d."serialNumber", g."timezone", g."graceDays", d."attStamp", d."opStamp", d."photoStamp"
  FROM devices d JOIN gyms g ON g.id = d."gymId"
 WHERE UPPER(d."serialNumber") = UPPER($1) AND d."deletedAt" IS NULL`

	var dev models.AdmsDevice
	err := dbPool.QueryRow(ctx, sqlStatement, serialNumber).Scan(&dev.Id, &dev.GymId, &dev.SerialNumber, &dev.Timezone, &dev.GraceDays, &dev.AttStamp, &dev.OpStamp, &dev.PhotoStamp)
	if err != nil {
		return nil, renderDaoError(ctx, DEVICES_DAO_ERROR_BASE_CODE+7, "GetAdmsDevice", err, "Unknown device")
	}

	return &dev, nil
}

// TouchDevice records that the device is alive. It runs on every /iclock request, so it is a
// single-row update. When the device had been silent for longer than onlineWindow (or never
// connected), it also announces that the device came online.
func (d *deviceDb) TouchDevice(ctx context.Context, gymId int, deviceId int, remoteIp string, pushVersion string, onlineWindow time.Duration) (cameOnline bool, appError *models.ApplicationError) {

	err := withTx(ctx, func(tx pgx.Tx) error {
		var previous *time.Time
		err := tx.QueryRow(ctx, `
UPDATE devices d SET "lastSeenAt"=NOW(), "lastIp"=$2, "pushVersion"=COALESCE(NULLIF($3,''),d."pushVersion")
  FROM (SELECT "lastSeenAt" FROM devices WHERE id=$1 FOR UPDATE) prev
 WHERE d.id=$1
RETURNING prev."lastSeenAt"`, deviceId, remoteIp, pushVersion).Scan(&previous)
		if err != nil {
			return err
		}

		cameOnline = previous == nil || time.Since(*previous) > onlineWindow
		if !cameOnline {
			return nil
		}
		return notifyTx(ctx, tx, models.LiveEvent{Type: models.LIVE_EVENT_DEVICE, GymId: gymId, DeviceId: deviceId})
	})

	if err != nil {
		return false, renderDaoError(ctx, DEVICES_DAO_ERROR_BASE_CODE+8, "TouchDevice", err, "Could not update device")
	}

	return cameOnline, nil
}

var stampColumns = map[string]string{"ATTLOG": `"attStamp"`, "OPERLOG": `"opStamp"`, "ATTPHOTO": `"photoStamp"`}

// SaveStamp stores the upload cursor a device reported with a batch, once the batch is stored.
func (d *deviceDb) SaveStamp(ctx context.Context, deviceId int, table string, stamp string) (appError *models.ApplicationError) {

	column, ok := stampColumns[table]
	if !ok || stamp == "" {
		return nil
	}

	if _, err := dbPool.Exec(ctx, `UPDATE devices SET `+column+`=$2 WHERE id=$1`, deviceId, stamp); err != nil {
		return renderDaoError(ctx, DEVICES_DAO_ERROR_BASE_CODE+9, "SaveStamp", err, "Could not update device")
	}

	return nil
}

func (d *deviceDb) SaveFirmware(ctx context.Context, deviceId int, firmware string) (appError *models.ApplicationError) {

	if _, err := dbPool.Exec(ctx, `UPDATE devices SET "firmware"=$2 WHERE id=$1`, deviceId, firmware); err != nil {
		return renderDaoError(ctx, DEVICES_DAO_ERROR_BASE_CODE+10, "SaveFirmware", err, "Could not update device")
	}

	return nil
}
