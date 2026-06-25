package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"gorm.io/gorm"

	"ai/backend/internal/database"
	"ai/backend/internal/database/generated"
	"ai/backend/internal/platform/sqlite"
)

const testRunAnswer = "done"
const testToolID = "tool_1"

func TestRepositoryPersistsRunAndStep(t *testing.T) {
	repository := NewRepository(newTestDatabase(t))

	run, err := repository.StartAgentExecution(context.Background(), CreateAgentExecutionRecord{})
	if err != nil {
		t.Fatalf("StartAgentExecution() error = %v, want nil", err)
	}

	err = repository.SaveStep(context.Background(), StepRecord{
		ExecutionID:   run.ID,
		StepOrder:     1,
		ToolID:        testToolID,
		InputSummary:  []byte(`{}`),
		OutputSummary: []byte(`{"ok":true}`),
		DurationMS:    10,
		Status:        StepStatusSucceeded,
	})
	if err != nil {
		t.Fatalf("SaveStep() error = %v, want nil", err)
	}
	steps, err := generated.AgentExecutionStepQueries[database.AgentExecutionStep](repository.database).ListByExecutionID(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("load saved steps error = %v", err)
	}
	if len(steps) != 1 {
		t.Fatalf("len(steps) = %d, want 1", len(steps))
	}
	if steps[0].ToolID != testToolID {
		t.Fatalf("saved step ToolID = %q, want %s", steps[0].ToolID, testToolID)
	}

	run.Status = AgentExecutionStatusSucceeded
	if err := repository.FinishAgentExecution(context.Background(), run); err != nil {
		t.Fatalf("FinishAgentExecution() error = %v, want nil", err)
	}
}

//nolint:cyclop // This integration test verifies ordered chat persistence, attachments, and run linkage in one transaction path.
func TestRepositoryPersistsChatSessionMessagesAttachmentsAndRunLink(t *testing.T) {
	repository := NewRepository(newTestDatabase(t))

	session, err := repository.CreateChatSession(context.Background(), CreateChatSessionRecord{Title: "Reports"})
	if err != nil {
		t.Fatalf("CreateChatSession() error = %v, want nil", err)
	}
	userMessage, err := repository.CreateChatMessage(context.Background(), CreateChatMessageRecord{
		SessionID: session.ID,
		Role:      ChatMessageRoleUser,
		Content:   "帮我导出报表",
		Attachments: []Attachment{{
			ID:       "att_test",
			Filename: "merchant_catalog.pdf",
			MIMEType: "application/pdf",
			Kind:     AttachmentKindPDF,
			Size:     123,
			Data:     "raw-data-must-not-persist",
		}},
	})
	if err != nil {
		t.Fatalf("CreateChatMessage(user) error = %v, want nil", err)
	}
	run, err := repository.StartAgentExecution(context.Background(), CreateAgentExecutionRecord{
		SessionID:        session.ID,
		TriggerMessageID: userMessage.ID,
	})
	if err != nil {
		t.Fatalf("StartAgentExecution() error = %v, want nil", err)
	}
	assistantMessage, err := repository.CreateChatMessage(context.Background(), CreateChatMessageRecord{
		SessionID:   session.ID,
		ExecutionID: run.ID,
		Role:        ChatMessageRoleAssistant,
		Content:     testRunAnswer,
	})
	if err != nil {
		t.Fatalf("CreateChatMessage(assistant) error = %v, want nil", err)
	}

	messages, err := repository.ListChatMessages(context.Background(), session.ID)
	if err != nil {
		t.Fatalf("ListChatMessages() error = %v, want nil", err)
	}
	if len(messages) != 2 {
		t.Fatalf("len(messages) = %d, want 2", len(messages))
	}
	if messages[0].ID != userMessage.ID || messages[0].Sequence != 1 || len(messages[0].Attachments) != 1 {
		t.Fatalf("first message = %#v, want user sequence 1 with attachment", messages[0])
	}
	if messages[1].ID != assistantMessage.ID || messages[1].ExecutionID != run.ID || messages[1].Sequence != 2 {
		t.Fatalf("second message = %#v, want assistant linked to run sequence 2", messages[1])
	}
}

