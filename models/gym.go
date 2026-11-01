package models

type Gym struct {
	Id            int    `json:"id"`
	Name          string `json:"name"`
	Timezone      string `json:"timezone"`
	GraceDays     int    `json:"graceDays"`
	BlockOnExpiry bool   `json:"blockOnExpiry"`
	BillingStatus string `json:"billingStatus"`
}

type GymPathParam struct {
	GymId int `uri:"gymId" binding:"required,min=1"`
}

type CreateGymRequestBody struct {
	Name     string                `json:"name" binding:"required,min=1,max=120"`
	Timezone string                `json:"timezone" binding:"omitempty,max=64"`
	Owner    CreateUserRequestBody `json:"owner" binding:"required"`
}

type CreateGymResponse struct {
	Gym   Gym          `json:"gym"`
	Owner UserResponse `json:"owner"`
}

// UpdateGymSettingsRequestBody is what a gym owner may change about their own gym.
type UpdateGymSettingsRequestBody struct {
	Name          *string `json:"name" binding:"omitempty,min=1,max=120"`
	Timezone      *string `json:"timezone" binding:"omitempty,max=64"`
	GraceDays     *int    `json:"graceDays" binding:"omitempty,min=0,max=60"`
	BlockOnExpiry *bool   `json:"blockOnExpiry"`
}

// UpdateGymAdminRequestBody adds the fields only a superadmin may change.
type UpdateGymAdminRequestBody struct {
	UpdateGymSettingsRequestBody
	BillingStatus *string `json:"billingStatus" binding:"omitempty,oneof=trial paid unpaid"`
}

type AdminGymListItem struct {
	Gym
	OwnerEmail    *string `json:"ownerEmail"`
	ActiveMembers int     `json:"activeMembers"`
}
