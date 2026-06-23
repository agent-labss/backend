package httpapi

import (
	"github.com/gofiber/fiber/v3"

	"ai/backend/internal/agent"
	"ai/backend/internal/status"
	"ai/backend/internal/toolcatalog"
)

const (
	HealthzPath = "/healthz"
	ReadyzPath  = "/readyz"
	StatusPath  = "/api/status"
)

type RouterConfig struct {
	StatusHandler status.Handler
	ToolHandler   ToolHandler
	AgentHandler  AgentHandler
}

type ToolHandler interface {
	RegisterTool(c fiber.Ctx) error
	ListTools(c fiber.Ctx) error
	UpdateInstructions(c fiber.Ctx) error
}

type AgentHandler interface {
	CreateRun(c fiber.Ctx) error
}

func NewRouter(config RouterConfig) *fiber.App {
	app := fiber.New()
	app.Use(withCORS)
	app.Get(HealthzPath, healthz)
	app.Get(ReadyzPath, config.StatusHandler.Readyz)
	app.Get(StatusPath, config.StatusHandler.Status)
	if config.ToolHandler != nil {
		app.Post(toolcatalog.ToolsPath, config.ToolHandler.RegisterTool)
		app.Get(toolcatalog.ToolsPath, config.ToolHandler.ListTools)
		app.Put(toolcatalog.AgentInstructionsPath, config.ToolHandler.UpdateInstructions)
	}
	if config.AgentHandler != nil {
		app.Post(agent.AgentRunsPath, config.AgentHandler.CreateRun)
	}
	return app
}
