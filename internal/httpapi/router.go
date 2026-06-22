package httpapi

import (
	"github.com/gofiber/fiber/v3"

	"orderbuddy-ai/backend/internal/status"
)

type RouterConfig struct {
	StatusHandler status.Handler
}

func NewRouter(config RouterConfig) *fiber.App {
	app := fiber.New()
	app.Use(withCORS)
	app.Get("/healthz", healthz)
	app.Get("/readyz", config.StatusHandler.Readyz)
	app.Get("/api/status", config.StatusHandler.Status)
	return app
}
