package models

type MemberPathParam struct {
	MemberId int `uri:"memberId" binding:"required,min=1"`
}

type MemberListQuery struct {
	Search string `form:"search" binding:"omitempty,max=120"`
	Status string `form:"status" binding:"omitempty,oneof=active overdue"`
}

// MemberRow is a member joined with their latest membership term, as the DAO returns it.
type MemberRow struct {
	Id        int
	DevicePin int
	Name      string
	Phone     string
	StartDate *string
	EndDate   *string
}

type MemberListItem struct {
	Id        int     `json:"id"`
	DevicePin int     `json:"devicePin"`
	Name      string  `json:"name"`
	Phone     string  `json:"phone"`
	StartDate *string `json:"startDate"`
	EndDate   *string `json:"endDate"`
	Status    string  `json:"status"`
	// DaysToExpiry is negative once expired: -3 means it expired three days ago.
	DaysToExpiry *int `json:"daysToExpiry"`
	// Access is whether the gym's devices let the member in (list endpoint only).
	Access *MemberAccessSummary `json:"access,omitempty"`
}

type CreateMemberRequestBody struct {
	Name   string `json:"name" binding:"required,min=1,max=120"`
	Phone  string `json:"phone" binding:"required,max=20"`
	PlanId int    `json:"planId" binding:"required,min=1"`
	Method string `json:"method" binding:"required,oneof=cash upi"`
}

type UpdateMemberRequestBody struct {
	Name  *string `json:"name" binding:"omitempty,min=1,max=120"`
	Phone *string `json:"phone" binding:"omitempty,max=20"`
}

type RenewMemberRequestBody struct {
	PlanId int    `json:"planId" binding:"required,min=1"`
	Method string `json:"method" binding:"required,oneof=cash upi"`
}

// NewTerm is a membership term plus the payment that bought it, written in one transaction.
type NewTerm struct {
	PlanId      int
	StartDate   string
	EndDate     string
	AmountPaise int64
	Method      string
	RecordedBy  int
}

type Membership struct {
	Id        int    `json:"id"`
	PlanId    int    `json:"planId"`
	PlanName  string `json:"planName"`
	StartDate string `json:"startDate"`
	EndDate   string `json:"endDate"`
}

type MemberDetail struct {
	MemberListItem
	Memberships []Membership `json:"memberships"`
	Payments    []Payment    `json:"payments"`
}
