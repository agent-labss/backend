package httpapi

import "github.com/gofiber/fiber/v3"

func writeJSON(c fiber.Ctx, statusCode int, body any) error {
	return c.Status(statusCode).JSON(body)
}
