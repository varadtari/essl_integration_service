package models

import "time"

type Payment struct {
	Id           int       `json:"id"`
	MemberId     int       `json:"memberId"`
	MemberName   string    `json:"memberName"`
	MembershipId int       `json:"membershipId"`
	AmountPaise  int64     `json:"amountPaise"`
	Method       string    `json:"method"`
	PaidAt       time.Time `json:"paidAt"`
}

type PaymentListQuery struct {
	Month string `form:"month" binding:"omitempty,datetime=2006-01"`
}

type PaymentListResponse struct {
	Month      string    `json:"month"`
	TotalPaise int64     `json:"totalPaise"`
	Payments   []Payment `json:"payments"`
}

type DashboardSummary struct {
	Today                 string    `json:"today"`
	ActiveMembers         int       `json:"activeMembers"`
	OverdueMembers        int       `json:"overdueMembers"`
	RevenueThisMonthPaise int64     `json:"revenueThisMonthPaise"`
	RecentPayments        []Payment `json:"recentPayments"`
}
