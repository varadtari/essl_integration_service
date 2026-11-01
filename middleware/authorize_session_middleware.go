package middleware

import (
	"context"
	"net/http"

	"essl_integration_service/models"
	"essl_integration_service/services"
	"essl_integration_service/utils"

	"github.com/gin-gonic/gin"
)

const SESSION_CONTEXT_KEY = "session"

// AuthorizeSession admits requests carrying a valid session cookie for one of the given roles,
// and stores the session in the gin context for controllers. The gym a request acts on always
// comes from here, never from the URL or body, which is what keeps gyms apart.
func AuthorizeSession(allowedRoles ...string) gin.HandlerFunc {
	return func(c *gin.Context) {

		ctx := context.WithValue(c.Request.Context(), models.CONTEXT_REQUEST_ID_KEY, c.GetString("requestId"))

		token, err := c.Cookie(models.SESSION_COOKIE_NAME)
		if err != nil || token == "" {
			apiError := utils.RenderApiErrorV2(ctx, http.StatusUnauthorized, 1003, "session cookie missing", "Please sign in", nil)
			c.AbortWithStatusJSON(apiError.StatusCode, apiError.ApplicationError)
			return
		}

		session, err := services.ParseSessionToken(token)
		if err != nil {
			apiError := utils.RenderApiErrorV2(ctx, http.StatusUnauthorized, 1004, "invalid session: "+err.Error(), "Please sign in again", nil)
			c.AbortWithStatusJSON(apiError.StatusCode, apiError.ApplicationError)
			return
		}

		valid, apiError := services.SessionStillValid(ctx, *session)
		if apiError != nil {
			c.AbortWithStatusJSON(apiError.StatusCode, apiError.ApplicationError)
			return
		}
		if !valid {
			apiError := utils.RenderApiErrorV2(ctx, http.StatusUnauthorized, 1006, "session user inactive or changed", "Please sign in again", nil)
			c.AbortWithStatusJSON(apiError.StatusCode, apiError.ApplicationError)
			return
		}

		if !roleAllowed(session.Role, allowedRoles) {
			apiError := utils.RenderApiErrorV2(ctx, http.StatusForbidden, 1005, "role "+session.Role+" not allowed here", "You do not have access to this page", nil)
			c.AbortWithStatusJSON(apiError.StatusCode, apiError.ApplicationError)
			return
		}

		c.Set(SESSION_CONTEXT_KEY, *session)
		c.Set("caller", models.CALLER_WEB_APP)
		c.Set("sub", session.UserId)

		c.Next()
	}
}

func roleAllowed(role string, allowedRoles []string) bool {
	for _, allowed := range allowedRoles {
		if role == allowed {
			return true
		}
	}
	return false
}
