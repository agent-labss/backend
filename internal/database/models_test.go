package database

import (
	"reflect"
	"testing"
)

func TestModelsExcludeLegacyRunFirstTables(t *testing.T) {
	for _, model := range Models() {
		name := reflect.TypeOf(model).Elem().Name()
		switch name {
		case "AgentRunAttachment", "AgentRunInteraction", "AgentRunTurn", "AgentRunTurnAttachment":
			t.Fatalf("Models() includes legacy run-first model %s", name)
		}
	}
}

func TestAgentExecutionExcludesLegacySummaryColumns(t *testing.T) {
	executionType := reflect.TypeOf(AgentExecution{})
	for _, name := range []string{"Message", "AnswerSummary", "OutputSummary"} {
		if _, ok := executionType.FieldByName(name); ok {
			t.Fatalf("AgentExecution contains legacy field %s", name)
		}
	}
}

func TestModelsUseAgentExecutionNames(t *testing.T) {
	modelNames := make(map[string]reflect.Type)
	for _, model := range Models() {
		modelType := reflect.TypeOf(model).Elem()
		modelNames[modelType.Name()] = modelType
	}
	for _, oldName := range []string{"AgentRun", "AgentRunObservation", "AgentRunStep"} {
		if _, ok := modelNames[oldName]; ok {
			t.Fatalf("Models() includes old model name %s, want AgentExecution names", oldName)
		}
	}
	for _, newName := range []string{"AgentExecution", "AgentExecutionObservation", "AgentExecutionStep"} {
		if _, ok := modelNames[newName]; !ok {
			t.Fatalf("Models() missing %s; models = %#v", newName, modelNames)
		}
	}
}

func TestDatabaseModelsUseExecutionIDForeignKeys(t *testing.T) {
	modelTypes := map[string]reflect.Type{}
	for _, model := range Models() {
		modelType := reflect.TypeOf(model).Elem()
		modelTypes[modelType.Name()] = modelType
	}
	for _, modelName := range []string{"ChatMessage", "AgentInterruption", "AgentExecutionObservation", "AgentExecutionStep"} {
		modelType, ok := modelTypes[modelName]
		if !ok {
			t.Fatalf("Models() missing %s", modelName)
		}
		if _, ok := modelType.FieldByName("RunID"); ok {
			t.Fatalf("%s contains RunID, want ExecutionID", modelType.Name())
		}
		if _, ok := modelType.FieldByName("ExecutionID"); !ok {
			t.Fatalf("%s is missing ExecutionID", modelType.Name())
		}
	}
}

func TestAgentInterruptionExcludesDeadRequestMessageColumn(t *testing.T) {
	interruptionType := reflect.TypeOf(AgentInterruption{})
	if _, ok := interruptionType.FieldByName("RequestMessageID"); ok {
		t.Fatal("AgentInterruption contains dead RequestMessageID field")
	}
}
