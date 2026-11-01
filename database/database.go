package database

import (
	"context"
	"fmt"
	"net/url"
	"os"

	"essl_integration_service/logger"

	"github.com/jackc/pgx/v5/pgxpool"
)

var dbPool *pgxpool.Pool

func InitializeDatabasePool() error {

	var err error

	databaseUrl := buildDatabaseUrl()

	dbconfig, err := pgxpool.ParseConfig(databaseUrl)
	if err != nil {
		return fmt.Errorf("failed to parse DB URL: %w", err)
	}

	dbPool, err = pgxpool.NewWithConfig(context.Background(), dbconfig)

	if err != nil {

		errorMsg := fmt.Sprintf("Cannot connect to database %s:%s/%s.Error:%s!\n", os.Getenv("DB_HOST"), os.Getenv("DB_PORT"), os.Getenv("DB_NAME"), err.Error())
		logger.Log.Error(errorMsg)
		return err

	}

	return nil

}

// buildDatabaseUrl differs from the template in one way: sslmode comes from the environment.
// Hosted Postgres needs require + a root cert, while the local docker Postgres used for the
// POC has no TLS at all, and hardcoding either one breaks the other.
func buildDatabaseUrl() string {

	sslMode := os.Getenv("DB_SSL_MODE")
	if sslMode == "" {
		sslMode = "require"
	}

	databaseUrl := fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=%s", url.QueryEscape(os.Getenv("DB_USER")), url.QueryEscape(os.Getenv("DB_PASSWORD")), os.Getenv("DB_HOST"), os.Getenv("DB_PORT"), os.Getenv("DB_NAME"), sslMode)

	if certPath := os.Getenv("DB_SSL_CERT_PATH"); certPath != "" {
		databaseUrl += "&sslrootcert=" + certPath
	}

	return databaseUrl
}

func PingDatabasePool() error {

	return dbPool.Ping(context.Background())

}

func CloseDatabasePool() {

	if dbPool != nil {
		dbPool.Close()
	}

}
