package agent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/sse"
)

const (
	errorField                  = "error"
	messageOrAttachmentRequired = "message or attachment is required"
)

var (
	errInvalidJSONRequestBody      = errors.New("invalid JSON request body")
	errInvalidMultipartRequestBody = errors.New("invalid multipart request body")
	errInvalidMultipartFile        = errors.New("invalid multipart file")
)

type ChatSessionService interface {
	CreateChat(ctx context.Context, request CreateChatRequest) (ChatSession, error)
	GetChat(ctx context.Context, chatID string) (ChatSession, error)
}

type ChatMessageService interface {
	ListChatMessages(ctx context.Context, chatID string) ([]ChatMessage, error)
	CreateChatMessage(ctx context.Context, chatID string, request CreateChatMessageRequest) (SubmitChatMessageResponse, error)
}

type ChatEventService interface {
	SubscribeChatEvents(ctx context.Context, chatID string) (<-chan ChatEvent, func(), error)
}

type Handler struct {
	chatSessions ChatSessionService
	chatMessages ChatMessageService
	chatEvents   ChatEventService
	uploadConfig UploadConfig
}

func NewHandler(chatSessions ChatSessionService, chatMessages ChatMessageService, chatEvents ChatEventService, uploadConfigs ...UploadConfig) Handler {
	config := UploadConfig{MaxFiles: 5, MaxFileBytes: 10 * 1024 * 1024, MaxTotalBytes: 25 * 1024 * 1024}
	if len(uploadConfigs) > 0 {
		config = uploadConfigs[0]
	}

	return Handler{chatSessions: chatSessions, chatMessages: chatMessages, chatEvents: chatEvents, uploadConfig: config}
}

func (handler Handler) CreateChat(c fiber.Ctx) error {
	var request CreateChatRequest
	if err := c.Bind().Body(&request); err != nil {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{errorField: errInvalidJSONRequestBody.Error()})
	}

	response, err := handler.chatSessions.CreateChat(c.Context(), request)
	if err != nil {
		return c.Status(http.StatusInternalServerError).JSON(fiber.Map{errorField: "create chat failed"})
	}

	return c.Status(http.StatusCreated).JSON(response)
}

func (handler Handler) GetChat(c fiber.Ctx) error {
	response, err := handler.chatSessions.GetChat(c.Context(), c.Params("chat_id"))
	if err != nil {
		return writeChatError(c, err)
	}

	return c.Status(http.StatusOK).JSON(response)
}

func (handler Handler) ListChatMessages(c fiber.Ctx) error {
	response, err := handler.chatMessages.ListChatMessages(c.Context(), c.Params("chat_id"))
	if err != nil {
		return writeChatError(c, err)
	}

	return c.Status(http.StatusOK).JSON(response)
}

func (handler Handler) CreateChatMessage(c fiber.Ctx) error {
	input, err := handler.userInputRequest(c)
	if err != nil {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{errorField: err.Error()})
	}
	if strings.TrimSpace(input.Message) == "" && len(input.Attachments) == 0 {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{errorField: messageOrAttachmentRequired})
	}

	response, err := handler.chatMessages.CreateChatMessage(c.Context(), c.Params("chat_id"), CreateChatMessageRequest(input))
	if err != nil {
		return writeChatError(c, err)
	}

	return c.Status(http.StatusAccepted).JSON(response)
}

func (handler Handler) SubscribeChatEvents(c fiber.Ctx) error {
	if handler.chatEvents == nil {
		return c.SendStatus(http.StatusNotFound)
	}
	chatID := c.Params("chat_id")
	events, unsubscribe, err := handler.chatEvents.SubscribeChatEvents(c.Context(), chatID)
	if err != nil {
		return writeChatError(c, err)
	}

	connected := ChatEvent{Type: EventTypeConnected, ChatID: chatID}
	return sse.New(sse.Config{
		DisableHeartbeat: true,
		Handler: func(_ fiber.Ctx, stream *sse.Stream) error {
			return streamChatEvents(stream, connected, events, unsubscribe)
		},
	})(c)
}

