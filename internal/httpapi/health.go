package httpapi

import (
	"net/http"

	"github.com/gofiber/fiber/v3"
)

func healthz(c fiber.Ctx) error {
	return writeJSON(c, http.StatusOK, fiber.Map{"status": "ok"})
}