func TestRepositoryRedactsChatMessageBeforePersisting(t *testing.T) {
	repository := NewRepository(newTestDatabase(t))
	session, err := repository.CreateChatSession(context.Background(), CreateChatSessionRecord{})
	if err != nil {
		t.Fatalf("CreateChatSession() error = %v", err)
	}
	message := "export report with Authorization: Bearer " + testSecretToken

	saved, err := repository.CreateChatMessage(context.Background(), CreateChatMessageRecord{
		SessionID: session.ID,
		Role:      ChatMessageRoleUser,
		Content:   message,
	})
	if err != nil {
		t.Fatalf("CreateChatMessage() error = %v", err)
	}

	if strings.Contains(saved.Content, testSecretToken) {
		t.Fatalf("saved.Content = %q, want redacted token", saved.Content)
	}
	if !strings.Contains(saved.Content, redactedValue) {
		t.Fatalf("saved.Content = %q, want redacted marker", saved.Content)
	}
}

func TestRepositoryReturnsNotFoundForMissingChatSessionAndRun(t *testing.T) {
	repository := NewRepository(newTestDatabase(t))

	if _, err := repository.GetChatSession(context.Background(), "chat_missing"); !errors.Is(err, ErrChatSessionNotFound) {
		t.Fatalf("GetChatSession() error = %v, want ErrChatSessionNotFound", err)
	}
	if _, err := repository.GetAgentExecutionState(context.Background(), "exec_missing"); !errors.Is(err, ErrAgentExecutionNotFound) {
		t.Fatalf("GetAgentExecutionState() error = %v, want ErrAgentExecutionNotFound", err)
	}
	if _, err := repository.ActiveAgentExecution(context.Background(), "chat_missing"); !errors.Is(err, ErrAgentExecutionNotFound) {
		t.Fatalf("ActiveAgentExecution() error = %v, want ErrAgentExecutionNotFound", err)
	}
}

func TestRepositoryFindsOnlyActiveAgentExecution(t *testing.T) {
	repository := NewRepository(newTestDatabase(t))
	session, err := repository.CreateChatSession(context.Background(), CreateChatSessionRecord{})
	if err != nil {
		t.Fatalf("CreateChatSession() error = %v", err)
	}
	execution, err := repository.StartAgentExecution(context.Background(), CreateAgentExecutionRecord{SessionID: session.ID})
	if err != nil {
		t.Fatalf("StartAgentExecution() error = %v", err)
	}

	active, err := repository.ActiveAgentExecution(context.Background(), session.ID)
	if err != nil {
		t.Fatalf("ActiveAgentExecution() error = %v", err)
	}
	if active.ID != execution.ID {
		t.Fatalf("active.ID = %q, want %q", active.ID, execution.ID)
	}

	execution.Status = AgentExecutionStatusSucceeded
	if err := repository.FinishAgentExecution(context.Background(), execution); err != nil {
		t.Fatalf("FinishAgentExecution() error = %v", err)
	}
	if _, err := repository.ActiveAgentExecution(context.Background(), session.ID); !errors.Is(err, ErrAgentExecutionNotFound) {
		t.Fatalf("ActiveAgentExecution() after finish error = %v, want ErrAgentExecutionNotFound", err)
	}
}

func TestRepositoryStartAgentExecutionRejectsActiveExecution(t *testing.T) {
	repository := NewRepository(newTestDatabase(t))
	session, err := repository.CreateChatSession(context.Background(), CreateChatSessionRecord{})
	if err != nil {
		t.Fatalf("CreateChatSession() error = %v", err)
	}
	if _, err := repository.StartAgentExecution(context.Background(), CreateAgentExecutionRecord{SessionID: session.ID}); err != nil {
		t.Fatalf("StartAgentExecution(first) error = %v", err)
	}

	_, err = repository.StartAgentExecution(context.Background(), CreateAgentExecutionRecord{SessionID: session.ID})

	if !errors.Is(err, ErrAgentExecutionActive) {
		t.Fatalf("StartAgentExecution(second) error = %v, want ErrAgentExecutionActive", err)
	}
}

