package status

import (
	"context"
	"errors"
	"fmt"
	"time"
)

const serviceName = "ai-backend"

const (
	DependencyStatusOK    DependencyStatusValue = "ok"
	DependencyStatusError DependencyStatusValue = "error"
)

var ErrDatabaseMissing = errors.New("database is missing")

type DependencyStatusValue string

type Service struct {
	database DatabasePinger
}

type Response struct {
	Service     string           `json:"service"`
	Environment string           `json:"environment"`
	Database    DependencyStatus `json:"database"`
}

type DependencyStatus struct {
	Status DependencyStatusValue `json:"status"`
}

func NewService(database DatabasePinger) Service {
	return Service{database: database}
}

func (service Service) Status(parent context.Context, environment string) Response {
	databaseStatus := DependencyStatusOK
	if service.pingDatabase(parent) != nil {
		databaseStatus = DependencyStatusError
	}

	return Response{
		Service:     serviceName,
		Environment: environment,
		Database: DependencyStatus{
			Status: databaseStatus,
		},
	}
}

func (service Service) pingDatabase(parent context.Context) error {
	if service.database == nil {
		return ErrDatabaseMissing
	}

	ctx, cancel := context.WithTimeout(parent, 2*time.Second)
	defer cancel()

	if err := service.database.Ping(ctx); err != nil {
		return fmt.Errorf("ping database: %w", err)
	}

	return nil
}
