package database

import (
	"cmp"
	"context"
	"slices"
	"time"

	"essl_integration_service/models"
	"essl_integration_service/utils"

	"github.com/jackc/pgx/v5"
)

type deviceCommandDb struct{}

type deviceCommandDbInterface interface {
	QueueCommand(ctx context.Context, gymId int, deviceId int, kind string, command string, requestedBy *int) (commandId int64, appError *models.ApplicationError)
	TakePendingCommands(ctx context.Context, deviceId int, limit int, resendAfter time.Duration, maxAttempts int) (commands []models.PendingCommand, appError *models.ApplicationError)
	CompleteCommands(ctx context.Context, deviceId int, results []models.DeviceCommandResult) (updated int, appError *models.ApplicationError)
	ListCommands(ctx context.Context, gymId int, deviceId int, limit int) (commands []models.DeviceCommand, appError *models.ApplicationError)
}

var CmdDb deviceCommandDbInterface

const (
	DEVICE_COMMANDS_DAO_BYTE3_VALUE = 0x00390000 //3 = dao, 9 = device commands
)

var (
	DEVICE_COMMANDS_DAO_ERROR_BASE_CODE int
)

func init() {
	CmdDb = &deviceCommandDb{}
	DEVICE_COMMANDS_DAO_ERROR_BASE_CODE = utils.ServiceErrorBaseCode + DEVICE_COMMANDS_DAO_BYTE3_VALUE
}

func (d *deviceCommandDb) QueueCommand(ctx context.Context, gymId int, deviceId int, kind string, command string, requestedBy *int) (commandId int64, appError *models.ApplicationError) {

	err := dbPool.QueryRow(ctx, `INSERT INTO device_commands ("gymId","deviceId","kind","command","requestedBy") VALUES ($1,$2,$3,$4,$5) RETURNING id`,
		gymId, deviceId, kind, command, requestedBy).Scan(&commandId)
	if err != nil {
		return 0, renderDaoError(ctx, DEVICE_COMMANDS_DAO_ERROR_BASE_CODE+1, "QueueCommand", err, "Could not queue command")
	}

	return commandId, nil
}

// TakePendingCommands hands a polling device its queued commands, plus any it was sent but
// never answered once resendAfter has passed, oldest first. A command that has already been
// sent maxAttempts times is failed instead of sent again. SKIP LOCKED keeps two overlapping
// polls from handing out the same command.
func (d *deviceCommandDb) TakePendingCommands(ctx context.Context, deviceId int, limit int, resendAfter time.Duration, maxAttempts int) (commands []models.PendingCommand, appError *models.ApplicationError) {

	err := withTx(ctx, func(tx pgx.Tx) error {

		if _, err := tx.Exec(ctx, `
UPDATE device_commands SET "status"='failed', "finishedAt"=NOW()
 WHERE "deviceId"=$1 AND "status"='sent' AND "attempts" >= $2 AND "sentAt" < NOW() - $3::interval`,
			deviceId, maxAttempts, resendAfter.String()); err != nil {
			return err
		}

		rows, err := tx.Query(ctx, `
UPDATE device_commands SET "status"='sent', "attempts"="attempts"+1, "sentAt"=NOW()
 WHERE id IN (
   SELECT id FROM device_commands
    WHERE "deviceId"=$1
      AND ("status"='queued' OR ("status"='sent' AND "sentAt" < NOW() - $3::interval AND "attempts" < $4))
    ORDER BY id
    LIMIT $2
    FOR UPDATE SKIP LOCKED)
RETURNING id, "command"`, deviceId, limit, resendAfter.String(), maxAttempts)
		if err != nil {
			return err
		}

		commands, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (models.PendingCommand, error) {
			var c models.PendingCommand
			err := row.Scan(&c.Id, &c.Command)
			return c, err
		})
		return err
	})

	if err != nil {
		return nil, renderDaoError(ctx, DEVICE_COMMANDS_DAO_ERROR_BASE_CODE+2, "TakePendingCommands", err, "Could not read commands")
	}

	// Oldest first matters: a block issued after an unblock must reach the device second, and
	// UPDATE ... RETURNING does not promise any order.
	slices.SortFunc(commands, func(a, b models.PendingCommand) int { return cmp.Compare(a.Id, b.Id) })
	return commands, nil
}

// CompleteCommands applies a device's results. The deviceId condition means a device can only
// acknowledge commands that were queued for it.
func (d *deviceCommandDb) CompleteCommands(ctx context.Context, deviceId int, results []models.DeviceCommandResult) (updated int, appError *models.ApplicationError) {

	err := withTx(ctx, func(tx pgx.Tx) error {
		for _, result := range results {
			status := models.COMMAND_STATUS_ACKED
			if result.Return < 0 {
				status = models.COMMAND_STATUS_FAILED
			}
			tag, err := tx.Exec(ctx, `
UPDATE device_commands SET "status"=$3, "returnCode"=$4, "finishedAt"=NOW()
 WHERE id=$1 AND "deviceId"=$2 AND "status" IN ('sent','queued')`, result.Id, deviceId, status, result.Return)
			if err != nil {
				return err
			}
			updated += int(tag.RowsAffected())
		}
		return nil
	})

	if err != nil {
		return 0, renderDaoError(ctx, DEVICE_COMMANDS_DAO_ERROR_BASE_CODE+3, "CompleteCommands", err, "Could not record command results")
	}

	return updated, nil
}

func (d *deviceCommandDb) ListCommands(ctx context.Context, gymId int, deviceId int, limit int) (commands []models.DeviceCommand, appError *models.ApplicationError) {

	rows, err := dbPool.Query(ctx, `
SELECT id, "kind", "command", "status", "attempts", "returnCode", "sentAt", "finishedAt", "createdAt", "requestedBy"
  FROM device_commands WHERE "gymId"=$1 AND "deviceId"=$2 ORDER BY id DESC LIMIT $3`, gymId, deviceId, limit)
	if err != nil {
		return nil, renderDaoError(ctx, DEVICE_COMMANDS_DAO_ERROR_BASE_CODE+4, "ListCommands", err, "Could not list commands")
	}

	commands, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (models.DeviceCommand, error) {
		var c models.DeviceCommand
		err := row.Scan(&c.Id, &c.Kind, &c.Command, &c.Status, &c.Attempts, &c.ReturnCode, &c.SentAt, &c.FinishedAt, &c.CreatedAt, &c.RequestedBy)
		return c, err
	})
	if err != nil {
		return nil, renderDaoError(ctx, DEVICE_COMMANDS_DAO_ERROR_BASE_CODE+5, "ListCommands", err, "Could not read commands")
	}

	return commands, nil
}
