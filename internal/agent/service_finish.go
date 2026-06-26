package agent

import (
	"context"
	"errors"
	"fmt"
)

func (service Service) finishBackgroundExecution(ctx context.Context, execution AgentExecution, sessionID string, response AgentExecutionResponse, executionErr error) {
	if executionErr != nil {
		service.failAndPublishExecution(execution, sessionID, response, executionErr)
		return
	}
	assistantMessage, err := service.createAssistantMessage(ctx, sessionID, response)
	if err != nil {
		service.failAndPublishExecution(execution, sessionID, response, err)
		return
	}
	if response.Status == AgentExecutionStatusInterrupted && response.Interruption != nil {
		service.publishInterruptionCreated(execution, sessionID, response, assistantMessage)
		return
	}
	if err := service.completeExecution(ctx, execution, response); err != nil {
		failed := AgentExecutionResponse{ExecutionID: execution.ID, Status: AgentExecutionStatusFailed, Error: RedactText(err.Error())}
		service.eventBus.Publish(sessionID, ChatEvent{Type: EventTypeExecutionFailed, ChatID: sessionID, ExecutionID: execution.ID, AgentExecution: &failed, Error: failed.Error})
		return
	}
	if assistantMessage != nil {
		service.publishMessageCreated(*assistantMessage)
	}
	service.eventBus.Publish(sessionID, ChatEvent{Type: EventTypeExecutionCompleted, ChatID: sessionID, ExecutionID: execution.ID, AgentExecution: &response})
}

func (service Service) failAndPublishExecution(execution AgentExecution, sessionID string, response AgentExecutionResponse, err error) {
	failed, failErr := service.failExecution(execution, response, err)
	if failErr != nil {
		failed.Error = RedactText(failErr.Error())
	}
	service.eventBus.Publish(sessionID, ChatEvent{Type: EventTypeExecutionFailed, ChatID: sessionID, ExecutionID: execution.ID, AgentExecution: &failed, Error: failed.Error})
}

func (service Service) publishInterruptionCreated(execution AgentExecution, sessionID string, response AgentExecutionResponse, assistantMessage *ChatMessage) {
	service.eventBus.Publish(sessionID, ChatEvent{
		Type:           EventTypeInterruptionCreated,
		ChatID:         sessionID,
		ExecutionID:    execution.ID,
		InterruptionID: response.Interruption.ID,
		Interruption:   response.Interruption,
	})
	if assistantMessage != nil {
		service.publishMessageCreated(*assistantMessage)
	}
}

func (service Service) publishMessageCreated(message ChatMessage) {
	message = chatMessageMetadata(message)
	service.eventBus.Publish(message.SessionID, ChatEvent{
		Type:        EventTypeMessageCreated,
		ChatID:      message.SessionID,
		MessageID:   message.ID,
		ExecutionID: message.ExecutionID,
		Message:     &message,
	})
}

func (service Service) createAssistantMessage(ctx context.Context, sessionID string, response AgentExecutionResponse) (*ChatMessage, error) {
	content := response.Answer
	if response.Interruption != nil {
		content = response.Interruption.Message
	}
	if content == "" {
		return nil, errAssistantMessageEmpty
	}
	message, err := service.executionStore.createChatMessage(ctx, CreateChatMessageRecord{
		SessionID:   sessionID,
		ExecutionID: response.ExecutionID,
		Role:        ChatMessageRoleAssistant,
		Content:     content,
	})
	if err != nil {
		return nil, fmt.Errorf("create assistant message: %w", err)
	}
	return &message, nil
}

func (service Service) completeExecution(ctx context.Context, execution AgentExecution, response AgentExecutionResponse) error {
	if response.Status == AgentExecutionStatusInterrupted {
		return nil
	}

	execution.Status = response.Status
	execution.ErrorSummary = response.Error
	if err := service.executionStore.finishExecution(ctx, execution); err != nil {
		return fmt.Errorf("finish execution: %w", err)
	}
	return nil
}

func (service Service) failExecution(execution AgentExecution, response AgentExecutionResponse, executionErr error) (AgentExecutionResponse, error) {
	execution.Status = AgentExecutionStatusFailed
	execution.ErrorSummary = executionErr.Error()
	ctx, cancel := context.WithTimeout(context.Background(), service.failedExecutionFinishTimeout)
	defer cancel()
	if err := service.executionStore.finishExecution(ctx, execution); err != nil {
		executionErr = errors.Join(executionErr, fmt.Errorf("finish failed execution: %w", err))
	}

	response.ExecutionID = execution.ID
	response.Status = AgentExecutionStatusFailed
	response.Error = RedactText(executionErr.Error())
	return response, executionErr
}

func finalExecutionResponse(executionID string, action PlannerAction) AgentExecutionResponse {
	return AgentExecutionResponse{
		ExecutionID: executionID,
		Status:      AgentExecutionStatusSucceeded,
		Answer:      RedactText(action.Answer),
		Outputs:     redactOutputs(action.Outputs),
	}
}
