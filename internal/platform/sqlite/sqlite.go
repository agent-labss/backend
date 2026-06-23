package sqlite

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"orderbuddy-ai/backend/internal/database"
)

type Connector struct{}

func (Connector) Connect(parent context.Context, databaseURL string) (*gorm.DB, error) {
	return Connect(parent, databaseURL)
}

func Connect(parent context.Context, databaseURL string) (*gorm.DB, error) {
	db, err := gorm.Open(sqlite.Open(databaseURL), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("open sqlite database: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("get sqlite database handle: %w", err)
	}
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)
	sqlDB.SetConnMaxLifetime(time.Hour)

	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()

	if err := sqlDB.PingContext(ctx); err != nil {
		return nil, closeWithError(sqlDB, fmt.Errorf("ping sqlite database: %w", err))
	}

	if err := db.AutoMigrate(database.Models()...); err != nil {
		return nil, closeWithError(sqlDB, fmt.Errorf("migrate sqlite schema: %w", err))
	}

	return db, nil
}

type sqlDatabase interface {
	Close() error
}

func closeWithError(db sqlDatabase, err error) error {
	if closeErr := db.Close(); closeErr != nil {
		return errors.Join(err, fmt.Errorf("close sqlite database: %w", closeErr))
	}
	return err
}