func streamChatEvents(stream *sse.Stream, connected ChatEvent, events <-chan ChatEvent, unsubscribe func()) error {
	defer unsubscribe()
	if err := writeSSEEvent(stream, connected); err != nil {
		return err
	}
	for {
		event, ok, err := nextChatEvent(stream, events)
		if err != nil {
			return err
		}
		if !ok {
			return nil
		}
		if err := writeSSEEvent(stream, event); err != nil {
			return err
		}
	}
}

func nextChatEvent(stream *sse.Stream, events <-chan ChatEvent) (ChatEvent, bool, error) {
	select {
	case event, ok := <-events:
		return event, ok, nil
	case <-stream.Done():
		if err := stream.Err(); err != nil {
			return ChatEvent{}, false, fmt.Errorf("sse stream closed: %w", err)
		}
		return ChatEvent{}, false, nil
	}
}

func writeSSEEvent(stream *sse.Stream, event ChatEvent) error {
	if err := stream.Event(sse.Event{Name: string(event.Type), Data: event}); err != nil {
		return fmt.Errorf("write sse event: %w", err)
	}
	return nil
}

type userInputRequest struct {
	Message     string       `json:"message"`
	Attachments []Attachment `json:"attachments,omitempty"`
}

func (handler Handler) userInputRequest(c fiber.Ctx) (userInputRequest, error) {
	contentType, err := mediaType(c.Get("Content-Type"))
	if err != nil {
		return userInputRequest{}, err
	}
	if contentType == "multipart/form-data" {
		return handler.multipartUserInputRequest(c)
	}

	var request userInputRequest
	if err := c.Bind().Body(&request); err != nil {
		return userInputRequest{}, errInvalidJSONRequestBody
	}
	attachments, err := normalizeJSONAttachments(request.Attachments, handler.uploadConfig)
	if err != nil {
		return userInputRequest{}, err
	}
	request.Attachments = attachments
	return request, nil
}

func mediaType(contentType string) (string, error) {
	if strings.TrimSpace(contentType) == "" {
		return "", nil
	}
	parsed, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return "", errInvalidJSONRequestBody
	}
	return parsed, nil
}

func (handler Handler) multipartUserInputRequest(c fiber.Ctx) (userInputRequest, error) {
	form, err := c.MultipartForm()
	if err != nil {
		return userInputRequest{}, errInvalidMultipartRequestBody
	}

	files, err := uploadedFilesFromForm(form)
	if err != nil {
		return userInputRequest{}, err
	}
	attachments, err := buildAttachments(files, handler.uploadConfig)
	if err != nil {
		return userInputRequest{}, err
	}

	return userInputRequest{Message: c.FormValue("message"), Attachments: attachments}, nil
}

func uploadedFilesFromForm(form *multipart.Form) ([]UploadedFile, error) {
	headers := append([]*multipart.FileHeader{}, form.File["files[]"]...)
	headers = append(headers, form.File["files"]...)
	files := make([]UploadedFile, 0, len(headers))
	for _, header := range headers {
		file, err := header.Open()
		if err != nil {
			return nil, errInvalidMultipartFile
		}
		data, readErr := io.ReadAll(file)
		closeErr := file.Close()
		if readErr != nil {
			return nil, errInvalidMultipartFile
		}
		if closeErr != nil {
			return nil, errInvalidMultipartFile
		}
		mimeType := header.Header.Get("Content-Type")
		if strings.TrimSpace(mimeType) == "" {
			mimeType = http.DetectContentType(data)
		}
		files = append(files, UploadedFile{Filename: header.Filename, MIMEType: mimeType, Data: data})
	}

	return files, nil
}

func writeChatError(c fiber.Ctx, err error) error {
	if errors.Is(err, ErrChatSessionNotFound) {
		return c.Status(http.StatusNotFound).JSON(fiber.Map{errorField: ErrChatSessionNotFound.Error()})
	}
	if errors.Is(err, ErrAgentExecutionNotFound) {
		return c.Status(http.StatusNotFound).JSON(fiber.Map{errorField: ErrAgentExecutionNotFound.Error()})
	}
	if errors.Is(err, ErrAgentExecutionActive) {
		return c.Status(http.StatusConflict).JSON(fiber.Map{errorField: ErrAgentExecutionActive.Error()})
	}

	return c.Status(http.StatusInternalServerError).JSON(fiber.Map{errorField: "chat message failed"})
}
