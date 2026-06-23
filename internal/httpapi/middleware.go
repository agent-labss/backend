package httpapi

import (
	"net/http"

	"github.com/gofiber/fiber/v3"
)

const (
	headerAccessControlAllowOrigin  = "Access-Control-Allow-Origin"
	headerAccessControlAllowHeaders = "Access-Control-Allow-Headers"
	headerAccessControlAllowMethods = "Access-Control-Allow-Methods"
	headerContentType               = "Content-Type"
	corsAllowedOrigin               = "*"
	corsAllowedMethods              = "GET, POST, PUT, OPTIONS"
)

func withCORS(c fiber.Ctx) error {
	c.Set(headerAccessControlAllowOrigin, corsAllowedOrigin)
	c.Set(headerAccessControlAllowHeaders, headerContentType)
	c.Set(headerAccessControlAllowMethods, corsAllowedMethods)

	if c.Method() == http.MethodOptions {
		return c.SendStatus(http.StatusNoContent)
	}

	return c.Next()
}
