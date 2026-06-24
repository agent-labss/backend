package agent

import (
	"encoding/json"
	"time"
)

const AgentRunsPath = "/api/agent/runs"

const (
	RunStatusRunning   RunStatus = "running"
	RunStatusSucceeded RunStatus = "succeeded"
	RunStatusFailed    RunStatus = "failed"
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
	ActionTypeFinalAnswer ActionType = "final_answer"
)

const (
	AttachmentKindPDF         AttachmentKind = "pdf"
	AttachmentKindImage       AttachmentKind = "image"
	AttachmentKindSpreadsheet AttachmentKind = "spreadsheet"
	AttachmentKindCSV         AttachmentKind = "csv"
)

type RunStatus string
type StepStatus string
type ToolResultStatus string
type ActionType string
type AttachmentKind string

type CreateRunRequest struct {
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
	Message     string
	Attachments []Attachment
}

type RunResponse struct {
	RunID   string         `json:"run_id"`
	Status  RunStatus      `json:"status"`
	Answer  string         `json:"answer,omitempty"`
	Outputs map[string]any `json:"outputs,omitempty"`
	Error   string         `json:"error,omitempty"`
}

type Run struct {
	ID           string
	Message      string
	Status       RunStatus
	Answer       string
	Outputs      map[string]any
	ErrorSummary string
	StartedAt    time.Time
	FinishedAt   time.Time
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
