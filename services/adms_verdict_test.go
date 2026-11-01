package services

import (
	"testing"

	"essl_integration_service/models"

	"github.com/stretchr/testify/assert"
)

func TestJudgePunch(t *testing.T) {
	member := &models.MemberTerms{MemberId: 1, Terms: [][2]string{{"2026-10-01", "2026-10-31"}, {"2026-12-01", "2026-12-31"}}}

	assert.Equal(t, "unknown", judgePunch(nil, d("2026-10-15"), 0))
	assert.Equal(t, "allowed", judgePunch(member, d("2026-10-01"), 0), "first day")
	assert.Equal(t, "allowed", judgePunch(member, d("2026-10-31"), 0), "last day")
	assert.Equal(t, "allowed_but_expired", judgePunch(member, d("2026-11-01"), 0), "day after expiry")
	assert.Equal(t, "allowed", judgePunch(member, d("2026-11-03"), 3), "inside grace")
	assert.Equal(t, "allowed_but_expired", judgePunch(member, d("2026-11-04"), 3), "after grace")
	assert.Equal(t, "allowed", judgePunch(member, d("2026-12-10"), 0), "a later term counts too: punches are judged on their own day")
	assert.Equal(t, "allowed_but_expired", judgePunch(member, d("2026-09-30"), 0), "before the first term")
	assert.Equal(t, "allowed_but_expired", judgePunch(&models.MemberTerms{MemberId: 2}, d("2026-10-15"), 0), "member with no terms")
}
