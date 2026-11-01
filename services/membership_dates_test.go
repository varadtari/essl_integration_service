package services

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func d(s string) time.Time {
	t, err := parseDate(s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestAddMonthsClampedToShorterMonth(t *testing.T) {
	assert.Equal(t, "2027-02-28", formatDate(addMonthsClamped(d("2027-01-31"), 1)))
	assert.Equal(t, "2028-02-29", formatDate(addMonthsClamped(d("2028-01-31"), 1)), "leap year")
	assert.Equal(t, "2027-04-30", formatDate(addMonthsClamped(d("2027-01-31"), 3)))
	assert.Equal(t, "2027-11-15", formatDate(addMonthsClamped(d("2026-11-15"), 12)))
	assert.Equal(t, "2027-01-15", formatDate(addMonthsClamped(d("2026-12-15"), 1)), "crosses year")
}

func TestRenewalOfExpiredMemberStartsToday(t *testing.T) {
	today := d("2026-11-10")
	expired := d("2026-11-01")

	start, end := termForRenewal(today, &expired, 1)

	assert.Equal(t, "2026-11-10", formatDate(start))
	assert.Equal(t, "2026-12-10", formatDate(end))
}

func TestRenewalOfActiveMemberExtendsCurrentTerm(t *testing.T) {
	today := d("2026-11-10")
	current := d("2026-11-20")

	start, end := termForRenewal(today, &current, 3)

	assert.Equal(t, "2026-11-20", formatDate(start))
	assert.Equal(t, "2027-02-20", formatDate(end))
}

func TestRenewalOnTheLastCoveredDayExtends(t *testing.T) {
	today := d("2026-11-10")

	start, _ := termForRenewal(today, &today, 1)

	assert.Equal(t, "2026-11-10", formatDate(start), "endDate == today is still active, so it extends")
}

func TestRenewalWithNoTermStartsToday(t *testing.T) {
	start, end := termForRenewal(d("2026-11-10"), nil, 6)

	assert.Equal(t, "2026-11-10", formatDate(start))
	assert.Equal(t, "2027-05-10", formatDate(end))
}

func TestMemberStatusAndDaysToExpiry(t *testing.T) {
	today := d("2026-11-10")
	yesterday, tomorrow := d("2026-11-09"), d("2026-11-11")

	assert.Equal(t, "active", memberStatus(&today, today))
	assert.Equal(t, "active", memberStatus(&tomorrow, today))
	assert.Equal(t, "overdue", memberStatus(&yesterday, today))
	assert.Equal(t, "overdue", memberStatus(nil, today))

	assert.Equal(t, -1, *daysToExpiry(&yesterday, today))
	assert.Equal(t, 0, *daysToExpiry(&today, today))
	assert.Equal(t, 1, *daysToExpiry(&tomorrow, today))
	assert.Nil(t, daysToExpiry(nil, today))
}

func TestGymTodayUsesGymTimezone(t *testing.T) {
	// 20:00 UTC on 9 Nov is 01:30 on 10 Nov in India.
	now := time.Date(2026, 11, 9, 20, 0, 0, 0, time.UTC)

	assert.Equal(t, "2026-11-10", formatDate(gymToday(now, "Asia/Kolkata")))
	assert.Equal(t, "2026-11-09", formatDate(gymToday(now, "UTC")))
	assert.Equal(t, "2026-11-10", formatDate(gymToday(now, "Not/AZone")), "falls back to IST")
}

func TestMonthRangeIsInGymTimezone(t *testing.T) {
	from, to, err := monthRange("2026-11", "Asia/Kolkata")

	assert.NoError(t, err)
	assert.Equal(t, "2026-10-31T18:30:00Z", from.UTC().Format(time.RFC3339))
	assert.Equal(t, "2026-11-30T18:30:00Z", to.UTC().Format(time.RFC3339))

	_, _, err = monthRange("2026-13", "Asia/Kolkata")
	assert.Error(t, err)
}

func TestNormalizePhone(t *testing.T) {
	cases := map[string]string{
		"9822011101":      "9822011101",
		"+91 98220 11101": "9822011101",
		"919822011101":    "9822011101",
		"09822011101":     "9822011101",
		"(982) 201-1101":  "9822011101",
	}
	for input, want := range cases {
		got, ok := normalizePhone(input)
		assert.True(t, ok, input)
		assert.Equal(t, want, got, input)
	}

	for _, bad := range []string{"", "12345", "5822011101", "98220111012", "+1 415 555 0100"} {
		_, ok := normalizePhone(bad)
		assert.False(t, ok, bad)
	}
}
