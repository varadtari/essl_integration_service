package app

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/go-playground/validator/v10"
	"go.uber.org/zap"

	"essl_integration_service/database"
	"essl_integration_service/logger"
	"essl_integration_service/models"
	"essl_integration_service/services"
)

var (
	Router *gin.Engine
)

const SHUTDOWN_TIMEOUT = 10 * time.Second

func init() {

	gin.SetMode(gin.ReleaseMode)

	Router = gin.New()

	// Only trust X-Forwarded-For from the reverse proxy in front of the service. Gin trusts every
	// peer by default, which would let a client fake its IP, and device IPs are recorded.
	if err := Router.SetTrustedProxies(trustedProxies()); err != nil {
		panic(err)
	}

	if v, ok := binding.Validator.Engine().(*validator.Validate); ok {
		v.RegisterValidation("inttype", validateIntType)

	}

}

// trustedProxies reads TRUSTED_PROXIES, a comma-separated list of proxy IPs or CIDRs. Unset
// means none: the TCP peer is the client.
func trustedProxies() []string {
	var proxies []string
	for _, p := range strings.Split(os.Getenv("TRUSTED_PROXIES"), ",") {
		if p = strings.TrimSpace(p); p != "" {
			proxies = append(proxies, p)
		}
	}
	return proxies
}

func StartApp() {

	defer func() {
		database.CloseDatabasePool()
	}()

	if len(models.JWT_SECRET) < models.MIN_JWT_SECRET_BYTES {
		panic("JWT_SECRET must be set to at least 32 bytes")
	}

	if err := database.InitializeDatabasePool(); err != nil {
		panic(err)
	}

	SetupAllRoutes()

	server := &http.Server{
		Addr:              ":" + os.Getenv("SERVER_PORT"),
		Handler:           Router,
		ReadHeaderTimeout: 10 * time.Second,
	}

	// Live events: one LISTEN connection relays pg_notify to the open dashboard streams. On
	// shutdown the hub closes every stream, otherwise Shutdown would wait out each open tab.
	liveCtx, stopLive := context.WithCancel(context.Background())
	defer stopLive()
	go services.StartLiveEvents(liveCtx)

	// Expiry: every gym is reconciled at start and then every few minutes, so members are
	// removed from devices once their term (plus grace) ends, even with nobody signed in.
	go services.StartAccessScheduler(liveCtx)
	server.RegisterOnShutdown(services.Hub.Close)

	go func() {

		logger.Log.Info("server listening", zap.String("addr", server.Addr))

		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			panic(err)
		}
	}()

	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, syscall.SIGINT, syscall.SIGTERM)
	<-interrupt

	// Drain in-flight requests before the pool closes. A device mid-upload that gets cut off
	// re-sends the batch from its own stamp, but a clean shutdown avoids the duplicate work.
	ctx, cancel := context.WithTimeout(context.Background(), SHUTDOWN_TIMEOUT)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		logger.Log.Error("server shutdown failed", zap.Error(err))
	}

}
