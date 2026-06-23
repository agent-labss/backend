package agent

import (
	"context"
	"fmt"
	"testing"

	"gorm.io/gorm"

	"ai/backend/internal/database"
	"ai/backend/internal/platform/sqlite"
)

const testRunAnswer = "done"
const testToolID = "tool_1"

func TestRepositoryPersistsRunAndStep(t *testing.T) {
	repository := NewRepository(newTestDatabase(t))

	run, err := repository.StartRun(context.Background(), "build a report")
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
