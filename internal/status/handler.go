package status

import (
	"net/http"

	"github.com/gofiber/fiber/v3"
)

type Handler struct {
	service     Service
	environment string
}

func NewHandler(service Service, environment string) Handler {
	return Handler{service: service, environment: environment}
}

func (handler Handler) Status(c fiber.Ctx) error {
	return c.Status(http.StatusOK).JSON(handler.service.Status(c.Context(), handler.environment))
}
