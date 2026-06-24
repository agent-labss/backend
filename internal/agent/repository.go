package agent

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"gorm.io/cli/gorm/typed"
	"gorm.io/gorm"

	"ai/backend/internal/database"
)

var (
	ErrDatabaseMissing = errors.New("agent database is missing")
	ErrRunNotFound     = errors.New("agent run not found")
	ErrRunNotWaiting   = errors.New("agent run is not waiting for user")
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
		ID:        newRuntimeID("run"),
		Message:   RedactText(record.Message),
		Status:    RunStatusRunning,
		StartedAt: time.Now().UTC(),
	}
	runRecord := database.AgentRun{
		ID:            run.ID,
		Message:       run.Message,
		Status:        string(run.Status),
		AnswerSummary: "",
		OutputSummary: database.JSON([]byte(`{}`)),
		ErrorSummary:  "",
		StartedAt:     run.StartedAt,
	}
	if err := repository.database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := typed.G[database.AgentRun](tx).Create(ctx, &runRecord); err != nil {
			return fmt.Errorf("start run: %w", err)
		}
		return saveAttachments(ctx, tx, run.ID, record.Attachments)
	}); err != nil {
		return Run{}, fmt.Errorf("start run transaction: %w", err)
	}

	return run, nil
}

func saveAttachments(ctx context.Context, db *gorm.DB, runID string, attachments []Attachment) error {
	for _, attachment := range attachments {
		record := database.AgentRunAttachment{
			ID:             attachment.ID,
			RunID:          runID,
			Filename:       RedactText(attachment.Filename),
			MIMEType:       attachment.MIMEType,
			Kind:           string(attachment.Kind),
			SizeBytes:      attachment.Size,
			ProviderFileID: attachment.FileID,
			CreatedAt:      time.Now().UTC(),
		}
		if err := typed.G[database.AgentRunAttachment](db).Create(ctx, &record); err != nil {
			return fmt.Errorf("save run attachment: %w", err)
		}
	}

	return nil
}

func (repository Repository) FinishRun(ctx context.Context, run Run) error {
	if repository.database == nil {
		return ErrDatabaseMissing
	}

	outputSummary, err := json.Marshal(RedactJSONValue(run.Outputs))
	if err != nil {
		return fmt.Errorf("marshal output summary: %w", err)
	}

	if err := typed.G[database.AgentRun](repository.database).Exec(ctx, `
UPDATE agent_runs
SET status = ?, answer_summary = ?, output_summary = ?, error_summary = ?, finished_at = ?
WHERE id = ?
`, string(run.Status), RedactText(run.Answer), database.JSON(outputSummary), RedactText(run.ErrorSummary), sql.NullTime{Time: time.Now().UTC(), Valid: true}, run.ID); err != nil {
		return fmt.Errorf("finish run: %w", err)
	}

	return nil
}

func (repository Repository) GetRun(ctx context.Context, runID string) (RunResponse, error) {
	state, err := repository.GetRunState(ctx, runID)
	if err != nil {
		return RunResponse{}, err
	}

	response := RunResponse{
		RunID:       state.Run.ID,
		Status:      state.Run.Status,
		Answer:      state.Run.Answer,
		Outputs:     state.Run.Outputs,
		Error:       state.Run.ErrorSummary,
		Interaction: state.Pending,
	}
	return response, nil
}

func (repository Repository) GetRunState(ctx context.Context, runID string) (RunStateRecord, error) {
	if repository.database == nil {
		return RunStateRecord{}, ErrDatabaseMissing
	}

	var runRecord database.AgentRun
	if err := repository.database.WithContext(ctx).Where("id = ?", runID).First(&runRecord).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return RunStateRecord{}, ErrRunNotFound
		}
		return RunStateRecord{}, fmt.Errorf("get run: %w", err)
	}

	state := RunStateRecord{Run: runFromRecord(runRecord)}
	attachments, err := repository.runAttachments(ctx, runID)
	if err != nil {
		return RunStateRecord{}, err
	}
	state.Attachments = attachments

	interactions, pending, err := repository.runInteractions(ctx, runID)
	if err != nil {
		return RunStateRecord{}, err
	}
	state.Interactions = interactions
	state.Pending = pending

	turns, err := repository.runTurns(ctx, runID)
	if err != nil {
		return RunStateRecord{}, err
	}
	state.Turns = turns

	observations, err := repository.runObservations(ctx, runID)
	if err != nil {
		return RunStateRecord{}, err
	}
	state.Observations = observations

	return state, nil
}

