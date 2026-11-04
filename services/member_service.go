package services

import (
	"context"
	"strings"
	"time"

	"essl_integration_service/database"
	"essl_integration_service/models"
	"essl_integration_service/utils"
)

const (
	MEMBER_SERVICE_BYTE3_VALUE = 0x00250000 //2 = services, 5 = members
)

var (
	MEMBER_SERVICE_ERROR_BASE_CODE int
)

func init() {
	MEMBER_SERVICE_ERROR_BASE_CODE = utils.ServiceErrorBaseCode + MEMBER_SERVICE_BYTE3_VALUE
}

// gymContext is what every member operation needs: the gym id from the session and "today" in
// that gym's timezone.
type gymContext struct {
	gymId int
	today time.Time
}

func loadGymContext(ctx context.Context, session models.Session) (*gymContext, *models.ApiError) {

	gymId, apiError := requireGym(ctx, session)
	if apiError != nil {
		return nil, apiError
	}

	gym, appError := database.GymDb.GetGym(ctx, gymId)
	if appError != nil {
		return nil, fromDaoError(ctx, "loadGymContext", appError)
	}

	return &gymContext{gymId: gymId, today: gymToday(nowFunc(), gym.Timezone)}, nil
}

func parseOptionalDate(s *string) *time.Time {
	if s == nil {
		return nil
	}
	t, err := parseDate(*s)
	if err != nil {
		return nil
	}
	return &t
}

func toMemberListItem(row models.MemberRow, today time.Time) models.MemberListItem {
	endDate := parseOptionalDate(row.EndDate)
	return models.MemberListItem{
		Id:           row.Id,
		DevicePin:    row.DevicePin,
		Name:         row.Name,
		Phone:        row.Phone,
		StartDate:    row.StartDate,
		EndDate:      row.EndDate,
		Status:       memberStatus(endDate, today),
		DaysToExpiry: daysToExpiry(endDate, today),
	}
}

func ListMembers(ctx context.Context, session models.Session, query models.MemberListQuery) (*models.ApiError, []models.MemberListItem) {

	gc, apiError := loadGymContext(ctx, session)
	if apiError != nil {
		return apiError, nil
	}

	rows, appError := database.MemDb.ListMembers(ctx, gc.gymId, formatDate(gc.today), strings.TrimSpace(query.Search), query.Status)
	if appError != nil {
		return fromDaoError(ctx, "ListMembers", appError), nil
	}

	accessRows, devices, appError := database.AccDb.AccessSummaries(ctx, gc.gymId)
	if appError != nil {
		return fromDaoError(ctx, "ListMembers", appError), nil
	}
	byMember := map[int][]models.DeviceAccess{}
	for _, a := range accessRows {
		byMember[a.MemberId] = append(byMember[a.MemberId], a)
	}

	items := make([]models.MemberListItem, 0, len(rows))
	for _, row := range rows {
		item := toMemberListItem(row, gc.today)
		summary := summarizeAccess(byMember[row.Id], devices)
		item.Access = &summary
		items = append(items, item)
	}

	return nil, items
}

func GetMemberDetail(ctx context.Context, session models.Session, memberId int) (*models.ApiError, *models.MemberDetail) {

	gc, apiError := loadGymContext(ctx, session)
	if apiError != nil {
		return apiError, nil
	}

	row, appError := database.MemDb.GetMember(ctx, gc.gymId, memberId)
	if appError != nil {
		return fromDaoError(ctx, "GetMemberDetail", appError), nil
	}

	memberships, appError := database.MemDb.ListMemberships(ctx, gc.gymId, memberId)
	if appError != nil {
		return fromDaoError(ctx, "GetMemberDetail", appError), nil
	}

	payments, appError := database.PayDb.ListMemberPayments(ctx, gc.gymId, memberId)
	if appError != nil {
		return fromDaoError(ctx, "GetMemberDetail", appError), nil
	}

	return nil, &models.MemberDetail{
		MemberListItem: toMemberListItem(*row, gc.today),
		Memberships:    memberships,
		Payments:       payments,
	}
}

// sellablePlan loads a plan of the caller's gym and refuses retired ones.
func sellablePlan(ctx context.Context, gymId int, planId int) (*models.Plan, *models.ApiError) {

	plan, appError := database.PlanDb.GetPlan(ctx, gymId, planId)
	if appError != nil {
		return nil, fromDaoError(ctx, "sellablePlan", appError)
	}
	if !plan.Active {
		return nil, badRequest(ctx, MEMBER_SERVICE_ERROR_BASE_CODE+1, "This plan is no longer sold")
	}

	return plan, nil
}

