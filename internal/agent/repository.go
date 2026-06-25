package agent

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	"ai/backend/internal/database"
	"ai/backend/internal/database/generated"
)

var (
	ErrDatabaseMissing      = errors.New("agent database is missing")
	ErrRunNotFound          = errors.New("agent run not found")
	ErrRunNotWaiting        = errors.New("agent run is not waiting for user")
	ErrChatSessionNotFound  = errors.New("chat session not found")
	ErrNoActiveInterruption = errors.New("active interruption not found")
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

func (repository Repository) StartRun(ctx context.Context, record CreateRunRecord) (Run, error) {
	if repository.database == nil {
		return Run{}, ErrDatabaseMissing
	}

	run := Run{
		ID:               newRuntimeID("run"),
		SessionID:        record.SessionID,
		TriggerMessageID: record.TriggerMessageID,
		Status:           RunStatusRunning,
		StartedAt:        time.Now().UTC(),
	}
	runRecord := database.AgentRun{
		ID:               run.ID,
		SessionID:        run.SessionID,
		TriggerMessageID: run.TriggerMessageID,
		Status:           string(run.Status),
		ErrorSummary:     "",
		StartedAt:        run.StartedAt,
	}
	if err := generated.AgentRunQueries[database.AgentRun](repository.database).Create(ctx, &runRecord); err != nil {
		return Run{}, fmt.Errorf("start run: %w", err)
	}

	return run, nil
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
		message.Attachments = attachments[record.ID]
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
			RunID:       record.RunID,
			Role:        record.Role,
			Content:     RedactText(record.Content),
			Status:      ChatMessageStatusCompleted,
			Sequence:    maxSequence + 1,
			Attachments: record.Attachments,
			CreatedAt:   now,
			CompletedAt: now,
		}
		messageRecord := database.ChatMessage{
			ID:          message.ID,
			SessionID:   message.SessionID,
			RunID:       message.RunID,
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

func (repository Repository) FinishRun(ctx context.Context, run Run) error {
	if repository.database == nil {
		return ErrDatabaseMissing
	}

	if err := generated.AgentRunQueries[database.AgentRun](repository.database).FinishByID(ctx, string(run.Status), RedactText(run.ErrorSummary), sql.NullTime{Time: time.Now().UTC(), Valid: true}, run.ID); err != nil {
		return fmt.Errorf("finish run: %w", err)
	}

	return nil
}

func (repository Repository) GetRunState(ctx context.Context, runID string) (RunStateRecord, error) {
	if repository.database == nil {
		return RunStateRecord{}, ErrDatabaseMissing
	}

	runRecord, err := repository.runRecord(ctx, runID)
	if err != nil {
		return RunStateRecord{}, err
	}
	state := RunStateRecord{Run: runFromRecord(runRecord)}
	if err := repository.loadRunStateRelations(ctx, &state); err != nil {
		return RunStateRecord{}, err
	}

	return state, nil
}

func (repository Repository) runRecord(ctx context.Context, runID string) (database.AgentRun, error) {
	runRecord, err := generated.AgentRunQueries[database.AgentRun](repository.database).GetByID(ctx, runID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return database.AgentRun{}, ErrRunNotFound
		}
		return database.AgentRun{}, fmt.Errorf("get run: %w", err)
	}
	if runRecord.ID == "" {
		return database.AgentRun{}, ErrRunNotFound
	}
	return runRecord, nil
}

func (repository Repository) loadRunStateRelations(ctx context.Context, state *RunStateRecord) error {
	interruptions, activeInterruption, err := repository.runInterruptions(ctx, state.Run.ID)
	if err != nil {
		return err
	}
	state.Interruptions = interruptions
	state.ActiveInterruption = activeInterruption

	observations, err := repository.runObservations(ctx, state.Run.ID)
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
		ID:        interruption.ID,
		SessionID: interruption.SessionID,
		RunID:     interruption.RunID,
		Type:      string(interruption.Type),
		Status:    string(interruption.Status),
		Message:   interruption.Message,
		Payload:   database.JSON(payload),
		CreatedAt: now,
	}
	if err := generated.AgentInterruptionQueries[database.AgentInterruption](repository.database).Create(ctx, &record); err != nil {
		return Interruption{}, fmt.Errorf("create interruption: %w", err)
	}

	return interruption, nil
}

