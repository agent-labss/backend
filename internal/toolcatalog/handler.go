package toolcatalog

import (
	"errors"
	"net/http"

	"github.com/gofiber/fiber/v3"
)

const errorField = "error"

type Handler struct {
	service Service
}

func NewHandler(service Service) Handler {
	return Handler{service: service}
}

func (handler Handler) RegisterTool(c fiber.Ctx) error {
	var request RegisterToolRequest
	if err := c.Bind().Body(&request); err != nil {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{errorField: "invalid JSON request body"})
	}

	tool, err := handler.service.RegisterTool(c.Context(), request)
	if err != nil {
		return handler.writeRegisterToolError(c, err)
	}

	return c.Status(http.StatusCreated).JSON(tool)
}

func (handler Handler) ListTools(c fiber.Ctx) error {
	tools, err := handler.service.ListEnabledTools(c.Context())
	if err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{errorField: "list tools failed"})
	}

	return c.Status(http.StatusOK).JSON(tools)
}

func (handler Handler) UpdateInstructions(c fiber.Ctx) error {
	var request UpdateInstructionsRequest
	if err := c.Bind().Body(&request); err != nil {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{errorField: "invalid JSON request body"})
	}

	instructions, err := handler.service.UpdateInstructions(c.Context(), request)
	if err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{errorField: "update instructions failed"})
	}

	return c.Status(http.StatusOK).JSON(instructions)
}

func (handler Handler) writeRegisterToolError(c fiber.Ctx, err error) error {
	if errors.Is(err, ErrInvalidTool) {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{errorField: err.Error()})
	}
	if errors.Is(err, ErrDuplicateToolName) {
		return c.Status(http.StatusConflict).JSON(fiber.Map{errorField: err.Error()})
	}

	return c.Status(http.StatusInternalServerError).JSON(fiber.Map{errorField: "register tool failed"})
}