func (repository Repository) CreateInteraction(ctx context.Context, interaction Interaction) (Interaction, error) {
	if repository.database == nil {
		return Interaction{}, ErrDatabaseMissing
	}

	now := time.Now().UTC()
	if interaction.ID == "" {
		interaction.ID = newRuntimeID("int")
	}
	if interaction.Type == "" {
		interaction.Type = InteractionTypeUserInput
	}
	if interaction.Status == "" {
		interaction.Status = InteractionStatusPending
	}
	interaction.Message = RedactText(interaction.Message)
	interaction.CreatedAt = now
	payload := interaction.Payload
	if len(payload) == 0 {
		payload = json.RawMessage(`{}`)
	}

	record := database.AgentRunInteraction{
		ID:        interaction.ID,
		RunID:     interaction.RunID,
		Type:      string(interaction.Type),
		Status:    string(interaction.Status),
		Message:   interaction.Message,
		Payload:   database.JSON(payload),
		CreatedAt: now,
	}
	if err := typed.G[database.AgentRunInteraction](repository.database).Create(ctx, &record); err != nil {
		return Interaction{}, fmt.Errorf("create interaction: %w", err)
	}

	return interaction, nil
}

func (repository Repository) MarkRunWaiting(ctx context.Context, run Run, _ Interaction) error {
	if repository.database == nil {
		return ErrDatabaseMissing
	}

	if err := typed.G[database.AgentRun](repository.database).Exec(ctx, `
UPDATE agent_runs
SET status = ?, error_summary = '', finished_at = NULL
WHERE id = ?
`, string(RunStatusWaitingForUser), run.ID); err != nil {
		return fmt.Errorf("mark run waiting: %w", err)
	}

	return nil
}

func (repository Repository) CreateRunTurn(ctx context.Context, record CreateRunTurnRecord) (RunTurn, error) {
	if repository.database == nil {
		return RunTurn{}, ErrDatabaseMissing
	}

	now := time.Now().UTC()
	turn := RunTurn{
		ID:          newRuntimeID("turn"),
		RunID:       record.RunID,
		Message:     RedactText(record.Message),
		Attachments: record.Attachments,
		CreatedAt:   now,
	}
	turnRecord := database.AgentRunTurn{
		ID:        turn.ID,
		RunID:     turn.RunID,
		Message:   turn.Message,
		CreatedAt: now,
	}
	if err := repository.database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := typed.G[database.AgentRunTurn](tx).Create(ctx, &turnRecord); err != nil {
			return fmt.Errorf("create run turn: %w", err)
		}
		return saveTurnAttachments(ctx, tx, turn, record.Attachments)
	}); err != nil {
		return RunTurn{}, fmt.Errorf("create run turn transaction: %w", err)
	}

	return turn, nil
}

func saveTurnAttachments(ctx context.Context, db *gorm.DB, turn RunTurn, attachments []Attachment) error {
	for _, attachment := range attachments {
		record := database.AgentRunTurnAttachment{
			ID:             attachment.ID,
			TurnID:         turn.ID,
			RunID:          turn.RunID,
			Filename:       RedactText(attachment.Filename),
			MIMEType:       attachment.MIMEType,
			Kind:           string(attachment.Kind),
			SizeBytes:      attachment.Size,
			ProviderFileID: attachment.FileID,
			CreatedAt:      time.Now().UTC(),
		}
		if err := typed.G[database.AgentRunTurnAttachment](db).Create(ctx, &record); err != nil {
			return fmt.Errorf("save turn attachment: %w", err)
		}
	}

	return nil
}

