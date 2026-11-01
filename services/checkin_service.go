package services

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"essl_integration_service/database"
	"essl_integration_service/models"
	"essl_integration_service/utils"
)

const (
	CHECKIN_SERVICE_BYTE3_VALUE = 0x00290000 //2 = services, 9 = check-ins
)

var (
	CHECKIN_SERVICE_ERROR_BASE_CODE int
)

func init() {
	CHECKIN_SERVICE_ERROR_BASE_CODE = utils.ServiceErrorBaseCode + CHECKIN_SERVICE_BYTE3_VALUE
}

const DEFAULT_CHECKIN_PAGE = 50

// The cursor is "<unix nanos>_<id>" of the last row of the previous page.
func encodeCheckinCursor(c models.Checkin) string {
	return fmt.Sprintf("%d_%d", c.PunchedAt.UnixNano(), c.Id)
}

func decodeCheckinCursor(cursor string) (*time.Time, int64, bool) {
	at, id, found := strings.Cut(cursor, "_")
	if !found {
		return nil, 0, false
	}
	nanos, err1 := strconv.ParseInt(at, 10, 64)
	eventId, err2 := strconv.ParseInt(id, 10, 64)
	if err1 != nil || err2 != nil {
		return nil, 0, false
	}
	t := time.Unix(0, nanos)
	return &t, eventId, true
}

// ListCheckins pages through the gym's check-ins, newest first. from/to are calendar days at
// the gym; to is inclusive.
func ListCheckins(ctx context.Context, session models.Session, query models.CheckinListQuery) (*models.ApiError, *models.CheckinPage) {

	gymId, apiError := requireGym(ctx, session)
	if apiError != nil {
		return apiError, nil
	}

	gym, appError := database.GymDb.GetGym(ctx, gymId)
	if appError != nil {
		return fromDaoError(ctx, "ListCheckins", appError), nil
	}
	loc := gymLocation(gym.Timezone)

	filter := models.CheckinFilter{MemberId: query.MemberId, DeviceId: query.DeviceId, Verdict: query.Verdict, Limit: query.Limit}
	if filter.Limit == 0 {
		filter.Limit = DEFAULT_CHECKIN_PAGE
	}

	if query.From != "" {
		from, _ := time.ParseInLocation(models.DATE_FORMAT, query.From, loc)
		filter.From = &from
	}
	if query.To != "" {
		to, _ := time.ParseInLocation(models.DATE_FORMAT, query.To, loc)
		end := to.AddDate(0, 0, 1)
		filter.To = &end
	}
	if query.Before != "" {
		at, id, ok := decodeCheckinCursor(query.Before)
		if !ok {
			return badRequest(ctx, CHECKIN_SERVICE_ERROR_BASE_CODE+1, "Invalid page cursor"), nil
		}
		filter.BeforeAt, filter.BeforeId = at, id
	}

	checkins, appError := database.EvDb.ListCheckins(ctx, gymId, filter)
	if appError != nil {
		return fromDaoError(ctx, "ListCheckins", appError), nil
	}

	page := &models.CheckinPage{Checkins: checkins}
	if len(checkins) == filter.Limit {
		cursor := encodeCheckinCursor(checkins[len(checkins)-1])
		page.NextCursor = &cursor
	}

	return nil, page
}

// countCheckinsToday counts punches on the gym's current calendar day.
func countCheckinsToday(ctx context.Context, gymId int, timezone string) (int, *models.ApiError) {

	loc := gymLocation(timezone)
	local := nowFunc().In(loc)
	start := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)

	count, appError := database.EvDb.CountCheckins(ctx, gymId, start, start.AddDate(0, 0, 1))
	if appError != nil {
		return 0, fromDaoError(ctx, "countCheckinsToday", appError)
	}

	return count, nil
}
