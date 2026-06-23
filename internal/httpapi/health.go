package httpapi

import (
	"net/http"

	"github.com/gofiber/fiber/v3"
)

const (
	responseStatusField = "status"
	responseStatusOK    = "ok"
)

func healthz(c fiber.Ctx) error {
	return writeJSON(c, http.StatusOK, fiber.Map{responseStatusField: responseStatusOK})
}
