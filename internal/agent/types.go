package agent

import (
	"encoding/json"
	"time"
)

const (
	ChatSessionsPath = "/api/chats"
	ChatSessionPath  = ChatSessionsPath + "/:chat_id"
	ChatMessagesPath = ChatSessionPath + "/messages"
	ChatMessagePath  = ChatMessagesPath + "/:message_id"
	ChatEventsPath   = ChatSessionPath + "/events"
)

const (
	RunStatusRunning     RunStatus = "running"
	RunStatusInterrupted RunStatus = "interrupted"
	RunStatusSucceeded   RunStatus = "succeeded"
	RunStatusFailed      RunStatus = "failed"
)

const (
	StepStatusSucceeded StepStatus = "succeeded"
	StepStatusFailed    StepStatus = "failed"
)

const (
	ToolResultStatusOK    ToolResultStatus = "ok"
	ToolResultStatusError ToolResultStatus = "error"
)

const (
	ActionTypeCallTool    ActionType = "call_tool"
	ActionTypeAskUser     ActionType = "ask_user"
	ActionTypeFinalAnswer ActionType = "final_answer"
)

const (
	AttachmentKindPDF         AttachmentKind = "pdf"
	AttachmentKindImage       AttachmentKind = "image"
	AttachmentKindSpreadsheet AttachmentKind = "spreadsheet"
	AttachmentKindCSV         AttachmentKind = "csv"
)

const (
	InterruptionTypeApproval     InterruptionType = "approval"
	InterruptionTypeInputRequest InterruptionType = "input_request"
)

const (
	InterruptionStatusAwaitingReview InterruptionStatus = "awaiting_review"
	InterruptionStatusApproved       InterruptionStatus = "approved"
	InterruptionStatusRejected       InterruptionStatus = "rejected"
	InterruptionStatusResolved       InterruptionStatus = "resolved"
	InterruptionStatusCancelled      InterruptionStatus = "cancelled"
)

const (
	ChatSessionStatusOpen     ChatSessionStatus = "open"
	ChatSessionStatusArchived ChatSessionStatus = "archived"
)

const (
	ChatMessageRoleUser      ChatMessageRole = "user"
	ChatMessageRoleAssistant ChatMessageRole = "assistant"
)

const (
	ChatMessageStatusCompleted ChatMessageStatus = "completed"
	ChatMessageStatusFailed    ChatMessageStatus = "failed"
)

type RunStatus string
type StepStatus string
type ToolResultStatus string
type ActionType string
type AttachmentKind string
type InterruptionType string
type InterruptionStatus string
type ChatSessionStatus string
type ChatMessageRole string
type ChatMessageStatus string

type runRequest struct {
	Message     string       `json:"message"`
	Attachments []Attachment `json:"attachments,omitempty"`
}

type Attachment struct {
	ID       string         `json:"id"`
	Filename string         `json:"filename"`
	MIMEType string         `json:"mime_type"`
	Kind     AttachmentKind `json:"kind"`
	Size     int64          `json:"size"`
	Data     string         `json:"data,omitempty"`
	FileID   string         `json:"file_id,omitempty"`
}

type UploadedFile struct {
	Filename string
	MIMEType string
	Data     []byte
}

type UploadConfig struct {
	MaxFiles      int
	MaxFileBytes  int
	MaxTotalBytes int
}

type CreateRunRecord struct {
	SessionID        string
	TriggerMessageID string
}

type RunResponse struct {
	RunID        string         `json:"run_id"`
	Status       RunStatus      `json:"status"`
	Answer       string         `json:"answer,omitempty"`
	Outputs      map[string]any `json:"outputs,omitempty"`
	Error        string         `json:"error,omitempty"`
	Interruption *Interruption  `json:"interruption,omitempty"`
}

type Run struct {
	ID               string
	SessionID        string
	TriggerMessageID string
	Status           RunStatus
	ErrorSummary     string
	StartedAt        time.Time
	FinishedAt       time.Time
}

