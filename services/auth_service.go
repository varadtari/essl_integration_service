package services

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"essl_integration_service/database"
	"essl_integration_service/models"
	"essl_integration_service/utils"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

const (
	AUTH_SERVICE_BYTE3_VALUE = 0x00210000 //2 = services, 1 = auth
)

var (
	AUTH_SERVICE_ERROR_BASE_CODE int
)

func init() {
	AUTH_SERVICE_ERROR_BASE_CODE = utils.ServiceErrorBaseCode + AUTH_SERVICE_BYTE3_VALUE
}

const (
	BCRYPT_COST = 12

	// Failed logins per email allowed inside LOGIN_FAILURE_WINDOW before further attempts are
	// refused until the window passes. Kept in memory: one instance serves the POC and pilot,
	// and a restart resetting the counter is acceptable at this scale.
	MAX_LOGIN_FAILURES   = 10
	LOGIN_FAILURE_WINDOW = 15 * time.Minute
)

// dummyPasswordHash is compared against when the email is unknown, so a login for a missing
// account takes as long as one with a wrong password and does not reveal which emails exist.
var dummyPasswordHash, _ = bcrypt.GenerateFromPassword([]byte("najah-timing-equaliser"), BCRYPT_COST)

type sessionClaims struct {
	GymId *int   `json:"gymId"`
	Role  string `json:"role"`
	jwt.RegisteredClaims
}

type loginLimiter struct {
	mu       sync.Mutex
	failures map[string][]time.Time
}

var limiter = &loginLimiter{failures: map[string][]time.Time{}}

func (l *loginLimiter) recentFailures(key string, now time.Time) []time.Time {
	recent := l.failures[key][:0]
	for _, at := range l.failures[key] {
		if now.Sub(at) < LOGIN_FAILURE_WINDOW {
			recent = append(recent, at)
		}
	}
	if len(recent) == 0 {
		delete(l.failures, key)
		return nil
	}
	l.failures[key] = recent
	return recent
}

func (l *loginLimiter) blocked(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.recentFailures(key, now)) >= MAX_LOGIN_FAILURES
}

func (l *loginLimiter) fail(key string, now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.failures[key] = append(l.recentFailures(key, now), now)
}

func (l *loginLimiter) reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.failures, key)
}

func HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), BCRYPT_COST)
	return string(hash), err
}

// Login checks credentials and returns a signed session token. Every failure gives the same
// message, whether the email is unknown, the password wrong or the account inactive.
func Login(ctx context.Context, email string, password string) (*models.ApiError, string, *models.MeResponse) {

	key := strings.ToLower(strings.TrimSpace(email))
	now := nowFunc()

	if limiter.blocked(key, now) {
		return utils.RenderApiErrorV2(ctx, http.StatusTooManyRequests, AUTH_SERVICE_ERROR_BASE_CODE+1, "too many failed logins for "+key, "Too many failed attempts. Try again in 15 minutes.", nil), "", nil
	}

	invalid := utils.RenderApiErrorV2(ctx, http.StatusUnauthorized, AUTH_SERVICE_ERROR_BASE_CODE+2, "invalid credentials", "Email or password is incorrect", nil)

	user, appError := database.UserDb.GetUserByEmail(ctx, key)
	if appError != nil && !database.IsNotFound(appError) {
		return fromDaoError(ctx, "Login", appError), "", nil
	}

	if user == nil {
		bcrypt.CompareHashAndPassword(dummyPasswordHash, []byte(password))
		limiter.fail(key, now)
		return invalid, "", nil
	}

	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)) != nil || !user.Active {
		limiter.fail(key, now)
		return invalid, "", nil
	}

	limiter.reset(key)

	token, err := issueToken(user.Id, user.GymId, user.Role, now)
	if err != nil {
		return utils.RenderApiErrorV2(ctx, http.StatusInternalServerError, AUTH_SERVICE_ERROR_BASE_CODE+3, "could not sign token: "+err.Error(), "Could not sign in", nil), "", nil
	}

	apiError, me := GetMe(ctx, user.Id)
	if apiError != nil {
		return apiError, "", nil
	}

	return nil, token, me
}

func GetMe(ctx context.Context, userId int) (*models.ApiError, *models.MeResponse) {

	me, appError := database.UserDb.GetMe(ctx, userId)
	if appError != nil {
		if database.IsNotFound(appError) {
			return utils.RenderApiErrorV2(ctx, http.StatusUnauthorized, AUTH_SERVICE_ERROR_BASE_CODE+4, fmt.Sprintf("user %d not found or inactive", userId), "Please sign in again", nil), nil
		}
		return fromDaoError(ctx, "GetMe", appError), nil
	}

	return nil, me
}

func issueToken(userId int, gymId *int, role string, now time.Time) (string, error) {

	claims := sessionClaims{
		GymId: gymId,
		Role:  role,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   fmt.Sprint(userId),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(models.SESSION_TTL)),
		},
	}

	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(models.JWT_SECRET)
}

// ParseSessionToken validates a session token and returns who it belongs to. Only HS256 is
// accepted, so a token cannot pick its own algorithm.
func ParseSessionToken(token string) (*models.Session, error) {

	claims := &sessionClaims{}
	_, err := jwt.ParseWithClaims(token, claims, func(t *jwt.Token) (any, error) {
		return models.JWT_SECRET, nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}), jwt.WithExpirationRequired())
	if err != nil {
		return nil, err
	}

	var userId int
	if _, err := fmt.Sscan(claims.Subject, &userId); err != nil {
		return nil, fmt.Errorf("bad subject: %w", err)
	}

	return &models.Session{UserId: userId, GymId: claims.GymId, Role: claims.Role}, nil
}

// SessionStillValid confirms a token's user still exists, is active, and still has the role and
// gym the token was issued for. Tokens live for days, so without this a deactivated front-desk
// login would keep working until its token expired.
func SessionStillValid(ctx context.Context, session models.Session) (bool, *models.ApiError) {

	user, appError := database.UserDb.GetSessionUser(ctx, session.UserId)
	if appError != nil {
		if database.IsNotFound(appError) {
			return false, nil
		}
		return false, fromDaoError(ctx, "SessionStillValid", appError)
	}

	sameGym := (user.GymId == nil && session.GymId == nil) || (user.GymId != nil && session.GymId != nil && *user.GymId == *session.GymId)

	return user.Active && user.Role == session.Role && sameGym, nil
}

// CreateSuperadmin is the bootstrap for the client's own login, reached only through the
// API-key protected internal routes.
func CreateSuperadmin(ctx context.Context, input models.CreateUserRequestBody) (*models.ApiError, *models.UserResponse) {

	hash, err := HashPassword(input.Password)
	if err != nil {
		return utils.RenderApiErrorV2(ctx, http.StatusInternalServerError, AUTH_SERVICE_ERROR_BASE_CODE+5, "hash failed: "+err.Error(), "Could not create user", nil), nil
	}

	email := strings.ToLower(strings.TrimSpace(input.Email))
	userId, appError := database.UserDb.CreateUser(ctx, models.User{Name: input.Name, Email: email, PasswordHash: hash, Role: models.ROLE_SUPERADMIN})
	if appError != nil {
		if database.IsConflict(appError) {
			appError.Message.DisplayMessage = "A user with this email already exists"
		}
		return fromDaoError(ctx, "CreateSuperadmin", appError), nil
	}

	return nil, &models.UserResponse{Id: userId, Name: input.Name, Email: email, Role: models.ROLE_SUPERADMIN, Active: true}
}
