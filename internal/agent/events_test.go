package agent

import (
	"testing"
	"time"
)

func TestEventBusPublishesToChatSubscribersOnly(t *testing.T) {
	bus := NewEventBusWithBuffer(2)
	chatEvents, unsubscribeChat := bus.Subscribe("chat_1")
	defer unsubscribeChat()
	otherEvents, unsubscribeOther := bus.Subscribe("chat_2")
	defer unsubscribeOther()

	bus.Publish("chat_1", ChatEvent{Type: EventTypeRunStarted, ChatID: "chat_1", RunID: "run_1"})

	select {
	case event := <-chatEvents:
		if event.Type != EventTypeRunStarted || event.ChatID != "chat_1" || event.RunID != "run_1" {
			t.Fatalf("event = %#v, want run.started for chat_1", event)
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

	bus.Publish("chat_1", ChatEvent{Type: EventTypeRunStarted, ChatID: "chat_1", RunID: "run_1"})
	bus.Publish("chat_1", ChatEvent{Type: EventTypeRunCompleted, ChatID: "chat_1", RunID: "run_1"})

	first, ok := <-events
	if !ok {
		t.Fatal("events channel closed before first event")
	}
	if first.Type != EventTypeRunStarted {
		t.Fatalf("first event type = %q, want %q", first.Type, EventTypeRunStarted)
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

	bus.Publish("chat_1", ChatEvent{Type: EventTypeRunStarted, ChatID: "chat_1", RunID: "run_1"})

	_, ok := <-events
	if ok {
		t.Fatal("events channel open after unsubscribe")
	}
}
