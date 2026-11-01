package test

import (
	"encoding/json"

	"essl_integration_service/app"
	"essl_integration_service/models"

	"github.com/stretchr/testify/assert"

	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetHealthNoErrors(t *testing.T) {

	assert := assert.New(t)

	app.SetupHealthRoute()

	resWriter := httptest.NewRecorder()

	req, _ := http.NewRequest("GET", app.SERVICE_BASE_PATH+"/v1/health", nil)

	req.Header.Set("Content-Type", "application/json")

	app.Router.ServeHTTP(resWriter, req)

	healthResponse := models.SuccessResponse{}
	json.Unmarshal(resWriter.Body.Bytes(), &healthResponse)

	assert.Equal(200, resWriter.Code)
	assert.Equal("success", healthResponse.Type)
	assert.Equal("healthy", healthResponse.Message)

}
