package app

import (
	"os"

	"essl_integration_service/controllers"
	"essl_integration_service/middleware"

	"github.com/gin-gonic/gin"
)

var (
	SERVICE_BASE_PATH string
)

func init() {

	SERVICE_BASE_PATH = os.Getenv("SERVICE_BASE_PATH")

}

// SetupRoutesMiddleware registers the middleware every route shares. It must run before any
// route is added: gin copies the engine's middleware into a route's handler chain when the
// route is registered, so Router.Use after registration silently skips existing routes.
func SetupRoutesMiddleware() {

	Router.Use(gin.Recovery())
	Router.Use(middleware.RequestId())
	Router.Use(middleware.CorsMiddleware())

}

func SetupHealthRoute() {

	Router.GET(SERVICE_BASE_PATH+"/v1/health", controllers.GetHealth)

}
