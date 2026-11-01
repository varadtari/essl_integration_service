package controllers

import (
	"net/http"

	"essl_integration_service/models"

	"github.com/gin-gonic/gin"
)

func GetHealth(c *gin.Context) {

	c.JSON(http.StatusOK, models.SuccessResponse{Type: "success", Message: "healthy"})

}
