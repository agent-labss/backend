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

func TestAgentRunExcludesLegacySummaryColumns(t *testing.T) {
	runType := reflect.TypeOf(AgentRun{})
	for _, name := range []string{"Message", "AnswerSummary", "OutputSummary"} {
		if _, ok := runType.FieldByName(name); ok {
			t.Fatalf("AgentRun contains legacy field %s", name)
		}
	}
}

func TestAgentInterruptionExcludesDeadRequestMessageColumn(t *testing.T) {
	interruptionType := reflect.TypeOf(AgentInterruption{})
	if _, ok := interruptionType.FieldByName("RequestMessageID"); ok {
		t.Fatal("AgentInterruption contains dead RequestMessageID field")
	}
}
