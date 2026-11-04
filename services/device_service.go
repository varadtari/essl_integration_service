package services

import (
	"context"
	"net/http"
	"regexp"
	"strings"
	"time"

	"essl_integration_service/database"
	"essl_integration_service/models"
	"essl_integration_service/utils"
)

const (
	DEVICE_SERVICE_BYTE3_VALUE = 0x00280000 //2 = services, 8 = devices
)

var (
	DEVICE_SERVICE_ERROR_BASE_CODE int
)

func init() {
	DEVICE_SERVICE_ERROR_BASE_CODE = utils.ServiceErrorBaseCode + DEVICE_SERVICE_BYTE3_VALUE
}

const COMMAND_HISTORY_LIMIT = 50

var serialPattern = regexp.MustCompile(`^[A-Z0-9-]{4,64}$`)

// normalizeSerial makes a typed serial comparable to what the device sends: label serials are
// upper-case letters and digits, and people add spaces.
func normalizeSerial(raw string) (string, bool) {
	serial := strings.ToUpper(strings.Join(strings.Fields(raw), ""))
	return serial, serialPattern.MatchString(serial)
}

func withOnline(device models.Device, now time.Time) models.Device {
	device.Online = device.LastSeenAt != nil && now.Sub(*device.LastSeenAt) < models.DEVICE_ONLINE_WINDOW
	return device
}

func ListDevices(ctx context.Context, session models.Session) (*models.ApiError, []models.Device) {

	gymId, apiError := requireGym(ctx, session)
	if apiError != nil {
		return apiError, nil
	}

	devices, appError := database.DevDb.ListDevices(ctx, gymId)
	if appError != nil {
		return fromDaoError(ctx, "ListDevices", appError), nil
	}

	now := nowFunc()
	for i := range devices {
		devices[i] = withOnline(devices[i], now)
	}

	return nil, devices
}

func GetDevice(ctx context.Context, session models.Session, deviceId int) (*models.ApiError, *models.Device) {

	gymId, apiError := requireGym(ctx, session)
	if apiError != nil {
		return apiError, nil
	}

	device, appError := database.DevDb.GetDevice(ctx, gymId, deviceId)
	if appError != nil {
		return fromDaoError(ctx, "GetDevice", appError), nil
	}

	result := withOnline(*device, nowFunc())
	return nil, &result
}

// CreateDevice registers a terminal by the serial on its label. Until it is registered its
// requests are refused (and kept in the raw log), so this is also what lets it in.
func CreateDevice(ctx context.Context, session models.Session, input models.CreateDeviceRequestBody) (*models.ApiError, *models.Device) {

	gymId, apiError := requireGym(ctx, session)
	if apiError != nil {
		return apiError, nil
	}
	if apiError := requireOwner(ctx, session); apiError != nil {
		return apiError, nil
	}

	serial, ok := normalizeSerial(input.SerialNumber)
	if !ok {
		return badRequest(ctx, DEVICE_SERVICE_ERROR_BASE_CODE+1, "Enter the serial number exactly as printed on the device label (letters and digits)"), nil
	}

	deviceId, appError := database.DevDb.CreateDevice(ctx, gymId, serial, strings.TrimSpace(input.Name))
	if appError != nil {
		if database.IsConflict(appError) {
			// Deliberately vague: saying which gym has it would leak other gyms' setup.
			appError.Message.DisplayMessage = "This device is already registered. Check the serial number, or contact support if it was moved from another gym."
		}
		return fromDaoError(ctx, "CreateDevice", appError), nil
	}

	// A new device gets every member who should be let in.
	syncGymAccess(ctx, gymId)

	return GetDevice(ctx, session, deviceId)
}

// SyncDeviceMembers forgets what the device was sent and sends every member again, e.g. after
// the device was reset or replaced under the same serial. Returns the number of commands queued.
func SyncDeviceMembers(ctx context.Context, session models.Session, deviceId int) (*models.ApiError, int) {

	gymId, apiError := requireGym(ctx, session)
	if apiError != nil {
		return apiError, 0
	}
	if apiError := requireOwner(ctx, session); apiError != nil {
		return apiError, 0
	}
	if _, appError := database.DevDb.GetDevice(ctx, gymId, deviceId); appError != nil {
		return fromDaoError(ctx, "SyncDeviceMembers", appError), 0
	}

	if appError := database.AccDb.ResetDeviceHashes(ctx, gymId, deviceId); appError != nil {
		return fromDaoError(ctx, "SyncDeviceMembers", appError), 0
	}

	queued, apiError := ReconcileGymAccess(ctx, gymId, 0)
	return apiError, queued
}