func (repository Repository) MarkRunInterrupted(ctx context.Context, run Run, _ Interruption) error {
	if repository.database == nil {
		return ErrDatabaseMissing
	}

	if err := generated.AgentRunQueries[database.AgentRun](repository.database).MarkInterruptedByID(ctx, string(RunStatusInterrupted), run.ID); err != nil {
		return fmt.Errorf("mark run interrupted: %w", err)
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
	if err := generated.AgentInterruptionQueries[database.AgentInterruption](repository.database).ResolveAwaitingByID(ctx, string(status), messageID, sql.NullTime{Time: time.Now().UTC(), Valid: true}, interruptionID, string(InterruptionStatusAwaitingReview)); err != nil {
		return fmt.Errorf("resolve interruption: %w", err)
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
	observationRecord := database.AgentRunObservation{
		ID:        newRuntimeID("obs"),
		RunID:     record.RunID,
		StepOrder: record.StepOrder,
		Payload:   database.JSON(payload),
		CreatedAt: time.Now().UTC(),
	}
	if err := generated.AgentRunObservationQueries[database.AgentRunObservation](repository.database).Create(ctx, &observationRecord); err != nil {
		return fmt.Errorf("save observation: %w", err)
	}

	return nil
}

func (repository Repository) SaveStep(ctx context.Context, step StepRecord) error {
	if repository.database == nil {
		return ErrDatabaseMissing
	}

	record := database.AgentRunStep{
		ID:            newRuntimeID("step"),
		RunID:         step.RunID,
		StepOrder:     step.StepOrder,
		ToolID:        step.ToolID,
		InputSummary:  database.JSON(step.InputSummary),
		OutputSummary: database.JSON(step.OutputSummary),
		DurationMS:    step.DurationMS,
		Status:        string(step.Status),
		ErrorSummary:  RedactText(step.ErrorSummary),
		CreatedAt:     time.Now().UTC(),
	}
	if err := generated.AgentRunStepQueries[database.AgentRunStep](repository.database).Create(ctx, &record); err != nil {
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

func (repository Repository) runInterruptions(ctx context.Context, runID string) ([]Interruption, *Interruption, error) {
	records, err := generated.AgentInterruptionQueries[database.AgentInterruption](repository.database).ListByRunID(ctx, runID)
	if err != nil {
		return nil, nil, fmt.Errorf("list run interruptions: %w", err)
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

func (repository Repository) runObservations(ctx context.Context, runID string) ([]Observation, error) {
	records, err := generated.AgentRunObservationQueries[database.AgentRunObservation](repository.database).ListByRunID(ctx, runID)
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

func runFromRecord(record database.AgentRun) Run {
	run := Run{
		ID:               record.ID,
		SessionID:        record.SessionID,
		TriggerMessageID: record.TriggerMessageID,
		Status:           RunStatus(record.Status),
		ErrorSummary:     record.ErrorSummary,
		StartedAt:        record.StartedAt,
	}
	if record.FinishedAt.Valid {
		run.FinishedAt = record.FinishedAt.Time
	}
	return run
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
		ID:        record.ID,
		SessionID: record.SessionID,
		RunID:     record.RunID,
		Role:      ChatMessageRole(record.Role),
		Content:   record.Content,
		Status:    ChatMessageStatus(record.Status),
		Sequence:  record.Sequence,
		CreatedAt: record.CreatedAt,
		Error:     record.ErrorSummary,
	}
	if record.CompletedAt.Valid {
		message.CompletedAt = record.CompletedAt.Time
	}
	return message
}

func interruptionFromRecord(record database.AgentInterruption) Interruption {
	interruption := Interruption{
		ID:        record.ID,
		SessionID: record.SessionID,
		RunID:     record.RunID,
		Type:      InterruptionType(record.Type),
		Status:    InterruptionStatus(record.Status),
		Message:   record.Message,
		Payload:   json.RawMessage(record.Payload),
		CreatedAt: record.CreatedAt,
	}
	if record.ResolvedAt.Valid {
		interruption.RespondedAt = record.ResolvedAt.Time
	}
	return interruption
}

func newRuntimeID(prefix string) string {
	return fmt.Sprintf("%s_%d", prefix, time.Now().UTC().UnixNano())
}
