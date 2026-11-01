package services

import (
	"context"

	"essl_integration_service/database"
	"essl_integration_service/models"
)

const RECENT_PAYMENTS_LIMIT = 5

// ListPayments returns one calendar month of payments at the gym, the current month by default.
func ListPayments(ctx context.Context, session models.Session, query models.PaymentListQuery) (*models.ApiError, *models.PaymentListResponse) {

	gymId, apiError := requireGym(ctx, session)
	if apiError != nil {
		return apiError, nil
	}

	gym, appError := database.GymDb.GetGym(ctx, gymId)
	if appError != nil {
		return fromDaoError(ctx, "ListPayments", appError), nil
	}

	month := query.Month
	if month == "" {
		month = gymToday(nowFunc(), gym.Timezone).Format("2006-01")
	}

	from, to, err := monthRange(month, gym.Timezone)
	if err != nil {
		return badRequest(ctx, COMMON_SERVICE_ERROR_BASE_CODE+3, "month must be YYYY-MM"), nil
	}

	payments, appError := database.PayDb.ListPayments(ctx, gymId, from, to)
	if appError != nil {
		return fromDaoError(ctx, "ListPayments", appError), nil
	}

	var total int64
	for _, p := range payments {
		total += p.AmountPaise
	}

	return nil, &models.PaymentListResponse{Month: month, TotalPaise: total, Payments: payments}
}

// GetDashboardSummary backs the prototype's three cards and the recent payments list.
func GetDashboardSummary(ctx context.Context, session models.Session) (*models.ApiError, *models.DashboardSummary) {

	gymId, apiError := requireGym(ctx, session)
	if apiError != nil {
		return apiError, nil
	}

	gym, appError := database.GymDb.GetGym(ctx, gymId)
	if appError != nil {
		return fromDaoError(ctx, "GetDashboardSummary", appError), nil
	}

	today := gymToday(nowFunc(), gym.Timezone)

	active, overdue, appError := database.MemDb.CountMembersByStatus(ctx, gymId, formatDate(today))
	if appError != nil {
		return fromDaoError(ctx, "GetDashboardSummary", appError), nil
	}

	from, to, _ := monthRange(today.Format("2006-01"), gym.Timezone)
	revenue, appError := database.PayDb.SumPayments(ctx, gymId, from, to)
	if appError != nil {
		return fromDaoError(ctx, "GetDashboardSummary", appError), nil
	}

	recent, appError := database.PayDb.RecentPayments(ctx, gymId, RECENT_PAYMENTS_LIMIT)
	if appError != nil {
		return fromDaoError(ctx, "GetDashboardSummary", appError), nil
	}

	return nil, &models.DashboardSummary{
		Today:                 formatDate(today),
		ActiveMembers:         active,
		OverdueMembers:        overdue,
		RevenueThisMonthPaise: revenue,
		RecentPayments:        recent,
	}
}