func UpdateDevice(ctx context.Context, session models.Session, deviceId int, input models.UpdateDeviceRequestBody) (*models.ApiError, *models.Device) {

	gymId, apiError := requireGym(ctx, session)
	if apiError != nil {
		return apiError, nil
	}
	if apiError := requireOwner(ctx, session); apiError != nil {
		return apiError, nil
	}

	if input.Name != nil {
		trimmed := strings.TrimSpace(*input.Name)
		input.Name = &trimmed
	}

	if appError := database.DevDb.UpdateDevice(ctx, gymId, deviceId, input.Name); appError != nil {
		return fromDaoError(ctx, "UpdateDevice", appError), nil
	}

	return GetDevice(ctx, session, deviceId)
}

func DeleteDevice(ctx context.Context, session models.Session, deviceId int) *models.ApiError {

	gymId, apiError := requireGym(ctx, session)
	if apiError != nil {
		return apiError
	}
	if apiError := requireOwner(ctx, session); apiError != nil {
		return apiError
	}

	if appError := database.DevDb.SoftDeleteDevice(ctx, gymId, deviceId); appError != nil {
		return fromDaoError(ctx, "DeleteDevice", appError)
	}

	return nil
}

// QueueDeviceCommand queues an action for the device's next heartbeat. Front-desk staff may
// open the door; only the owner may reboot or re-pull logs.
func QueueDeviceCommand(ctx context.Context, session models.Session, deviceId int, input models.CreateDeviceCommandRequestBody) (*models.ApiError, *models.DeviceCommand) {

	gymId, apiError := requireGym(ctx, session)
	if apiError != nil {
		return apiError, nil
	}
	if input.Kind != models.COMMAND_OPEN_DOOR {
		if apiError := requireOwner(ctx, session); apiError != nil {
			return apiError, nil
		}
	}

	gym, appError := database.GymDb.GetGym(ctx, gymId)
	if appError != nil {
		return fromDaoError(ctx, "QueueDeviceCommand", appError), nil
	}

	// Confirms the device is this gym's and still registered.
	if _, appError := database.DevDb.GetDevice(ctx, gymId, deviceId); appError != nil {
		return fromDaoError(ctx, "QueueDeviceCommand", appError), nil
	}

	now := nowFunc()
	if input.Kind == models.COMMAND_REPULL_LOGS && (input.From == nil || !input.From.Before(now)) {
		return badRequest(ctx, DEVICE_SERVICE_ERROR_BASE_CODE+2, "Choose a start time in the past to re-pull logs from"), nil
	}

	text, err := commandText(input.Kind, now, gym.Timezone, input.From)
	if err != nil {
		return utils.RenderApiErrorV2(ctx, http.StatusBadRequest, DEVICE_SERVICE_ERROR_BASE_CODE+3, err.Error(), "Unsupported command", nil), nil
	}

	commandId, appError := database.CmdDb.QueueCommand(ctx, gymId, deviceId, input.Kind, text, &session.UserId)
	if appError != nil {
		return fromDaoError(ctx, "QueueDeviceCommand", appError), nil
	}

	return nil, &models.DeviceCommand{Id: commandId, Kind: input.Kind, Command: text, Status: models.COMMAND_STATUS_QUEUED, CreatedAt: now, RequestedBy: &session.UserId}
}

func ListDeviceCommands(ctx context.Context, session models.Session, deviceId int) (*models.ApiError, []models.DeviceCommand) {

	gymId, apiError := requireGym(ctx, session)
	if apiError != nil {
		return apiError, nil
	}

	if _, appError := database.DevDb.GetDevice(ctx, gymId, deviceId); appError != nil {
		return fromDaoError(ctx, "ListDeviceCommands", appError), nil
	}

	commands, appError := database.CmdDb.ListCommands(ctx, gymId, deviceId, COMMAND_HISTORY_LIMIT)
	if appError != nil {
		return fromDaoError(ctx, "ListDeviceCommands", appError), nil
	}

	return nil, commands
}
