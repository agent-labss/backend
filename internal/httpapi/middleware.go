package httpapi

import (
	"net/http"

	"github.com/gofiber/fiber/v3"
)

func withCORS(c fiber.Ctx) error {
	c.Set("Access-Control-Allow-Origin", "*")
	c.Set("Access-Control-Allow-Headers", "Content-Type")
	c.Set("Access-Control-Allow-Methods", "GET, OPTIONS")

	if c.Method() == http.MethodOptions {
		return c.SendStatus(http.StatusNoContent)
	}

	return c.Next()
}
