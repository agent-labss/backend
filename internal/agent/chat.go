package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

const (
	ChatSessionStatusOpen     ChatSessionStatus = "open"
	ChatSessionStatusArchived ChatSessionStatus = "archived"
)

const (
	ChatMessageRoleUser      ChatMessageRole = "user"
	ChatMessageRoleAssistant ChatMessageRole = "assistant"
)

const (
	ChatMessageStatusCompleted ChatMessageStatus = "completed"
	ChatMessageStatusFailed    ChatMessageStatus = "failed"
)

const (
	resumePayloadResponseMessageID   = "response_message_id"
	resumePayloadResponseMessage     = "response_message"
	resumePayloadResponseAttachments = "response_attachments"
)

type ChatSessionStatus string
type ChatMessageRole string
type ChatMessageStatus string

type CreateChatRequest struct {
	Title string `json:"title,omitempty"`
}

type CreateChatMessageRequest struct {
	Message     string       `json:"message"`
	Attachments []Attachment `json:"attachments,omitempty"`
}

type SubmitChatMessageResponse struct {
	ChatID      string               `json:"chat_id"`
	UserMessage ChatMessage          `json:"user_message"`
	ExecutionID string               `json:"execution_id"`
	Status      AgentExecutionStatus `json:"status"`
}

type CreateChatSessionRecord struct {
	Title string
}

type CreateChatMessageRecord struct {
	SessionID   string
	ExecutionID string
	Role        ChatMessageRole
	Content     string
	Attachments []Attachment
}

type ChatSession struct {
	ID        string            `json:"id"`
	Title     string            `json:"title,omitempty"`
	Status    ChatSessionStatus `json:"status"`
	CreatedAt time.Time         `json:"created_at,omitempty"`
	UpdatedAt time.Time         `json:"updated_at,omitempty"`
}

type ChatMessage struct {
	ID          string            `json:"id"`
	SessionID   string            `json:"session_id"`
	ExecutionID string            `json:"execution_id,omitempty"`
	Role        ChatMessageRole   `json:"role"`
	Content     string            `json:"content"`
	Status      ChatMessageStatus `json:"status"`
	Sequence    int               `json:"sequence"`
	Attachments []Attachment      `json:"attachments,omitempty"`
	CreatedAt   time.Time         `json:"created_at,omitempty"`
	CompletedAt time.Time         `json:"completed_at,omitempty"`
	Error       string            `json:"error,omitempty"`
}

type ChatMessageResponse struct {
	ChatID           string                 `json:"chat_id"`
	UserMessage      ChatMessage            `json:"user_message"`
	AssistantMessage *ChatMessage           `json:"assistant_message,omitempty"`
	AgentExecution   AgentExecutionResponse `json:"execution"`
	Interruption     *Interruption          `json:"interruption,omitempty"`
}

func (service Service) CreateChat(ctx context.Context, request CreateChatRequest) (ChatSession, error) {
	session, err := service.executionStore.createChatSession(ctx, CreateChatSessionRecord(request))
	if err != nil {
		return ChatSession{}, fmt.Errorf("create chat session: %w", err)
	}
	return session, nil
}

func (service Service) GetChat(ctx context.Context, sessionID string) (ChatSession, error) {
	session, err := service.executionStore.getChatSession(ctx, sessionID)
	if err != nil {
		return ChatSession{}, fmt.Errorf("get chat session: %w", err)
	}
	return session, nil
}

func (service Service) ListChatMessages(ctx context.Context, sessionID string) ([]ChatMessage, error) {
	messages, err := service.executionStore.listChatMessages(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("list chat messages: %w", err)
	}
	return messages, nil
}

func (service Service) SubscribeChatEvents(ctx context.Context, chatID string) (<-chan ChatEvent, func(), error) {
	if _, err := service.executionStore.getChatSession(ctx, chatID); err != nil {
		return nil, nil, fmt.Errorf("get chat session: %w", err)
	}
	events, unsubscribe := service.eventBus.Subscribe(chatID)
	return events, unsubscribe, nil
}

func (service Service) CreateChatMessage(ctx context.Context, sessionID string, request CreateChatMessageRequest) (SubmitChatMessageResponse, error) {
	if _, err := service.executionStore.getChatSession(ctx, sessionID); err != nil {
		return SubmitChatMessageResponse{}, fmt.Errorf("get chat session: %w", err)
	}
	active, err := service.executionStore.activeInterruption(ctx, sessionID)
	if errors.Is(err, ErrNoActiveInterruption) {
		active = nil
	} else if err != nil {
		return SubmitChatMessageResponse{}, fmt.Errorf("active interruption: %w", err)
	}
	if active == nil {
		if err := service.ensureNoActiveExecution(ctx, sessionID); err != nil {
			return SubmitChatMessageResponse{}, err
		}
	}

	userMessage, err := service.executionStore.createChatMessage(ctx, CreateChatMessageRecord{
		SessionID:   sessionID,
		Role:        ChatMessageRoleUser,
		Content:     request.Message,
		Attachments: request.Attachments,
	})
	if err != nil {
		return SubmitChatMessageResponse{}, fmt.Errorf("create user message: %w", err)
	}
	userMessage = chatMessageMetadata(userMessage)
	service.publishMessageCreated(userMessage)

	if active != nil {
		return service.submitResumeChatExecution(ctx, userMessage, *active, request)
	}
	return service.submitNewChatExecution(ctx, userMessage, request)
}

