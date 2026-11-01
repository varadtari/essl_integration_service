package services

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"essl_integration_service/models"
)

// A membership's endDate is the last calendar day it covers, in the gym's timezone: a member
// whose endDate is today can still train today and shows as overdue tomorrow. This matches the
// prototype, where a member was overdue once exp < today.

// gymLocation resolves a gym's timezone, falling back to IST for a value that fails to load
// (gyms are created with a validated zone, so this only guards against a later bad edit).
func gymLocation(timezone string) *time.Location {
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		loc, _ = time.LoadLocation(models.DEFAULT_GYM_TIMEZONE)
	}
	return loc
}

// civilDate is midnight UTC on the given calendar day. Date maths is done in UTC so a DST
// change in the gym's zone can never move a date by a day.
func civilDate(year int, month time.Month, day int) time.Time {
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}

// gymToday is the current calendar day at the gym.
func gymToday(now time.Time, timezone string) time.Time {
	local := now.In(gymLocation(timezone))
	return civilDate(local.Year(), local.Month(), local.Day())
}

func parseDate(s string) (time.Time, error) {
	return time.Parse(models.DATE_FORMAT, s)
}

func formatDate(t time.Time) string {
	return t.Format(models.DATE_FORMAT)
}

// addMonthsClamped adds calendar months, clamping to the last day of a shorter target month:
// 31 Jan + 1 month is 28 (or 29) Feb, not 3 Mar as time.AddDate and JavaScript's setMonth give.
func addMonthsClamped(date time.Time, months int) time.Time {
	firstOfTarget := civilDate(date.Year(), date.Month(), 1).AddDate(0, months, 0)
	lastDayOfTarget := firstOfTarget.AddDate(0, 1, -1).Day()

	day := date.Day()
	if day > lastDayOfTarget {
		day = lastDayOfTarget
	}

	return civilDate(firstOfTarget.Year(), firstOfTarget.Month(), day)
}

// termForNewMember starts a first membership today.
func termForNewMember(today time.Time, months int) (start time.Time, end time.Time) {
	return today, addMonthsClamped(today, months)
}

// termForRenewal follows the prototype: an expired (or never-paid) member starts a fresh term
// today, while an active member's new term extends their current one so no paid days are lost.
func termForRenewal(today time.Time, currentEnd *time.Time, months int) (start time.Time, end time.Time) {
	if currentEnd == nil || currentEnd.Before(today) {
		return today, addMonthsClamped(today, months)
	}
	return *currentEnd, addMonthsClamped(*currentEnd, months)
}

// memberStatus and daysToExpiry describe a member against today; a member with no term yet is
// reported overdue so they surface in the front desk's list.
func memberStatus(endDate *time.Time, today time.Time) string {
	if endDate == nil || endDate.Before(today) {
		return models.MEMBER_STATUS_OVERDUE
	}
	return models.MEMBER_STATUS_ACTIVE
}

func daysToExpiry(endDate *time.Time, today time.Time) *int {
	if endDate == nil {
		return nil
	}
	days := int(endDate.Sub(today).Hours() / 24)
	return &days
}

// monthRange returns the instants bounding a calendar month at the gym, for payment queries:
// a payment logged at 00:30 IST on the 1st belongs to the new month even though it is the
// previous day in UTC.
func monthRange(month string, timezone string) (from time.Time, to time.Time, err error) {
	loc := gymLocation(timezone)
	first, err := time.ParseInLocation("2006-01", month, loc)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("month must be YYYY-MM: %w", err)
	}
	return first, first.AddDate(0, 1, 0), nil
}

var indianMobile = regexp.MustCompile(`^[6-9][0-9]{9}$`)

// normalizePhone reduces an Indian mobile number to its 10 digits, accepting the +91, 91 and
// 0 prefixes people type. WhatsApp links add the country code back.
func normalizePhone(raw string) (string, bool) {
	digits := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, raw)

	switch {
	case len(digits) == 12 && strings.HasPrefix(digits, "91"):
		digits = digits[2:]
	case len(digits) == 11 && strings.HasPrefix(digits, "0"):
		digits = digits[1:]
	}

	return digits, indianMobile.MatchString(digits)
}
