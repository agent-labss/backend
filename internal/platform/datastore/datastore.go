package datastore

import (
	"context"
	"errors"
	"fmt"
	"log"

	"gorm.io/gorm"

	"ai/backend/internal/platform/sqlite"
)

const (
	DriverSQLite = "sqlite"
)

var ErrUnsupportedDriver = errors.New("unsupported database driver")

type Config struct {
	Driver string
	URL    string
}

func Connect(ctx context.Context, cfg Config) (*gorm.DB, error) {
	var database *gorm.DB
	var err error
	switch cfg.Driver {
	case DriverSQLite:
		database, err = sqlite.Connect(ctx, cfg.URL)
	default:
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedDriver, cfg.Driver)
	}
	if err != nil {
		return nil, fmt.Errorf("connect %s database: %w", cfg.Driver, err)
	}

	return database, nil
}

func Close(database *gorm.DB) {
	sqlDB, err := database.DB()
	if err == nil {
		if closeErr := sqlDB.Close(); closeErr != nil {
			log.Printf("close database: %v", closeErr)
		}
	}
}
