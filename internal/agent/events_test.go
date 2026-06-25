package agent

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestEventBusPublishesToChatSubscribersOnly(t *testing.T) {
	bus := NewEventBusWithBuffer(2)
	chatEvents, unsubscribeChat := bus.Subscribe("chat_1")
	defer unsubscribeChat()
	otherEvents, unsubscribeOther := bus.Subscribe("chat_2")
	defer unsubscribeOther()

	bus.Publish("chat_1", ChatEvent{Type: EventTypeExecutionStarted, ChatID: "chat_1", ExecutionID: "exec_1"})

	select {
	case event := <-chatEvents:
		if event.Type != EventTypeExecutionStarted || event.ChatID != "chat_1" || event.ExecutionID != "exec_1" {
			t.Fatalf("event = %#v, want execution.started for chat_1", event)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timed out waiting for chat_1 event")
	}

	select {
	case event := <-otherEvents:
		t.Fatalf("chat_2 received event %#v, want none", event)
	case <-time.After(25 * time.Millisecond):
	}
}

func TestEventBusClosesSlowSubscriber(t *testing.T) {
	bus := NewEventBusWithBuffer(1)
	events, unsubscribe := bus.Subscribe("chat_1")
	defer unsubscribe()

	bus.Publish("chat_1", ChatEvent{Type: EventTypeExecutionStarted, ChatID: "chat_1", ExecutionID: "exec_1"})
	bus.Publish("chat_1", ChatEvent{Type: EventTypeExecutionCompleted, ChatID: "chat_1", ExecutionID: "exec_1"})

	first, ok := <-events
	if !ok {
		t.Fatal("events channel closed before first event")
	}
	if first.Type != EventTypeExecutionStarted {
		t.Fatalf("first event type = %q, want %q", first.Type, EventTypeExecutionStarted)
	}

	_, ok = <-events
	if ok {
		t.Fatal("events channel still open after buffer overflow")
	}
}

func TestEventBusUnsubscribeStopsDelivery(t *testing.T) {
	bus := NewEventBusWithBuffer(2)
	events, unsubscribe := bus.Subscribe("chat_1")
	unsubscribe()

	bus.Publish("chat_1", ChatEvent{Type: EventTypeExecutionStarted, ChatID: "chat_1", ExecutionID: "exec_1"})

	_, ok := <-events
	if ok {
		t.Fatal("events channel open after unsubscribe")
	}
}

func TestChatEventJSONUsesExecutionID(t *testing.T) {
	body, err := json.Marshal(ChatEvent{
		Type:           EventTypeExecutionStarted,
		ChatID:         "chat_1",
		ExecutionID:    "exec_1",
		AgentExecution: &AgentExecutionResponse{ExecutionID: "exec_1", Status: AgentExecutionStatusRunning},
	})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if strings.Contains(string(body), "run_id") || !strings.Contains(string(body), "execution_id") {
		t.Fatalf("event JSON = %s, want execution_id and no run_id", body)
	}
	if strings.Contains(string(body), `"run":`) || !strings.Contains(string(body), `"execution":`) {
		t.Fatalf("event JSON = %s, want execution payload and no run payload", body)
	}
}
