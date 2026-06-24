package agent

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"gorm.io/gorm"

	"ai/backend/internal/database"
	"ai/backend/internal/platform/sqlite"
)

const testRunAnswer = "done"
const testToolID = "tool_1"

func TestRepositoryPersistsRunAndStep(t *testing.T) {
	repository := NewRepository(newTestDatabase(t))

	run, err := repository.StartRun(context.Background(), CreateRunRecord{Message: "build a report"})
	if err != nil {
		t.Fatalf("StartRun() error = %v, want nil", err)
	}

	err = repository.SaveStep(context.Background(), StepRecord{
		RunID:         run.ID,
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
	var step database.AgentRunStep
	if err := repository.database.WithContext(context.Background()).Where("run_id = ?", run.ID).First(&step).Error; err != nil {
		t.Fatalf("load saved step error = %v", err)
	}
	if step.ToolID != testToolID {
		t.Fatalf("saved step ToolID = %q, want %s", step.ToolID, testToolID)
	}

	run.Status = RunStatusSucceeded
	run.Answer = testRunAnswer
	if err := repository.FinishRun(context.Background(), run); err != nil {
		t.Fatalf("FinishRun() error = %v, want nil", err)
	}
}

func TestRepositoryRedactsMessageBeforePersistingRun(t *testing.T) {
	repository := NewRepository(newTestDatabase(t))
	message := "export report with Authorization: Bearer " + testSecretToken

	run, err := repository.StartRun(context.Background(), CreateRunRecord{Message: message})
	if err != nil {
		t.Fatalf("StartRun() error = %v, want nil", err)
	}

	var record database.AgentRun
	if err := repository.database.WithContext(context.Background()).Where("id = ?", run.ID).First(&record).Error; err != nil {
		t.Fatalf("load saved run error = %v", err)
	}
	if strings.Contains(record.Message, testSecretToken) {
		t.Fatalf("record.Message = %q, want redacted token", record.Message)
	}
	if !strings.Contains(record.Message, redactedValue) {
		t.Fatalf("record.Message = %q, want redacted marker", record.Message)
	}
}

func TestRepositoryPersistsRunAttachments(t *testing.T) {
	repository := NewRepository(newTestDatabase(t))

	run, err := repository.StartRun(context.Background(), CreateRunRecord{
		Message: "update catalog",
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
		t.Fatalf("StartRun() error = %v, want nil", err)
	}

	var records []database.AgentRunAttachment
	if err := repository.database.WithContext(context.Background()).Where("run_id = ?", run.ID).Find(&records).Error; err != nil {
		t.Fatalf("load attachments error = %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("len(records) = %d, want 1", len(records))
	}
	if records[0].Filename != "merchant_catalog.pdf" {
		t.Fatalf("Filename = %q, want merchant_catalog.pdf", records[0].Filename)
	}
	if records[0].ProviderFileID != "" {
		t.Fatalf("ProviderFileID = %q, want empty", records[0].ProviderFileID)
	}
}

func TestRepositoryRollsBackRunWhenAttachmentPersistenceFails(t *testing.T) {
	repository := NewRepository(newTestDatabase(t))

	_, err := repository.StartRun(context.Background(), CreateRunRecord{
		Message: "update catalog",
		Attachments: []Attachment{
			{
				ID:       "att_duplicate",
				Filename: "merchant_catalog.pdf",
				MIMEType: "application/pdf",
				Kind:     AttachmentKindPDF,
				Size:     123,
			},
			{
				ID:       "att_duplicate",
				Filename: "merchant_catalog_2.pdf",
				MIMEType: "application/pdf",
				Kind:     AttachmentKindPDF,
				Size:     456,
			},
		},
	})
	if err == nil {
		t.Fatal("StartRun() error = nil, want duplicate attachment error")
	}

	var runCount int64
	if err := repository.database.WithContext(context.Background()).Model(&database.AgentRun{}).Count(&runCount).Error; err != nil {
		t.Fatalf("count runs error = %v", err)
	}
	if runCount != 0 {
		t.Fatalf("run count = %d, want 0", runCount)
	}

	var attachmentCount int64
	if err := repository.database.WithContext(context.Background()).Model(&database.AgentRunAttachment{}).Count(&attachmentCount).Error; err != nil {
		t.Fatalf("count attachments error = %v", err)
	}
	if attachmentCount != 0 {
		t.Fatalf("attachment count = %d, want 0", attachmentCount)
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