type Interruption struct {
	ID          string             `json:"id"`
	SessionID   string             `json:"session_id,omitempty"`
	RunID       string             `json:"run_id,omitempty"`
	Type        InterruptionType   `json:"type"`
	Status      InterruptionStatus `json:"status,omitempty"`
	Message     string             `json:"message"`
	Payload     json.RawMessage    `json:"payload,omitempty"`
	CreatedAt   time.Time          `json:"created_at,omitempty"`
	RespondedAt time.Time          `json:"responded_at,omitempty"`
}

type ObservationRecord struct {
	RunID       string
	StepOrder   int
	Observation Observation
}

type RunStateRecord struct {
	Run                Run
	Interruptions      []Interruption
	ActiveInterruption *Interruption
	Observations       []Observation
}

type StepRecord struct {
	ID            string
	RunID         string
	StepOrder     int
	ToolID        string
	InputSummary  json.RawMessage
	OutputSummary json.RawMessage
	DurationMS    int64
	Status        StepStatus
	ErrorSummary  string
	CreatedAt     time.Time
}

type PlannerAction struct {
	Type    ActionType      `json:"type"`
	Tool    string          `json:"tool,omitempty"`
	Inputs  json.RawMessage `json:"inputs,omitempty"`
	Answer  string          `json:"answer,omitempty"`
	Outputs map[string]any  `json:"outputs,omitempty"`
	Message string          `json:"message,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

type Observation struct {
	StepOrder int            `json:"step_order"`
	ToolName  string         `json:"tool_name"`
	Status    StepStatus     `json:"status"`
	Outputs   map[string]any `json:"outputs,omitempty"`
	Error     string         `json:"error,omitempty"`
}

type ToolInputEnvelope struct {
	RunID   string         `json:"run_id"`
	StepID  string         `json:"step_id"`
	Inputs  map[string]any `json:"inputs"`
	Context map[string]any `json:"context"`
}

type ToolResult struct {
	Status  ToolResultStatus      `json:"status"`
	Outputs map[string]ToolOutput `json:"outputs,omitempty"`
	Summary string                `json:"summary,omitempty"`
	Error   *ToolError            `json:"error,omitempty"`
}

type ToolOutput struct {
	Sensitive bool `json:"sensitive"`
	Value     any  `json:"value"`
}

type ToolError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type CreateChatRequest struct {
	Title string `json:"title,omitempty"`
}

type CreateChatMessageRequest struct {
	Message     string       `json:"message"`
	Attachments []Attachment `json:"attachments,omitempty"`
}

type SubmitChatMessageResponse struct {
	ChatID      string      `json:"chat_id"`
	UserMessage ChatMessage `json:"user_message"`
	RunID       string      `json:"run_id"`
	Status      RunStatus   `json:"status"`
}

type CreateChatSessionRecord struct {
	Title string
}

type CreateChatMessageRecord struct {
	SessionID   string
	RunID       string
	Role        ChatMessageRole
	Content     string
	Attachments []Attachment
}

type ChatSession struct {
	ID        string            `json:"id"`
	Title     string            `json:"title,omitempty"`
	Status    ChatSessionStatus `json:"status"`
	CreatedAt time.Time         `json:"created_at,omitempty"`
	UpdatedAt time.Time         `json:"updated_at,omitempty"`
}

type ChatMessage struct {
	ID          string            `json:"id"`
	SessionID   string            `json:"session_id"`
	RunID       string            `json:"run_id,omitempty"`
	Role        ChatMessageRole   `json:"role"`
	Content     string            `json:"content"`
	Status      ChatMessageStatus `json:"status"`
	Sequence    int               `json:"sequence"`
	Attachments []Attachment      `json:"attachments,omitempty"`
	CreatedAt   time.Time         `json:"created_at,omitempty"`
	CompletedAt time.Time         `json:"completed_at,omitempty"`
	Error       string            `json:"error,omitempty"`
}

type ChatMessageResponse struct {
	ChatID           string        `json:"chat_id"`
	UserMessage      ChatMessage   `json:"user_message"`
	AssistantMessage *ChatMessage  `json:"assistant_message,omitempty"`
	Run              RunResponse   `json:"run"`
	Interruption     *Interruption `json:"interruption,omitempty"`
}
