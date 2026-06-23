package httpapi

import (
	"github.com/gofiber/fiber/v3"

	"orderbuddy-ai/backend/internal/status"
)

const (
	HealthzPath = "/healthz"
	ReadyzPath  = "/readyz"
	StatusPath  = "/api/status"
)

type RouterConfig struct {
	StatusHandler status.Handler
}

func NewRouter(config RouterConfig) *fiber.App {
	app := fiber.New()
	app.Use(withCORS)
	app.Get(HealthzPath, healthz)
	app.Get(ReadyzPath, config.StatusHandler.Readyz)
	app.Get(StatusPath, config.StatusHandler.Status)
	return app
}
