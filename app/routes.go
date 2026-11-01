package app

import (
	"os"

	"essl_integration_service/controllers"
	"essl_integration_service/middleware"
	"essl_integration_service/models"

	"github.com/gin-gonic/gin"
)

var (
	SERVICE_BASE_PATH string
	authRoutes        *gin.RouterGroup
	sessionRoutes     *gin.RouterGroup
	gymRoutes         *gin.RouterGroup
	adminRoutes       *gin.RouterGroup
	internalRoutes    *gin.RouterGroup
	admsRoutes        *gin.RouterGroup
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

	authRoutes = Router.Group(SERVICE_BASE_PATH + "/v1/auth")
	authRoutes.Use(middleware.LogRequest())

	// Any signed-in user, superadmins included.
	sessionRoutes = Router.Group(SERVICE_BASE_PATH)
	sessionRoutes.Use(middleware.LogRequest())
	sessionRoutes.Use(middleware.AuthorizeSession(models.ROLE_SUPERADMIN, models.ROLE_OWNER, models.ROLE_STAFF))

	// A gym's owner and front desk; every handler here acts on the session's gym.
	gymRoutes = Router.Group(SERVICE_BASE_PATH)
	gymRoutes.Use(middleware.LogRequest())
	gymRoutes.Use(middleware.AuthorizeSession(models.ROLE_OWNER, models.ROLE_STAFF))

	// The client running the platform.
	adminRoutes = Router.Group(SERVICE_BASE_PATH + "/admin")
	adminRoutes.Use(middleware.LogRequest())
	adminRoutes.Use(middleware.AuthorizeSession(models.ROLE_SUPERADMIN))

	// Devices call fixed /iclock paths baked into their firmware, so this group ignores
	// SERVICE_BASE_PATH. It logs to device_raw_messages instead of LogRequest.
	admsRoutes = Router.Group("/iclock")
	admsRoutes.Use(middleware.AuthorizeAdmsDevice())

	internalRoutes = Router.Group(SERVICE_BASE_PATH + "/internal")
	internalRoutes.Use(middleware.LogRequest())
	internalRoutes.Use(middleware.AuthorizeApiKey(middleware.API_KEY))

}

func SetupHealthRoute() {

	Router.GET(SERVICE_BASE_PATH+"/v1/health", controllers.GetHealth)

}

func SetupAuthRoutes() {

	authRoutes.POST("/login", controllers.Login)
	authRoutes.POST("/logout", controllers.Logout)

	sessionRoutes.GET("/v1/me", controllers.GetMe)

}

func SetupGymRoutes() {

	gymRoutes.GET("/v1/dashboard/summary", controllers.GetDashboardSummary)

	gymRoutes.GET("/v1/gym", controllers.GetMyGym)
	gymRoutes.PATCH("/v1/gym", controllers.UpdateMyGym)

	gymRoutes.GET("/v1/plans", controllers.ListPlans)
	gymRoutes.POST("/v1/plans", controllers.CreatePlan)
	gymRoutes.PATCH("/v1/plans/:planId", controllers.UpdatePlan)

	gymRoutes.GET("/v1/members", controllers.ListMembers)
	gymRoutes.POST("/v1/members", controllers.CreateMember)
	gymRoutes.GET("/v1/members/:memberId", controllers.GetMember)
	gymRoutes.PATCH("/v1/members/:memberId", controllers.UpdateMember)
	gymRoutes.DELETE("/v1/members/:memberId", controllers.DeleteMember)
	gymRoutes.POST("/v1/members/:memberId/renew", controllers.RenewMember)

	gymRoutes.GET("/v1/overdue", controllers.ListOverdue)
	gymRoutes.GET("/v1/payments", controllers.ListPayments)

	gymRoutes.GET("/v1/devices", controllers.ListDevices)
	gymRoutes.POST("/v1/devices", controllers.CreateDevice)
	gymRoutes.GET("/v1/devices/:deviceId", controllers.GetDevice)
	gymRoutes.PATCH("/v1/devices/:deviceId", controllers.UpdateDevice)
	gymRoutes.DELETE("/v1/devices/:deviceId", controllers.DeleteDevice)
	gymRoutes.GET("/v1/devices/:deviceId/commands", controllers.ListDeviceCommands)
	gymRoutes.POST("/v1/devices/:deviceId/commands", controllers.CreateDeviceCommand)

	gymRoutes.GET("/v1/checkins", controllers.ListCheckins)

	gymRoutes.GET("/v1/staff", controllers.ListStaff)
	gymRoutes.POST("/v1/staff", controllers.CreateStaff)
	gymRoutes.PATCH("/v1/staff/:userId", controllers.SetStaffActive)

}

func SetupAdminRoutes() {

	adminRoutes.GET("/v1/gyms", controllers.AdminListGyms)
	adminRoutes.POST("/v1/gyms", controllers.AdminCreateGym)
	adminRoutes.PATCH("/v1/gyms/:gymId", controllers.AdminUpdateGym)

}

func SetupAdmsRoutes() {

	admsRoutes.Any("/*path", controllers.AdmsDispatch)

}

func SetupInternalRoutes() {

	internalRoutes.POST("/v1/superadmins", controllers.CreateSuperadmin)

}

// SetupAllRoutes wires every route; StartApp and the HTTP tests share it.
func SetupAllRoutes() {

	SetupRoutesMiddleware()
	SetupHealthRoute()
	SetupAuthRoutes()
	SetupGymRoutes()
	SetupAdminRoutes()
	SetupAdmsRoutes()
	SetupInternalRoutes()

}
