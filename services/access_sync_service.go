package services

import (
	"context"
	"strconv"
	"time"

	"essl_integration_service/database"
	"essl_integration_service/logger"
	"essl_integration_service/models"

	"go.uber.org/zap"
)

// MAX_ACCESS_FAILURES is how many times a (member, device) command may fail before the reconciler
// stops retrying it and the members list shows an error. A change in what is wanted (e.g. a
// renewal) starts the count again.
const MAX_ACCESS_FAILURES = 5

// ACCESS_SWEEP_INTERVAL is how often every gym is reconciled: expiries take effect within this
// long of midnight, and anything a crash or outage left behind is caught up. A variable so
// tests can run sweeps directly.
var ACCESS_SWEEP_INTERVAL = 5 * time.Minute

func commandInFlight(a *models.DeviceAccess) bool {
	return a != nil && a.LastCommandStatus != nil &&
		(*a.LastCommandStatus == models.COMMAND_STATUS_QUEUED || *a.LastCommandStatus == models.COMMAND_STATUS_SENT)
}

// ReconcileGymAccess brings the gym's devices in line with its memberships, for one member when
// memberId > 0. It returns how many commands it queued. Errors on one pair are logged and the
// rest carry on.
func ReconcileGymAccess(ctx context.Context, gymId int, memberId int) (int, *models.ApiError) {

	gym, appError := database.GymDb.GetGym(ctx, gymId)
	if appError != nil {
		return 0, fromDaoError(ctx, "ReconcileGymAccess", appError)
	}

	state, appError := database.AccDb.LoadAccessState(ctx, gymId, memberId)
	if appError != nil {
		return 0, fromDaoError(ctx, "ReconcileGymAccess", appError)
	}

	today := gymToday(nowFunc(), gym.Timezone)
	queued := 0

	for _, member := range state.Members {
		desired := desiredAccess(member, *gym, today)

		for _, deviceId := range state.DeviceIds {
			var current *models.DeviceAccess
			if a, ok := state.Access[[2]int{member.Id, deviceId}]; ok {
				current = &a
			}

			plan := planAccess(member, desired, current, state.Templates[member.Id], termValidity(member, *gym))
			if plan == nil {
				continue
			}

			if current != nil && current.DesiredState == desired {
				// Already on its way: the same state was asked for and the device has not answered.
				sameRecord := plan.PayloadHash == "" || (current.PayloadHash != nil && *current.PayloadHash == plan.PayloadHash)
				if commandInFlight(current) && sameRecord {
					continue
				}
				// The device keeps refusing this; leave it for a person to look at.
				if current.FailedAttempts >= MAX_ACCESS_FAILURES && sameRecord {
					continue
				}
			}

			if appError := database.AccDb.ApplyAccessPlan(ctx, gymId, member.Id, deviceId, desired, plan.PayloadHash, plan.Commands); appError != nil {
				logger.Log.Error("access_apply_failed", zap.Int("gymId", gymId), zap.Int("memberId", member.Id), zap.Int("deviceId", deviceId), zap.String("error", appError.Message.ErrorMessage))
				continue
			}
			queued += len(plan.Commands)
		}
	}

	if queued > 0 {
		logger.Log.Info("access_reconciled", zap.Int("gymId", gymId), zap.Int("memberId", memberId), zap.Int("commandsQueued", queued))
	}
	return queued, nil
}

// syncMemberAccess runs after a change to one member. It never fails the request that caused
// it: the periodic sweep retries whatever this could not queue.
func syncMemberAccess(ctx context.Context, gymId int, memberId int) {
	if _, apiError := ReconcileGymAccess(context.WithoutCancel(ctx), gymId, memberId); apiError != nil {
		logger.Log.Error("access_sync_failed", zap.Int("gymId", gymId), zap.Int("memberId", memberId), zap.String("error", apiError.ApplicationError.Message.ErrorMessage))
	}
}

func syncGymAccess(ctx context.Context, gymId int) {
	syncMemberAccess(ctx, gymId, 0)
}

// SweepAllGyms reconciles every gym once.
//
// Run by one process: two instances sweeping at once could queue a command twice (harmless to
// the device, but noisy). The deployment runs a single api container.
func SweepAllGyms(ctx context.Context) {

	gymIds, appError := database.AccDb.ListGymIds(ctx)
	if appError != nil {
		logger.Log.Error("access_sweep_failed", zap.String("error", appError.Message.ErrorMessage))
		return
	}

	for _, gymId := range gymIds {
		if ctx.Err() != nil {
			return
		}
		syncGymAccess(ctx, gymId)
	}
}

// StartAccessScheduler sweeps now and then every ACCESS_SWEEP_INTERVAL until ctx ends.
func StartAccessScheduler(ctx context.Context) {
	SweepAllGyms(ctx)
	ticker := time.NewTicker(ACCESS_SWEEP_INTERVAL)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			SweepAllGyms(ctx)
		}
	}
}

// storeUploadedTemplates keeps the biometric records of an OPERLOG upload so a member removed
// on expiry can be restored on renewal without enrolling again.
func storeUploadedTemplates(ctx context.Context, device models.AdmsDevice, body string) {

	byPin := map[string][]models.BiometricTemplateRow{}
	for _, t := range parseOperLogTemplates(body) {
		if _, err := strconv.Atoi(t.Pin); err != nil {
			continue
		}
		byPin[t.Pin] = append(byPin[t.Pin], models.BiometricTemplateRow{Kind: t.Kind, Slot: t.Slot, Line: t.Line})
	}
	if len(byPin) == 0 {
		return
	}

	saved, appError := database.AccDb.SaveTemplates(ctx, device.GymId, device.Id, byPin)
	if appError != nil {
		logger.Log.Error("templates_store_failed", zap.String("serial", device.SerialNumber), zap.String("error", appError.Message.ErrorMessage))
		return
	}
	logger.Log.Info("templates_stored", zap.String("serial", device.SerialNumber), zap.Int("saved", saved))
}

// summarizeAccess reduces a member's rows across the gym's devices to one label for the list.
func summarizeAccess(rows []models.DeviceAccess, devices int) models.MemberAccessSummary {

	summary := models.MemberAccessSummary{Devices: devices}
	if devices == 0 {
		summary.State = models.ACCESS_SUMMARY_NO_DEVICES
		return summary
	}

	allActive, allOff, syncing := true, true, len(rows) < devices
	for _, r := range rows {
		if r.FailedAttempts >= MAX_ACCESS_FAILURES {
			summary.State, summary.Error = models.ACCESS_SUMMARY_ERROR, r.LastError
			return summary
		}
		if r.DesiredState != r.ActualState || commandInFlight(&r) {
			syncing = true
		}
		allActive = allActive && r.ActualState == models.ACCESS_ACTIVE
		allOff = allOff && r.ActualState != models.ACCESS_ACTIVE
	}

	switch {
	case syncing:
		summary.State = models.ACCESS_SUMMARY_SYNCING
	case allActive:
		summary.State = models.ACCESS_SUMMARY_ON_DEVICE
	case allOff && len(rows) > 0 && rows[0].DesiredState == models.ACCESS_BLOCKED:
		summary.State = models.ACCESS_SUMMARY_BLOCKED
	default:
		summary.State = models.ACCESS_SUMMARY_NOT_ON
	}
	return summary
}