func (service Service) ensureNoActiveExecution(ctx context.Context, sessionID string) error {
	execution, err := service.executionStore.activeExecution(ctx, sessionID)
	if errors.Is(err, ErrAgentExecutionNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("active execution: %w", err)
	}
	if execution != nil {
		return ErrAgentExecutionActive
	}
	return nil
}

func (service Service) submitNewChatExecution(ctx context.Context, userMessage ChatMessage, request CreateChatMessageRequest) (SubmitChatMessageResponse, error) {
	execution, err := service.executionStore.startExecution(ctx, CreateAgentExecutionRecord{
		SessionID:        userMessage.SessionID,
		TriggerMessageID: userMessage.ID,
	})
	if err != nil {
		return SubmitChatMessageResponse{}, fmt.Errorf("start execution: %w", err)
	}
	service.eventBus.Publish(userMessage.SessionID, ChatEvent{Type: EventTypeExecutionStarted, ChatID: userMessage.SessionID, ExecutionID: execution.ID})
	service.schedule(func(ctx context.Context) {
		service.executeNewChatExecution(ctx, execution, userMessage.SessionID, executionRequest(request))
	})
	return SubmitChatMessageResponse{
		ChatID:      userMessage.SessionID,
		UserMessage: userMessage,
		ExecutionID: execution.ID,
		Status:      AgentExecutionStatusRunning,
	}, nil
}

func (service Service) submitResumeChatExecution(ctx context.Context, userMessage ChatMessage, interruption Interruption, request CreateChatMessageRequest) (SubmitChatMessageResponse, error) {
	stateRecord, err := service.executionStore.getExecutionState(ctx, interruption.ExecutionID)
	if err != nil {
		return SubmitChatMessageResponse{}, fmt.Errorf("get execution state: %w", err)
	}
	execution := stateRecord.AgentExecution
	execution.Status = AgentExecutionStatusRunning
	resumeRequest, err := service.resumedExecutionRequest(ctx, execution, userMessage, request)
	if err != nil {
		return SubmitChatMessageResponse{}, err
	}
	if err := service.executionStore.resolveInterruption(ctx, interruption.ID, userMessage.ID, InterruptionStatusResolved); err != nil {
		return SubmitChatMessageResponse{}, fmt.Errorf("resolve interruption: %w", err)
	}
	resolved := resolvedInterruption(interruption, userMessage)

	service.eventBus.Publish(userMessage.SessionID, ChatEvent{
		Type:           EventTypeInterruptionResolved,
		ChatID:         userMessage.SessionID,
		ExecutionID:    execution.ID,
		InterruptionID: resolved.ID,
		Interruption:   &resolved,
	})
	service.eventBus.Publish(userMessage.SessionID, ChatEvent{Type: EventTypeExecutionResumed, ChatID: userMessage.SessionID, ExecutionID: execution.ID})
	service.schedule(func(ctx context.Context) {
		service.executeResumedChatExecution(ctx, execution, userMessage.SessionID, resumeRequest, &resolved, stateRecord.Observations)
	})
	return SubmitChatMessageResponse{
		ChatID:      userMessage.SessionID,
		UserMessage: userMessage,
		ExecutionID: execution.ID,
		Status:      AgentExecutionStatusRunning,
	}, nil
}

func (service Service) resumedExecutionRequest(ctx context.Context, execution AgentExecution, userMessage ChatMessage, request CreateChatMessageRequest) (executionRequest, error) {
	resumeRequest := executionRequest{Message: userMessage.Content, Attachments: request.Attachments}
	if execution.TriggerMessageID == "" {
		return resumeRequest, nil
	}

	messages, err := service.executionStore.listChatMessages(ctx, execution.SessionID)
	if err != nil {
		return executionRequest{}, fmt.Errorf("list chat messages: %w", err)
	}
	for _, message := range messages {
		if message.ID == execution.TriggerMessageID {
			resumeRequest.Message = message.Content
			return resumeRequest, nil
		}
	}

	return resumeRequest, nil
}

func resolvedInterruption(interruption Interruption, userMessage ChatMessage) Interruption {
	resolved := interruption
	resolved.Status = InterruptionStatusResolved
	resolved.RespondedAt = time.Now().UTC()
	resolved.Payload = resolvedInterruptionPayload(interruption.Payload, userMessage)
	return resolved
}

func resolvedInterruptionPayload(raw json.RawMessage, userMessage ChatMessage) json.RawMessage {
	payload := map[string]any{}
	if len(raw) > 0 {
		var existing map[string]any
		if err := json.Unmarshal(raw, &existing); err == nil && existing != nil {
			payload = existing
		}
	}
	payload[resumePayloadResponseMessageID] = userMessage.ID
	payload[resumePayloadResponseMessage] = userMessage.Content
	if len(userMessage.Attachments) > 0 {
		payload[resumePayloadResponseAttachments] = attachmentMetadataList(userMessage.Attachments)
	}
	return mustMarshalJSON(payload)
}

func chatMessageMetadata(message ChatMessage) ChatMessage {
	message.Attachments = attachmentMetadataList(message.Attachments)
	return message
}
