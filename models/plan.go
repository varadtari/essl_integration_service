package models

type Plan struct {
	Id         int    `json:"id"`
	Name       string `json:"name"`
	Months     int    `json:"months"`
	PricePaise int64  `json:"pricePaise"`
	Active     bool   `json:"active"`
}

type PlanPathParam struct {
	PlanId int `uri:"planId" binding:"required,min=1"`
}

type CreatePlanRequestBody struct {
	Name       string `json:"name" binding:"required,min=1,max=60"`
	Months     int    `json:"months" binding:"required,min=1,max=36"`
	PricePaise int64  `json:"pricePaise" binding:"min=0,max=10000000"`
}

type UpdatePlanRequestBody struct {
	Name       *string `json:"name" binding:"omitempty,min=1,max=60"`
	PricePaise *int64  `json:"pricePaise" binding:"omitempty,min=0,max=10000000"`
	Active     *bool   `json:"active"`
}

// DefaultPlans seeds a new gym with the prototype's price list so it can take payments on day
// one; the owner edits them in Settings.
var DefaultPlans = []CreatePlanRequestBody{
	{Name: "1 month", Months: 1, PricePaise: 120000},
	{Name: "3 months", Months: 3, PricePaise: 330000},
	{Name: "6 months", Months: 6, PricePaise: 600000},
	{Name: "12 months", Months: 12, PricePaise: 1080000},
}
