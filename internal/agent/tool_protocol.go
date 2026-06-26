package agent

const (
	ToolResultStatusOK    ToolResultStatus = "ok"
	ToolResultStatusError ToolResultStatus = "error"
)

type ToolResultStatus string

type ToolInputEnvelope struct {
	ExecutionID string         `json:"execution_id"`
	StepID      string         `json:"step_id"`
	Inputs      map[string]any `json:"inputs"`
	Context     map[string]any `json:"context"`
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
