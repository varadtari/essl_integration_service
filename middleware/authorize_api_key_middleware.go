package middleware

import (
	"context"
	"crypto/subtle"
	"net/http"
	"os"

	"essl_integration_service/models"
	"essl_integration_service/utils"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type API_KEY_TYPE int

const (
	API_KEY API_KEY_TYPE = iota
)

var (
	apiKeys = map[API_KEY_TYPE]string{
		API_KEY: os.Getenv("API_KEY"),
	}
)

func AuthorizeApiKey(keyType API_KEY_TYPE) gin.HandlerFunc {
	return func(c *gin.Context) {

		xApiKey := c.GetHeader("x-api-key")

		requestId := uuid.New().String()
		ctx := context.WithValue(c.Request.Context(), models.CONTEXT_REQUEST_ID_KEY, requestId)

		if xApiKey == "" {
			apiError := utils.RenderApiErrorV2(ctx, http.StatusUnauthorized, 1001, "X-API-KEY missing in header!", "X-API-KEY missing in header!", nil)
			c.Abort()
			c.JSON(apiError.StatusCode, apiError.ApplicationError)
			return
		}

		// An unset key must never match an empty header, hence the explicit check before the
		// constant-time compare.
		expectedApiKey, ok := apiKeys[keyType]
		if !ok || expectedApiKey == "" || subtle.ConstantTimeCompare([]byte(xApiKey), []byte(expectedApiKey)) != 1 {
			apiError := utils.RenderApiErrorV2(ctx, http.StatusUnauthorized, 1002, "API key mismatch!", "API key mismatch!", nil)
			c.Abort()
			c.JSON(apiError.StatusCode, apiError.ApplicationError)
			return
		}

		c.Set("caller", models.CALLER_INTERNAL)
		c.Set("sub", "internal")
		c.Set("requestId", requestId)

	}
}

// SetApiKeyForTests replaces a configured key. Only the HTTP tests call it.
func SetApiKeyForTests(keyType API_KEY_TYPE, key string) {
	apiKeys[keyType] = key
}
