package agent

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"ai/backend/internal/database"
	"ai/backend/internal/database/generated"
)

var (
	ErrDatabaseMissing          = errors.New("agent database is missing")
	ErrAgentExecutionNotFound   = errors.New("agent execution not found")
	ErrAgentExecutionNotWaiting = errors.New("agent execution is not waiting for user")
	ErrAgentExecutionActive     = errors.New("agent execution is already active")
	ErrChatSessionNotFound      = errors.New("chat session not found")
	ErrNoActiveInterruption     = errors.New("active interruption not found")
)

type Repository struct {
	database *gorm.DB
}

func NewRepository(db *gorm.DB) Repository {
	if db == nil {
		return Repository{}
	}

	return Repository{database: db}
}

func (repository Repository) StartAgentExecution(ctx context.Context, record CreateAgentExecutionRecord) (AgentExecution, error) {
	if repository.database == nil {
		return AgentExecution{}, ErrDatabaseMissing
	}

	execution := AgentExecution{
		ID:               newRuntimeID("exec"),
		SessionID:        record.SessionID,
		TriggerMessageID: record.TriggerMessageID,
		Status:           AgentExecutionStatusRunning,
		StartedAt:        time.Now().UTC(),
	}
	executionRecord := database.AgentExecution{
		ID:               execution.ID,
		SessionID:        execution.SessionID,
		TriggerMessageID: execution.TriggerMessageID,
		Status:           string(execution.Status),
		ErrorSummary:     "",
		StartedAt:        execution.StartedAt,
	}
	if err := generated.AgentExecutionQueries[database.AgentExecution](repository.database).Create(ctx, &executionRecord); err != nil {
		if isUniqueConstraintError(err) {
			return AgentExecution{}, ErrAgentExecutionActive
		}
		return AgentExecution{}, fmt.Errorf("start execution: %w", err)
	}

	return execution, nil
}

func (repository Repository) CreateChatSession(ctx context.Context, record CreateChatSessionRecord) (ChatSession, error) {
	if repository.database == nil {
		return ChatSession{}, ErrDatabaseMissing
	}

	now := time.Now().UTC()
	session := ChatSession{
		ID:        newRuntimeID("chat"),
		Title:     RedactText(record.Title),
		Status:    ChatSessionStatusOpen,
		CreatedAt: now,
		UpdatedAt: now,
	}
	sessionRecord := database.ChatSession{
		ID:        session.ID,
		Title:     session.Title,
		Status:    string(session.Status),
		CreatedAt: session.CreatedAt,
		UpdatedAt: session.UpdatedAt,
	}
	if err := generated.ChatSessionQueries[database.ChatSession](repository.database).Create(ctx, &sessionRecord); err != nil {
		return ChatSession{}, fmt.Errorf("create chat session: %w", err)
	}

	return session, nil
}

func (repository Repository) GetChatSession(ctx context.Context, sessionID string) (ChatSession, error) {
	if repository.database == nil {
		return ChatSession{}, ErrDatabaseMissing
	}

	record, err := generated.ChatSessionQueries[database.ChatSession](repository.database).GetByID(ctx, sessionID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ChatSession{}, ErrChatSessionNotFound
		}
		return ChatSession{}, fmt.Errorf("get chat session: %w", err)
	}
	if record.ID == "" {
		return ChatSession{}, ErrChatSessionNotFound
	}

	return chatSessionFromRecord(record), nil
}

func (repository Repository) ListChatMessages(ctx context.Context, sessionID string) ([]ChatMessage, error) {
	if repository.database == nil {
		return nil, ErrDatabaseMissing
	}

	records, err := generated.ChatMessageQueries[database.ChatMessage](repository.database).ListBySessionID(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("list chat messages: %w", err)
	}
	attachments, err := repository.chatAttachments(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	messages := make([]ChatMessage, 0, len(records))
	for _, record := range records {
		message := chatMessageFromRecord(record)
		message.Attachments = attachmentMetadataList(attachments[record.ID])
		messages = append(messages, message)
	}
	return messages, nil
}

func (repository Repository) CreateChatMessage(ctx context.Context, record CreateChatMessageRecord) (ChatMessage, error) {
	if repository.database == nil {
		return ChatMessage{}, ErrDatabaseMissing
	}

	now := time.Now().UTC()
	var message ChatMessage
	if err := repository.database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		messageQueries := generated.ChatMessageQueries[database.ChatMessage](tx)
		maxSequence, err := messageQueries.MaxSequenceBySessionID(ctx, record.SessionID)
		if err != nil {
			return fmt.Errorf("next chat message sequence: %w", err)
		}
		message = ChatMessage{
			ID:          newRuntimeID("msg"),
			SessionID:   record.SessionID,
			ExecutionID: record.ExecutionID,
			Role:        record.Role,
			Content:     RedactText(record.Content),
			Status:      ChatMessageStatusCompleted,
			Sequence:    maxSequence + 1,
			Attachments: attachmentMetadataList(record.Attachments),
			CreatedAt:   now,
			CompletedAt: now,
		}
		messageRecord := database.ChatMessage{
			ID:          message.ID,
			SessionID:   message.SessionID,
			ExecutionID: message.ExecutionID,
			Role:        string(message.Role),
			Content:     message.Content,
			Status:      string(message.Status),
			Sequence:    message.Sequence,
			CreatedAt:   message.CreatedAt,
			CompletedAt: sql.NullTime{Time: message.CompletedAt, Valid: true},
		}
		if err := messageQueries.Create(ctx, &messageRecord); err != nil {
			return fmt.Errorf("create chat message: %w", err)
		}
		if err := saveChatAttachments(ctx, tx, message, record.Attachments); err != nil {
			return err
		}
		return generated.ChatSessionQueries[database.ChatSession](tx).UpdateUpdatedAtByID(ctx, now, record.SessionID)
	}); err != nil {
		return ChatMessage{}, fmt.Errorf("create chat message transaction: %w", err)
	}

	return message, nil
}

