package agent

import "sync"

const defaultEventBusBuffer = 16

const (
	EventTypeConnected            ChatEventType = "connected"
	EventTypeMessageCreated       ChatEventType = "message.created"
	EventTypeRunStarted           ChatEventType = "run.started"
	EventTypeRunResumed           ChatEventType = "run.resumed"
	EventTypeToolStarted          ChatEventType = "tool.started"
	EventTypeToolFinished         ChatEventType = "tool.finished"
	EventTypeInterruptionCreated  ChatEventType = "interruption.created"
	EventTypeInterruptionResolved ChatEventType = "interruption.resolved"
	EventTypeRunCompleted         ChatEventType = "run.completed"
	EventTypeRunFailed            ChatEventType = "run.failed"
)

type ChatEventType string

type ChatEvent struct {
	Type           ChatEventType `json:"type"`
	ChatID         string        `json:"chat_id"`
	MessageID      string        `json:"message_id,omitempty"`
	RunID          string        `json:"run_id,omitempty"`
	ToolName       string        `json:"tool_name,omitempty"`
	InterruptionID string        `json:"interruption_id,omitempty"`
	Message        *ChatMessage  `json:"message,omitempty"`
	Run            *RunResponse  `json:"run,omitempty"`
	Interruption   *Interruption `json:"interruption,omitempty"`
	Observation    *Observation  `json:"observation,omitempty"`
	Error          string        `json:"error,omitempty"`
}

type EventBus struct {
	mu          sync.Mutex
	nextID      int
	buffer      int
	subscribers map[string]map[int]chan ChatEvent
}

func NewEventBus() *EventBus {
	return NewEventBusWithBuffer(defaultEventBusBuffer)
}

func NewEventBusWithBuffer(buffer int) *EventBus {
	if buffer < 1 {
		buffer = defaultEventBusBuffer
	}
	return &EventBus{
		buffer:      buffer,
		subscribers: make(map[string]map[int]chan ChatEvent),
	}
}

func (bus *EventBus) Subscribe(chatID string) (<-chan ChatEvent, func()) {
	bus.mu.Lock()
	defer bus.mu.Unlock()

	bus.nextID++
	id := bus.nextID
	events := make(chan ChatEvent, bus.buffer)
	if bus.subscribers[chatID] == nil {
		bus.subscribers[chatID] = make(map[int]chan ChatEvent)
	}
	bus.subscribers[chatID][id] = events

	var once sync.Once
	unsubscribe := func() {
		once.Do(func() {
			bus.mu.Lock()
			defer bus.mu.Unlock()
			bus.removeSubscriber(chatID, id)
		})
	}
	return events, unsubscribe
}

func (bus *EventBus) Publish(chatID string, event ChatEvent) {
	bus.mu.Lock()
	defer bus.mu.Unlock()

	if event.ChatID == "" {
		event.ChatID = chatID
	}
	for id, events := range bus.subscribers[chatID] {
		select {
		case events <- event:
		default:
			bus.removeSubscriber(chatID, id)
		}
	}
}

func (bus *EventBus) removeSubscriber(chatID string, id int) {
	events, ok := bus.subscribers[chatID][id]
	if !ok {
		return
	}
	delete(bus.subscribers[chatID], id)
	close(events)
	if len(bus.subscribers[chatID]) == 0 {
		delete(bus.subscribers, chatID)
	}
}
