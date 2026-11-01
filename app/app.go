package app

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/go-playground/validator/v10"
	"go.uber.org/zap"

	"essl_integration_service/database"
	"essl_integration_service/logger"
	"essl_integration_service/models"
)

var (
	Router *gin.Engine
)

const SHUTDOWN_TIMEOUT = 10 * time.Second

func init() {

	gin.SetMode(gin.ReleaseMode)

	Router = gin.New()

	if v, ok := binding.Validator.Engine().(*validator.Validate); ok {
		v.RegisterValidation("inttype", validateIntType)

	}

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