func saveChatAttachments(ctx context.Context, db *gorm.DB, message ChatMessage, attachments []Attachment) error {
	for _, attachment := range attachments {
		record := database.ChatAttachment{
			ID:             attachment.ID,
			SessionID:      message.SessionID,
			MessageID:      message.ID,
			Filename:       RedactText(attachment.Filename),
			MIMEType:       attachment.MIMEType,
			Kind:           string(attachment.Kind),
			SizeBytes:      attachment.Size,
			ProviderFileID: attachment.FileID,
			CreatedAt:      time.Now().UTC(),
		}
		if err := generated.ChatAttachmentQueries[database.ChatAttachment](db).Create(ctx, &record); err != nil {
			return fmt.Errorf("save chat attachment: %w", err)
		}
	}

	return nil
}

func (repository Repository) FinishAgentExecution(ctx context.Context, execution AgentExecution) error {
	if repository.database == nil {
		return ErrDatabaseMissing
	}

	if err := generated.AgentExecutionQueries[database.AgentExecution](repository.database).FinishByID(ctx, string(execution.Status), RedactText(execution.ErrorSummary), sql.NullTime{Time: time.Now().UTC(), Valid: true}, execution.ID); err != nil {
		return fmt.Errorf("finish execution: %w", err)
	}

	return nil
}

func (repository Repository) GetAgentExecutionState(ctx context.Context, executionID string) (AgentExecutionStateRecord, error) {
	if repository.database == nil {
		return AgentExecutionStateRecord{}, ErrDatabaseMissing
	}

	executionRecord, err := repository.executionRecord(ctx, executionID)
	if err != nil {
		return AgentExecutionStateRecord{}, err
	}
	state := AgentExecutionStateRecord{AgentExecution: agentExecutionFromRecord(executionRecord)}
	if err := repository.loadExecutionStateRelations(ctx, &state); err != nil {
		return AgentExecutionStateRecord{}, err
	}

	return state, nil
}

func (repository Repository) ActiveAgentExecution(ctx context.Context, sessionID string) (*AgentExecution, error) {
	if repository.database == nil {
		return nil, ErrDatabaseMissing
	}

	record, err := generated.AgentExecutionQueries[database.AgentExecution](repository.database).ActiveBySessionID(ctx, sessionID, string(AgentExecutionStatusRunning), string(AgentExecutionStatusInterrupted))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrAgentExecutionNotFound
		}
		return nil, fmt.Errorf("active execution: %w", err)
	}
	if record.ID == "" {
		return nil, ErrAgentExecutionNotFound
	}
	execution := agentExecutionFromRecord(record)
	return &execution, nil
}

func (repository Repository) executionRecord(ctx context.Context, executionID string) (database.AgentExecution, error) {
	executionRecord, err := generated.AgentExecutionQueries[database.AgentExecution](repository.database).GetByID(ctx, executionID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return database.AgentExecution{}, ErrAgentExecutionNotFound
		}
		return database.AgentExecution{}, fmt.Errorf("get execution: %w", err)
	}
	if executionRecord.ID == "" {
		return database.AgentExecution{}, ErrAgentExecutionNotFound
	}
	return executionRecord, nil
}

