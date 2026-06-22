package status

import (
	"context"
	"errors"
	"time"
)

const serviceName = "ai-backend"

type Service struct {
	database DatabasePinger
}

type Response struct {
	Service     string           `json:"service"`
	Environment string           `json:"environment"`
	Database    DependencyStatus `json:"database"`
}

type DependencyStatus struct {
	Status string `json:"status"`
}

func NewService(database DatabasePinger) Service {
	return Service{database: database}
}

func (service Service) Ready(parent context.Context) error {
	return service.pingDatabase(parent)
}

func (service Service) Status(parent context.Context, environment string) Response {
	databaseStatus := "ok"
	if service.pingDatabase(parent) != nil {
		databaseStatus = "error"
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
		return errors.New("database is missing")
	}

	ctx, cancel := context.WithTimeout(parent, 2*time.Second)
	defer cancel()

	return service.database.Ping(ctx)
}
