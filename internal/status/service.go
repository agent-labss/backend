package status

import (
	"context"
)

const serviceName = "ai-backend"

type Service struct{}

type Response struct {
	Service     string `json:"service"`
	Environment string `json:"environment"`
}

func NewService() Service {
	return Service{}
}

func (service Service) Status(parent context.Context, environment string) Response {
	_ = parent

	return Response{
		Service:     serviceName,
		Environment: environment,
	}
}