func (repository Repository) loadExecutionStateRelations(ctx context.Context, state *AgentExecutionStateRecord) error {
	interruptions, activeInterruption, err := repository.executionInterruptions(ctx, state.AgentExecution.ID)
	if err != nil {
		return err
	}
	state.Interruptions = interruptions
	state.ActiveInterruption = activeInterruption

	observations, err := repository.executionObservations(ctx, state.AgentExecution.ID)
	if err != nil {
		return err
	}
	state.Observations = observations

	return nil
}

func (repository Repository) CreateInterruption(ctx context.Context, interruption Interruption) (Interruption, error) {
	if repository.database == nil {
		return Interruption{}, ErrDatabaseMissing
	}

	now := time.Now().UTC()
	if interruption.ID == "" {
		interruption.ID = newRuntimeID("int")
	}
	if interruption.Type == "" {
		interruption.Type = InterruptionTypeInputRequest
	}
	if interruption.Status == "" {
		interruption.Status = InterruptionStatusAwaitingReview
	}
	interruption.Message = RedactText(interruption.Message)
	interruption.CreatedAt = now
	payload := interruption.Payload
	if len(payload) == 0 {
		payload = json.RawMessage(`{}`)
	}

	record := database.AgentInterruption{
		ID:          interruption.ID,
		SessionID:   interruption.SessionID,
		ExecutionID: interruption.ExecutionID,
		Type:        string(interruption.Type),
		Status:      string(interruption.Status),
		Message:     interruption.Message,
		Payload:     database.JSON(payload),
		CreatedAt:   now,
	}
	if err := generated.AgentInterruptionQueries[database.AgentInterruption](repository.database).Create(ctx, &record); err != nil {
		return Interruption{}, fmt.Errorf("create interruption: %w", err)
	}

	return interruption, nil
}

func (repository Repository) MarkAgentExecutionInterrupted(ctx context.Context, execution AgentExecution, _ Interruption) error {
	if repository.database == nil {
		return ErrDatabaseMissing
	}

	if err := generated.AgentExecutionQueries[database.AgentExecution](repository.database).MarkInterruptedByID(ctx, string(AgentExecutionStatusInterrupted), execution.ID); err != nil {
		return fmt.Errorf("mark execution interrupted: %w", err)
	}

	return nil
}

func (repository Repository) ResolveInterruption(ctx context.Context, interruptionID string, messageID string, status InterruptionStatus) error {
	if repository.database == nil {
		return ErrDatabaseMissing
	}

	if status == "" {
		status = InterruptionStatusResolved
	}
	record, err := generated.AgentInterruptionQueries[database.AgentInterruption](repository.database).ResolveAwaitingByID(ctx, string(status), messageID, sql.NullTime{Time: time.Now().UTC(), Valid: true}, interruptionID, string(InterruptionStatusAwaitingReview))
	if err != nil {
		return fmt.Errorf("resolve interruption: %w", err)
	}
	if record.ID == "" {
		return ErrAgentExecutionNotWaiting
	}

	return nil
}

func (repository Repository) ActiveInterruption(ctx context.Context, sessionID string) (*Interruption, error) {
	if repository.database == nil {
		return nil, ErrDatabaseMissing
	}

	record, err := generated.AgentInterruptionQueries[database.AgentInterruption](repository.database).ActiveBySessionID(ctx, sessionID, string(InterruptionStatusAwaitingReview))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNoActiveInterruption
		}
		return nil, fmt.Errorf("active interruption: %w", err)
	}
	if record.ID == "" {
		return nil, ErrNoActiveInterruption
	}
	interruption := interruptionFromRecord(record)
	return &interruption, nil
}

func (repository Repository) SaveObservation(ctx context.Context, record ObservationRecord) error {
	if repository.database == nil {
		return ErrDatabaseMissing
	}

	payload, err := json.Marshal(RedactJSONValue(record.Observation))
	if err != nil {
		return fmt.Errorf("marshal observation: %w", err)
	}
	observationRecord := database.AgentExecutionObservation{
		ID:          newRuntimeID("obs"),
		ExecutionID: record.ExecutionID,
		StepOrder:   record.StepOrder,
		Payload:     database.JSON(payload),
		CreatedAt:   time.Now().UTC(),
	}
	if err := generated.AgentExecutionObservationQueries[database.AgentExecutionObservation](repository.database).Create(ctx, &observationRecord); err != nil {
		return fmt.Errorf("save observation: %w", err)
	}

	return nil
}

func (repository Repository) SaveStep(ctx context.Context, step StepRecord) error {
	if repository.database == nil {
		return ErrDatabaseMissing
	}

	record := database.AgentExecutionStep{
		ID:            newRuntimeID("step"),
		ExecutionID:   step.ExecutionID,
		StepOrder:     step.StepOrder,
		ToolID:        step.ToolID,
		InputSummary:  database.JSON(step.InputSummary),
		OutputSummary: database.JSON(step.OutputSummary),
		DurationMS:    step.DurationMS,
		Status:        string(step.Status),
		ErrorSummary:  RedactText(step.ErrorSummary),
		CreatedAt:     time.Now().UTC(),
	}
	if err := generated.AgentExecutionStepQueries[database.AgentExecutionStep](repository.database).Create(ctx, &record); err != nil {
		return fmt.Errorf("save step: %w", err)
	}

	return nil
}

