package controllers

import (
	"net/http"

	"essl_integration_service/models"
	"essl_integration_service/services"
	"essl_integration_service/utils"

	"github.com/gin-gonic/gin"
)

const (
	AUTH_CONTROLLER_BYTE3_VALUE = 0x00110000 //1 = controller, 1 = auth
)

var (
	AUTH_CONTROLLER_ERROR_BASE_CODE int
)

func init() {
	AUTH_CONTROLLER_ERROR_BASE_CODE = utils.ServiceErrorBaseCode + AUTH_CONTROLLER_BYTE3_VALUE
}

func setSessionCookie(c *gin.Context, token string, maxAgeSeconds int) {
	// SameSite=Lax keeps the cookie off cross-site POSTs, which together with the JSON-only API
	// and the single allowed CORS origin is the CSRF defence.
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(models.SESSION_COOKIE_NAME, token, maxAgeSeconds, "/", "", models.COOKIE_SECURE, true)
}

func Login(c *gin.Context) {

	ctx := getContextFromGinContext(c)

	var input models.LoginRequestBody
	if err := c.ShouldBindJSON(&input); err != nil {
		// Never echo the body back into logs here: it carries the password.
		respondBindError(c, ctx, AUTH_CONTROLLER_ERROR_BASE_CODE+1, "Login", "Request body", nil, err)
		return
	}

	apiError, token, me := services.Login(ctx, input.Email, input.Password)
	if apiError != nil {
		respondApiError(c, apiError)
		return
	}

	setSessionCookie(c, token, int(models.SESSION_TTL.Seconds()))
	respondSuccess(c, http.StatusOK, me)
}

func Logout(c *gin.Context) {

	setSessionCookie(c, "", -1)
	respondSuccess(c, http.StatusOK, "signed out")
}

func GetMe(c *gin.Context) {

	ctx := getContextFromGinContext(c)

	apiError, me := services.GetMe(ctx, getSession(c).UserId)
	if apiError != nil {
		if apiError.StatusCode == http.StatusUnauthorized {
			setSessionCookie(c, "", -1)
		}
		respondApiError(c, apiError)
		return
	}

	respondSuccess(c, http.StatusOK, me)
}

// CreateSuperadmin bootstraps the client's login; it is only routed on the internal group.
func CreateSuperadmin(c *gin.Context) {

	ctx := getContextFromGinContext(c)

	var input models.CreateUserRequestBody
	if err := c.ShouldBindJSON(&input); err != nil {
		respondBindError(c, ctx, AUTH_CONTROLLER_ERROR_BASE_CODE+2, "CreateSuperadmin", "Request body", nil, err)
		return
	}

	apiError, user := services.CreateSuperadmin(ctx, input)
	if apiError != nil {
		respondApiError(c, apiError)
		return
	}

	respondSuccess(c, http.StatusCreated, user)
}