func (repository Repository) MarkInteractionResponded(ctx context.Context, interactionID string, turnID string) error {
	if repository.database == nil {
		return ErrDatabaseMissing
	}

	if err := typed.G[database.AgentRunInteraction](repository.database).Exec(ctx, `
UPDATE agent_run_interactions
SET status = ?, response_turn_id = ?, responded_at = ?
WHERE id = ? AND status = ?
`, string(InteractionStatusResponded), turnID, sql.NullTime{Time: time.Now().UTC(), Valid: true}, interactionID, string(InteractionStatusPending)); err != nil {
		return fmt.Errorf("mark interaction responded: %w", err)
	}

	return nil
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
	if err := typed.G[database.AgentRunObservation](repository.database).Create(ctx, &observationRecord); err != nil {
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
	if err := typed.G[database.AgentRunStep](repository.database).Create(ctx, &record); err != nil {
		return fmt.Errorf("save step: %w", err)
	}

	return nil
}

func (repository Repository) runAttachments(ctx context.Context, runID string) ([]Attachment, error) {
	var records []database.AgentRunAttachment
	if err := repository.database.WithContext(ctx).Where("run_id = ?", runID).Order("created_at ASC").Find(&records).Error; err != nil {
		return nil, fmt.Errorf("list run attachments: %w", err)
	}
	attachments := make([]Attachment, 0, len(records))
	for _, record := range records {
		attachments = append(attachments, Attachment{
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

func (repository Repository) runInteractions(ctx context.Context, runID string) ([]Interaction, *Interaction, error) {
	var records []database.AgentRunInteraction
	if err := repository.database.WithContext(ctx).Where("run_id = ?", runID).Order("created_at ASC").Find(&records).Error; err != nil {
		return nil, nil, fmt.Errorf("list run interactions: %w", err)
	}
	interactions := make([]Interaction, 0, len(records))
	var pending *Interaction
	for _, record := range records {
		interaction := interactionFromRecord(record)
		interactions = append(interactions, interaction)
		if interaction.Status == InteractionStatusPending {
			pendingCopy := interaction
			pending = &pendingCopy
		}
	}
	return interactions, pending, nil
}

func (repository Repository) runTurns(ctx context.Context, runID string) ([]RunTurn, error) {
	var turnRecords []database.AgentRunTurn
	if err := repository.database.WithContext(ctx).Where("run_id = ?", runID).Order("created_at ASC").Find(&turnRecords).Error; err != nil {
		return nil, fmt.Errorf("list run turns: %w", err)
	}
	attachments, err := repository.turnAttachments(ctx, runID)
	if err != nil {
		return nil, err
	}
	turns := make([]RunTurn, 0, len(turnRecords))
	for _, record := range turnRecords {
		turns = append(turns, RunTurn{
			ID:          record.ID,
			RunID:       record.RunID,
			Message:     record.Message,
			Attachments: attachments[record.ID],
			CreatedAt:   record.CreatedAt,
		})
	}
	return turns, nil
}

func (repository Repository) turnAttachments(ctx context.Context, runID string) (map[string][]Attachment, error) {
	var records []database.AgentRunTurnAttachment
	if err := repository.database.WithContext(ctx).Where("run_id = ?", runID).Order("created_at ASC").Find(&records).Error; err != nil {
		return nil, fmt.Errorf("list turn attachments: %w", err)
	}
	attachments := make(map[string][]Attachment)
	for _, record := range records {
		attachments[record.TurnID] = append(attachments[record.TurnID], Attachment{
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

func (repository Repository) runObservations(ctx context.Context, runID string) ([]Observation, error) {
	var records []database.AgentRunObservation
	if err := repository.database.WithContext(ctx).Where("run_id = ?", runID).Order("step_order ASC, created_at ASC").Find(&records).Error; err != nil {
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
	outputs := make(map[string]any)
	if len(record.OutputSummary) > 0 {
		if err := json.Unmarshal(record.OutputSummary, &outputs); err != nil {
			outputs = map[string]any{}
		}
	}
	run := Run{
		ID:           record.ID,
		Message:      record.Message,
		Status:       RunStatus(record.Status),
		Answer:       record.AnswerSummary,
		Outputs:      outputs,
		ErrorSummary: record.ErrorSummary,
		StartedAt:    record.StartedAt,
	}
	if record.FinishedAt.Valid {
		run.FinishedAt = record.FinishedAt.Time
	}
	return run
}

func interactionFromRecord(record database.AgentRunInteraction) Interaction {
	interaction := Interaction{
		ID:        record.ID,
		RunID:     record.RunID,
		Type:      InteractionType(record.Type),
		Status:    InteractionStatus(record.Status),
		Message:   record.Message,
		Payload:   json.RawMessage(record.Payload),
		CreatedAt: record.CreatedAt,
	}
	if record.RespondedAt.Valid {
		interaction.RespondedAt = record.RespondedAt.Time
	}
	return interaction
}

func newRuntimeID(prefix string) string {
	return fmt.Sprintf("%s_%d", prefix, time.Now().UTC().UnixNano())
}