//nolint:cyclop,funlen,gocognit // This integration test verifies interruption persistence, lookup, resolution, and run state together.
func TestRepositoryPersistsAndResolvesInterruption(t *testing.T) {
	repository := NewRepository(newTestDatabase(t))
	session, err := repository.CreateChatSession(context.Background(), CreateChatSessionRecord{})
	if err != nil {
		t.Fatalf("CreateChatSession() error = %v", err)
	}
	userMessage, err := repository.CreateChatMessage(context.Background(), CreateChatMessageRecord{
		SessionID: session.ID,
		Role:      ChatMessageRoleUser,
		Content:   "delete duplicate account",
	})
	if err != nil {
		t.Fatalf("CreateChatMessage(user) error = %v", err)
	}
	run, err := repository.StartAgentExecution(context.Background(), CreateAgentExecutionRecord{
		SessionID:        session.ID,
		TriggerMessageID: userMessage.ID,
	})
	if err != nil {
		t.Fatalf("StartAgentExecution() error = %v", err)
	}
	interruption := Interruption{
		SessionID:   session.ID,
		ExecutionID: run.ID,
		Type:        InterruptionTypeApproval,
		Status:      InterruptionStatusAwaitingReview,
		Message:     "Delete the duplicate account?",
		Payload:     json.RawMessage(`{"risk":"destructive"}`),
	}

	saved, err := repository.CreateInterruption(context.Background(), interruption)
	if err != nil {
		t.Fatalf("CreateInterruption() error = %v", err)
	}
	run.Status = AgentExecutionStatusInterrupted
	if err := repository.MarkAgentExecutionInterrupted(context.Background(), run, saved); err != nil {
		t.Fatalf("MarkAgentExecutionInterrupted() error = %v", err)
	}

	loaded, err := repository.GetAgentExecutionState(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("GetAgentExecutionState() error = %v", err)
	}
	if loaded.AgentExecution.Status != AgentExecutionStatusInterrupted {
		t.Fatalf("Status = %q, want interrupted", loaded.AgentExecution.Status)
	}
	if loaded.ActiveInterruption == nil || loaded.ActiveInterruption.Message != "Delete the duplicate account?" {
		t.Fatalf("ActiveInterruption = %#v, want active interruption", loaded.ActiveInterruption)
	}
	active, err := repository.ActiveInterruption(context.Background(), session.ID)
	if err != nil {
		t.Fatalf("ActiveInterruption() error = %v", err)
	}
	if active.ID != saved.ID {
		t.Fatalf("active.ID = %q, want %q", active.ID, saved.ID)
	}

	response, err := repository.CreateChatMessage(context.Background(), CreateChatMessageRecord{
		SessionID: session.ID,
		Role:      ChatMessageRoleUser,
		Content:   "continue",
	})
	if err != nil {
		t.Fatalf("CreateChatMessage(response) error = %v", err)
	}
	if err := repository.ResolveInterruption(context.Background(), saved.ID, response.ID, InterruptionStatusResolved); err != nil {
		t.Fatalf("ResolveInterruption() error = %v", err)
	}
	if _, err := repository.ActiveInterruption(context.Background(), session.ID); !errors.Is(err, ErrNoActiveInterruption) {
		t.Fatalf("ActiveInterruption() error = %v, want ErrNoActiveInterruption", err)
	}
	if err := repository.ResolveInterruption(context.Background(), saved.ID, response.ID, InterruptionStatusResolved); !errors.Is(err, ErrAgentExecutionNotWaiting) {
		t.Fatalf("ResolveInterruption() second error = %v, want ErrAgentExecutionNotWaiting", err)
	}
}

func newTestDatabase(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := sqlite.Connect(context.Background(), "file::memory:?cache=shared")
	if err != nil {
		t.Fatalf("connect sqlite: %v", err)
	}
	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			if closeErr := sqlDB.Close(); closeErr != nil {
				t.Fatal(fmt.Errorf("close sqlite: %w", closeErr))
			}
		}
	})
	return db
}