// CreateMember adds a member with their first term and payment, as the prototype's Add member
// modal did, and allocates the pin they will be enrolled under on the device.
func CreateMember(ctx context.Context, session models.Session, input models.CreateMemberRequestBody) (*models.ApiError, *models.MemberListItem) {

	gc, apiError := loadGymContext(ctx, session)
	if apiError != nil {
		return apiError, nil
	}

	phone, ok := normalizePhone(input.Phone)
	if !ok {
		return badRequest(ctx, MEMBER_SERVICE_ERROR_BASE_CODE+2, "Enter a valid 10-digit mobile number"), nil
	}

	name := strings.TrimSpace(input.Name)
	if name == "" {
		return badRequest(ctx, MEMBER_SERVICE_ERROR_BASE_CODE+3, "Enter the member's name"), nil
	}

	plan, apiError := sellablePlan(ctx, gc.gymId, input.PlanId)
	if apiError != nil {
		return apiError, nil
	}

	start, end := termForNewMember(gc.today, plan.Months)
	term := models.NewTerm{PlanId: plan.Id, StartDate: formatDate(start), EndDate: formatDate(end), AmountPaise: plan.PricePaise, Method: input.Method, RecordedBy: session.UserId}

	memberId, devicePin, appError := database.MemDb.CreateMemberWithTerm(ctx, gc.gymId, name, phone, term)
	if appError != nil {
		return fromDaoError(ctx, "CreateMember", appError), nil
	}

	// Puts the new member on the gym's devices so their face can be enrolled under this pin.
	syncMemberAccess(ctx, gc.gymId, memberId)

	item := toMemberListItem(models.MemberRow{Id: memberId, DevicePin: devicePin, Name: name, Phone: phone, StartDate: &term.StartDate, EndDate: &term.EndDate}, gc.today)
	return nil, &item
}

// RenewMember logs a payment and adds the term it bought.
func RenewMember(ctx context.Context, session models.Session, memberId int, input models.RenewMemberRequestBody) (*models.ApiError, *models.MemberListItem) {

	gc, apiError := loadGymContext(ctx, session)
	if apiError != nil {
		return apiError, nil
	}

	row, appError := database.MemDb.GetMember(ctx, gc.gymId, memberId)
	if appError != nil {
		return fromDaoError(ctx, "RenewMember", appError), nil
	}

	plan, apiError := sellablePlan(ctx, gc.gymId, input.PlanId)
	if apiError != nil {
		return apiError, nil
	}

	start, end := termForRenewal(gc.today, parseOptionalDate(row.EndDate), plan.Months)
	term := models.NewTerm{PlanId: plan.Id, StartDate: formatDate(start), EndDate: formatDate(end), AmountPaise: plan.PricePaise, Method: input.Method, RecordedBy: session.UserId}

	_, appError = database.MemDb.RenewMember(ctx, gc.gymId, memberId, row.EndDate, term)
	if appError != nil {
		if database.IsConflict(appError) {
			appError.Message.DisplayMessage = "This membership was just updated. Refresh and check before renewing again."
		}
		return fromDaoError(ctx, "RenewMember", appError), nil
	}

	// An expired member gets door access back at once, not at the next sweep.
	syncMemberAccess(ctx, gc.gymId, memberId)

	row.StartDate, row.EndDate = &term.StartDate, &term.EndDate
	item := toMemberListItem(*row, gc.today)
	return nil, &item
}

func UpdateMember(ctx context.Context, session models.Session, memberId int, input models.UpdateMemberRequestBody) (*models.ApiError, *models.MemberListItem) {

	gc, apiError := loadGymContext(ctx, session)
	if apiError != nil {
		return apiError, nil
	}

	if input.Name != nil {
		trimmed := strings.TrimSpace(*input.Name)
		if trimmed == "" {
			return badRequest(ctx, MEMBER_SERVICE_ERROR_BASE_CODE+4, "Enter the member's name"), nil
		}
		input.Name = &trimmed
	}

	if input.Phone != nil {
		phone, ok := normalizePhone(*input.Phone)
		if !ok {
			return badRequest(ctx, MEMBER_SERVICE_ERROR_BASE_CODE+5, "Enter a valid 10-digit mobile number"), nil
		}
		input.Phone = &phone
	}

	if appError := database.MemDb.UpdateMember(ctx, gc.gymId, memberId, input.Name, input.Phone); appError != nil {
		return fromDaoError(ctx, "UpdateMember", appError), nil
	}

	row, appError := database.MemDb.GetMember(ctx, gc.gymId, memberId)
	if appError != nil {
		return fromDaoError(ctx, "UpdateMember", appError), nil
	}

	// A new name is sent to the devices.
	syncMemberAccess(ctx, gc.gymId, memberId)

	item := toMemberListItem(*row, gc.today)
	return nil, &item
}

// DeleteMember hides a member; their payments stay in the revenue history.
func DeleteMember(ctx context.Context, session models.Session, memberId int) *models.ApiError {

	gymId, apiError := requireGym(ctx, session)
	if apiError != nil {
		return apiError
	}
	if apiError := requireOwner(ctx, session); apiError != nil {
		return apiError
	}

	if appError := database.MemDb.SoftDeleteMember(ctx, gymId, memberId); appError != nil {
		return fromDaoError(ctx, "DeleteMember", appError)
	}

	syncMemberAccess(ctx, gymId, memberId)

	return nil
}