func (repository Repository) chatAttachments(ctx context.Context, sessionID string) (map[string][]Attachment, error) {
	records, err := generated.ChatAttachmentQueries[database.ChatAttachment](repository.database).ListBySessionID(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("list chat attachments: %w", err)
	}
	attachments := make(map[string][]Attachment)
	for _, record := range records {
		attachments[record.MessageID] = append(attachments[record.MessageID], Attachment{
			ID:       record.ID,
			Filename: record.Filename,
			MIMEType: record.MIMEType,
			Kind:     AttachmentKind(record.Kind),
			Size:     record.SizeBytes,
			FileID:   record.ProviderFileID,
		})
	}
	return attachments, nil
}

func (repository Repository) executionInterruptions(ctx context.Context, executionID string) ([]Interruption, *Interruption, error) {
	records, err := generated.AgentInterruptionQueries[database.AgentInterruption](repository.database).ListByExecutionID(ctx, executionID)
	if err != nil {
		return nil, nil, fmt.Errorf("list execution interruptions: %w", err)
	}
	interruptions := make([]Interruption, 0, len(records))
	var active *Interruption
	for _, record := range records {
		interruption := interruptionFromRecord(record)
		interruptions = append(interruptions, interruption)
		if interruption.Status == InterruptionStatusAwaitingReview {
			activeCopy := interruption
			active = &activeCopy
		}
	}
	return interruptions, active, nil
}

func (repository Repository) executionObservations(ctx context.Context, executionID string) ([]Observation, error) {
	records, err := generated.AgentExecutionObservationQueries[database.AgentExecutionObservation](repository.database).ListByExecutionID(ctx, executionID)
	if err != nil {
		return nil, fmt.Errorf("list observations: %w", err)
	}
	observations := make([]Observation, 0, len(records))
	for _, record := range records {
		var observation Observation
		if err := json.Unmarshal(record.Payload, &observation); err != nil {
			return nil, fmt.Errorf("decode observation: %w", err)
		}
		observations = append(observations, observation)
	}
	return observations, nil
}

func agentExecutionFromRecord(record database.AgentExecution) AgentExecution {
	execution := AgentExecution{
		ID:               record.ID,
		SessionID:        record.SessionID,
		TriggerMessageID: record.TriggerMessageID,
		Status:           AgentExecutionStatus(record.Status),
		ErrorSummary:     record.ErrorSummary,
		StartedAt:        record.StartedAt,
	}
	if record.FinishedAt.Valid {
		execution.FinishedAt = record.FinishedAt.Time
	}
	return execution
}

func chatSessionFromRecord(record database.ChatSession) ChatSession {
	return ChatSession{
		ID:        record.ID,
		Title:     record.Title,
		Status:    ChatSessionStatus(record.Status),
		CreatedAt: record.CreatedAt,
		UpdatedAt: record.UpdatedAt,
	}
}

func chatMessageFromRecord(record database.ChatMessage) ChatMessage {
	message := ChatMessage{
		ID:          record.ID,
		SessionID:   record.SessionID,
		ExecutionID: record.ExecutionID,
		Role:        ChatMessageRole(record.Role),
		Content:     record.Content,
		Status:      ChatMessageStatus(record.Status),
		Sequence:    record.Sequence,
		CreatedAt:   record.CreatedAt,
		Error:       record.ErrorSummary,
	}
	if record.CompletedAt.Valid {
		message.CompletedAt = record.CompletedAt.Time
	}
	return message
}

func interruptionFromRecord(record database.AgentInterruption) Interruption {
	interruption := Interruption{
		ID:          record.ID,
		SessionID:   record.SessionID,
		ExecutionID: record.ExecutionID,
		Type:        InterruptionType(record.Type),
		Status:      InterruptionStatus(record.Status),
		Message:     record.Message,
		Payload:     json.RawMessage(record.Payload),
		CreatedAt:   record.CreatedAt,
	}
	if record.ResolvedAt.Valid {
		interruption.RespondedAt = record.ResolvedAt.Time
	}
	return interruption
}

func newRuntimeID(prefix string) string {
	return fmt.Sprintf("%s_%d", prefix, time.Now().UTC().UnixNano())
}

func isUniqueConstraintError(err error) bool {
	return strings.Contains(err.Error(), "UNIQUE constraint failed")
}
