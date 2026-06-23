package agent

import (
	"context"
	"net/http"
	"strings"

	"github.com/gofiber/fiber/v3"
)

const errorField = "error"

type Runner interface {
	Run(ctx context.Context, request CreateRunRequest) (RunResponse, error)
}

type Handler struct {
	runner Runner
}

func NewHandler(runner Runner) Handler {
	return Handler{runner: runner}
}

func (handler Handler) CreateRun(c fiber.Ctx) error {
	var request CreateRunRequest
	if err := c.Bind().Body(&request); err != nil {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{errorField: "invalid JSON request body"})
	}
	if strings.TrimSpace(request.Message) == "" {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{errorField: "message is required"})
	}

	response, err := handler.runner.Run(c.Context(), request)
	if err != nil {
		return writeRunError(c, response)
	}

	return c.Status(http.StatusOK).JSON(response)
}

func writeRunError(c fiber.Ctx, response RunResponse) error {
	if response.Status == RunStatusFailed {
		return c.Status(http.StatusBadRequest).JSON(response)
	}

	return c.Status(http.StatusInternalServerError).JSON(fiber.Map{errorField: "agent run failed"})
}
