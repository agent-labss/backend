package httpapi

import (
	"github.com/gofiber/fiber/v3"

	"ai/backend/internal/agent"
	"ai/backend/internal/status"
	"ai/backend/internal/toolcatalog"
)

const (
	StatusPath      = "/api/status"
	uploadBodyLimit = 25 * 1024 * 1024
)

type RouterConfig struct {
	StatusHandler      status.Handler
	ToolHandler        ToolHandler
	ChatSessionHandler ChatSessionHandler
	ChatMessageHandler ChatMessageHandler
}

type ToolHandler interface {
	RegisterTool(c fiber.Ctx) error
	ListTools(c fiber.Ctx) error
	UpdateInstructions(c fiber.Ctx) error
}

type ChatSessionHandler interface {
	CreateChat(c fiber.Ctx) error
	GetChat(c fiber.Ctx) error
}

type ChatMessageHandler interface {
	ListChatMessages(c fiber.Ctx) error
	CreateChatMessage(c fiber.Ctx) error
	SubscribeChatEvents(c fiber.Ctx) error
}

func NewRouter(config RouterConfig) *fiber.App {
	app := fiber.New(fiber.Config{BodyLimit: uploadBodyLimit})
	app.Use(withCORS)
	app.Get(StatusPath, config.StatusHandler.Status)
	if config.ToolHandler != nil {
		app.Post(toolcatalog.ToolsPath, config.ToolHandler.RegisterTool)
		app.Get(toolcatalog.ToolsPath, config.ToolHandler.ListTools)
		app.Put(toolcatalog.AgentInstructionsPath, config.ToolHandler.UpdateInstructions)
	}
	if config.ChatSessionHandler != nil {
		app.Post(agent.ChatSessionsPath, config.ChatSessionHandler.CreateChat)
		app.Get(agent.ChatSessionPath, config.ChatSessionHandler.GetChat)
	}
	if config.ChatMessageHandler != nil {
		app.Get(agent.ChatMessagesPath, config.ChatMessageHandler.ListChatMessages)
		app.Post(agent.ChatMessagesPath, config.ChatMessageHandler.CreateChatMessage)
		app.Get(agent.ChatEventsPath, config.ChatMessageHandler.SubscribeChatEvents)
	}
	return app
}
