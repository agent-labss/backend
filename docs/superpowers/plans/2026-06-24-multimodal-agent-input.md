# Multimodal Agent Input Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add V1 multimodal agent runs where users submit a prompt with PDFs, images, Excel workbooks, or CSVs, and AI reads the files before calling controlled business tools.

**Architecture:** Keep multimodal support inside `internal/agent` for the first slice to avoid adding a new package before the boundary proves necessary. Add run-scoped attachment types, upload validation, metadata persistence, multipart parsing, Responses API multimodal input construction, and a confirmation endpoint for preview-based writes. V1 sends uploaded file bytes to the model as Responses input content; provider-hosted file IDs and deterministic parsers remain future upgrades.

**Tech Stack:** Go 1.25.0, Fiber v3, SQLite through GORM, official OpenAI Go SDK `github.com/openai/openai-go/v3`, Responses API input items with `input_text`, `input_file`, and `input_image`, standard-library MIME/base64 handling, existing CLI tool protocol.

---

## Scope

This plan implements the accepted spec in `docs/superpowers/specs/2026-06-24-multimodal-agent-input-design.md`.

V1 behavior:

- Accept `multipart/form-data` agent run requests with `message` and `files[]`.
- Continue accepting JSON run requests with `message` and optional `attachments`.
- Validate file count, MIME type, extension, per-file size, and total upload size.
- Store attachment metadata linked to the run.
- Pass file bytes to OpenAI Responses as multimodal input content.
- Let AI extract structured business changes and decide tool calls.
- Keep all side effects behind registered tools.
- Add a confirmation endpoint that applies a previously previewed change by `preview_id`.

Explicitly out of scope:

- Backend-native PDF parsing, OCR, or Excel interpretation.
- File search, vector stores, or long-term knowledge base indexing.
- Provider file upload lifecycle management through the Files API.
- Async background processing.
- Persisting raw uploaded bytes in SQLite.
- New direct dependencies.

## File Structure

- Modify `internal/config/config.go`: add upload limit defaults and environment variables.
- Modify `internal/config/config_test.go`: cover upload limit defaults, overrides, and invalid values.
- Modify `.env.example`: document upload limit environment variables.
- Modify `internal/database/models.go`: add `AgentRunAttachment`.
- Modify `internal/agent/types.go`: add attachment types, upload constants, confirmation request/response types, and path constant.
- Create `internal/agent/attachments.go`: classify uploaded files, validate limits, build data URLs/file data, and create attachment metadata.
- Create `internal/agent/attachments_test.go`: validation, classification, base64 encoding, and redaction-safe metadata tests.
- Modify `internal/agent/repository.go`: persist attachments when starting a run.
- Modify `internal/agent/repository_test.go`: verify attachment metadata is stored and linked to the run.
- Modify `internal/agent/handler.go`: parse both JSON and multipart requests and add confirmation handler.
- Modify `internal/agent/handler_test.go`: JSON compatibility, multipart success, invalid file errors, and confirmation route tests.
- Modify `internal/agent/service.go`: carry attachments into run state and planner requests; add confirmation method.
- Modify `internal/agent/service_test.go`: verify attachments reach the planner and confirmation calls the executor/tool path selected for apply.
- Modify `internal/agent/llm.go`: include attachment metadata in prompt and build Responses multimodal input content.
- Modify `internal/agent/llm_test.go`: assert prompt includes attachment metadata and OpenAI request payload includes `input_file` / `input_image`.
- Modify `internal/httpapi/router.go`: add confirmation route.
- Modify `internal/httpapi/router_test.go`: verify confirmation route registration.
- Modify `internal/app/app.go`: pass upload config into the agent handler/service.
- Run `./scripts/repo-guard.sh`.

## API Shape

### Create Run With Files

```text
POST /api/agent/runs
Content-Type: multipart/form-data

message=根据我上传的 pdf，更新商家的目录
files[]=merchant_catalog.pdf
```

### Create Run With JSON

```json
{
  "message": "根据我上传的 pdf，更新商家的目录",
  "attachments": [
    {
      "id": "att_123",
      "filename": "merchant_catalog.pdf",
      "mime_type": "application/pdf",
      "kind": "pdf",
      "size": 245912,
      "data": "base64..."
    }
  ]
}
```

### Confirm Preview

```text
POST /api/agent/runs/{run_id}/confirm
```

Request:

```json
{
  "preview_id": "preview_123"
}
```

The confirmation path constructs a controlled agent run message equivalent to "apply this preview", but the apply tool receives only `preview_id`, not an arbitrary model-generated write payload.

## Task 1: Add Upload Configuration

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`
- Modify: `.env.example`

- [ ] **Step 1: Write failing config tests**

Add these defaults to `defaultConfig()` in `internal/config/config_test.go` once the fields exist:

```go
AgentMaxFilesPerRun:       DefaultAgentMaxFilesPerRun,
AgentMaxFileBytes:         DefaultAgentMaxFileBytes,
AgentMaxTotalFileBytes:    DefaultAgentMaxTotalFileBytes,
```

Add this test:

```go
func TestLoadUsesUploadLimitEnvironmentOverrides(t *testing.T) {
	clearEnv(t)
	t.Setenv("AGENT_MAX_FILES_PER_RUN", "4")
	t.Setenv("AGENT_MAX_FILE_BYTES", "1048576")
	t.Setenv("AGENT_MAX_TOTAL_FILE_BYTES", "2097152")

	cfg := Load()
	want := defaultConfig()
	want.AgentMaxFilesPerRun = 4
	want.AgentMaxFileBytes = 1048576
	want.AgentMaxTotalFileBytes = 2097152

	assertConfig(t, cfg, want)
}
```

Extend `TestLoadFallsBackForInvalidAgentNumbers`:

```go
t.Setenv("AGENT_MAX_FILES_PER_RUN", "0")
t.Setenv("AGENT_MAX_FILE_BYTES", "-1")
t.Setenv("AGENT_MAX_TOTAL_FILE_BYTES", "invalid")
```

Extend `clearEnv`:

```go
t.Setenv("AGENT_MAX_FILES_PER_RUN", "")
t.Setenv("AGENT_MAX_FILE_BYTES", "")
t.Setenv("AGENT_MAX_TOTAL_FILE_BYTES", "")
```

- [ ] **Step 2: Run the failing tests**

Run:

```bash
go test ./internal/config
```

Expected: compile failure because the new config fields and constants do not exist.

- [ ] **Step 3: Add config fields and defaults**

In `internal/config/config.go`, add:

```go
DefaultAgentMaxFilesPerRun    = 5
DefaultAgentMaxFileBytes      = 10 * 1024 * 1024
DefaultAgentMaxTotalFileBytes = 25 * 1024 * 1024
```

Add fields to `Config`:

```go
AgentMaxFilesPerRun    int
AgentMaxFileBytes      int
AgentMaxTotalFileBytes int
```

In `Load`, add:

```go
AgentMaxFilesPerRun:    getPositiveIntEnv(dotEnv, "AGENT_MAX_FILES_PER_RUN", DefaultAgentMaxFilesPerRun),
AgentMaxFileBytes:      getPositiveIntEnv(dotEnv, "AGENT_MAX_FILE_BYTES", DefaultAgentMaxFileBytes),
AgentMaxTotalFileBytes: getPositiveIntEnv(dotEnv, "AGENT_MAX_TOTAL_FILE_BYTES", DefaultAgentMaxTotalFileBytes),
```

- [ ] **Step 4: Document env vars**

Append to `.env.example`:

```dotenv
AGENT_MAX_FILES_PER_RUN=5
AGENT_MAX_FILE_BYTES=10485760
AGENT_MAX_TOTAL_FILE_BYTES=26214400
```

- [ ] **Step 5: Run tests**

Run:

```bash
go test ./internal/config
```

Expected: `ok ai/backend/internal/config`.

- [ ] **Step 6: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go .env.example
git commit -m "Add agent upload limit config"
```

## Task 2: Add Attachment Domain Types And Validation

**Files:**
- Modify: `internal/agent/types.go`
- Create: `internal/agent/attachments.go`
- Create: `internal/agent/attachments_test.go`

- [ ] **Step 1: Write failing attachment tests**

Create `internal/agent/attachments_test.go`:

```go
package agent

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

func TestBuildAttachmentAcceptsPDF(t *testing.T) {
	cfg := UploadConfig{MaxFiles: 2, MaxFileBytes: 1024, MaxTotalBytes: 2048}
	attachment, err := buildAttachment(UploadedFile{
		Filename: "merchant_catalog.pdf",
		MIMEType: "application/pdf",
		Data:     []byte("%PDF-1.7"),
	}, cfg)
	if err != nil {
		t.Fatalf("buildAttachment() error = %v", err)
	}
	if attachment.Kind != AttachmentKindPDF {
		t.Fatalf("Kind = %q, want %q", attachment.Kind, AttachmentKindPDF)
	}
	if attachment.MIMEType != "application/pdf" {
		t.Fatalf("MIMEType = %q, want application/pdf", attachment.MIMEType)
	}
	if attachment.Size != int64(len("%PDF-1.7")) {
		t.Fatalf("Size = %d, want %d", attachment.Size, len("%PDF-1.7"))
	}
	if attachment.Data != base64.StdEncoding.EncodeToString([]byte("%PDF-1.7")) {
		t.Fatalf("Data = %q, want base64 file bytes", attachment.Data)
	}
}

func TestBuildAttachmentRejectsUnsupportedType(t *testing.T) {
	cfg := UploadConfig{MaxFiles: 2, MaxFileBytes: 1024, MaxTotalBytes: 2048}
	_, err := buildAttachment(UploadedFile{
		Filename: "script.sh",
		MIMEType: "text/x-shellscript",
		Data:     []byte("echo nope"),
	}, cfg)
	if !errors.Is(err, ErrUnsupportedAttachmentType) {
		t.Fatalf("buildAttachment() error = %v, want ErrUnsupportedAttachmentType", err)
	}
}

func TestValidateUploadedFilesRejectsTooManyFiles(t *testing.T) {
	err := validateUploadedFiles([]UploadedFile{
		{Filename: "a.pdf", MIMEType: "application/pdf", Data: []byte("a")},
		{Filename: "b.pdf", MIMEType: "application/pdf", Data: []byte("b")},
	}, UploadConfig{MaxFiles: 1, MaxFileBytes: 1024, MaxTotalBytes: 2048})
	if !errors.Is(err, ErrTooManyAttachments) {
		t.Fatalf("validateUploadedFiles() error = %v, want ErrTooManyAttachments", err)
	}
}

func TestValidateUploadedFilesRejectsFileTooLarge(t *testing.T) {
	err := validateUploadedFiles([]UploadedFile{
		{Filename: "a.pdf", MIMEType: "application/pdf", Data: []byte("abc")},
	}, UploadConfig{MaxFiles: 1, MaxFileBytes: 2, MaxTotalBytes: 2048})
	if !errors.Is(err, ErrAttachmentTooLarge) {
		t.Fatalf("validateUploadedFiles() error = %v, want ErrAttachmentTooLarge", err)
	}
}

func TestValidateUploadedFilesRejectsTotalTooLarge(t *testing.T) {
	err := validateUploadedFiles([]UploadedFile{
		{Filename: "a.pdf", MIMEType: "application/pdf", Data: []byte("ab")},
		{Filename: "b.pdf", MIMEType: "application/pdf", Data: []byte("cd")},
	}, UploadConfig{MaxFiles: 2, MaxFileBytes: 10, MaxTotalBytes: 3})
	if !errors.Is(err, ErrAttachmentTooLarge) {
		t.Fatalf("validateUploadedFiles() error = %v, want ErrAttachmentTooLarge", err)
	}
}

func TestAttachmentPromptViewOmitsRawData(t *testing.T) {
	view := attachmentPromptView(Attachment{
		ID:       "att_1",
		Filename: "merchant_catalog.pdf",
		MIMEType: "application/pdf",
		Kind:     AttachmentKindPDF,
		Size:     8,
		Data:     base64.StdEncoding.EncodeToString([]byte("%PDF-1.7")),
	})
	if strings.Contains(view["data"].(string), "%PDF") {
		t.Fatalf("prompt view leaked raw data: %#v", view)
	}
}
```

- [ ] **Step 2: Run failing tests**

Run:

```bash
go test ./internal/agent -run 'TestBuildAttachment|TestValidateUploadedFiles|TestAttachmentPromptView'
```

Expected: compile failure because attachment types and helpers do not exist.

- [ ] **Step 3: Add attachment types**

In `internal/agent/types.go`, add:

```go
const AgentRunConfirmPath = AgentRunsPath + "/:run_id/confirm"

const (
	AttachmentKindPDF         AttachmentKind = "pdf"
	AttachmentKindImage       AttachmentKind = "image"
	AttachmentKindSpreadsheet AttachmentKind = "spreadsheet"
	AttachmentKindCSV         AttachmentKind = "csv"
)

type AttachmentKind string

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

type ConfirmRunRequest struct {
	PreviewID string `json:"preview_id"`
}
```

Update `CreateRunRequest`:

```go
type CreateRunRequest struct {
	Message     string       `json:"message"`
	Attachments []Attachment `json:"attachments,omitempty"`
}
```

- [ ] **Step 4: Add validation helpers**

Create `internal/agent/attachments.go`:

```go
package agent

import (
	"encoding/base64"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

var (
	ErrTooManyAttachments      = errors.New("too many attachments")
	ErrAttachmentTooLarge      = errors.New("attachment too large")
	ErrUnsupportedAttachmentType = errors.New("unsupported attachment type")
)

func buildAttachments(files []UploadedFile, config UploadConfig) ([]Attachment, error) {
	if err := validateUploadedFiles(files, config); err != nil {
		return nil, err
	}
	attachments := make([]Attachment, 0, len(files))
	for _, file := range files {
		attachment, err := buildAttachment(file, config)
		if err != nil {
			return nil, err
		}
		attachments = append(attachments, attachment)
	}
	return attachments, nil
}

func validateUploadedFiles(files []UploadedFile, config UploadConfig) error {
	if len(files) > config.MaxFiles {
		return ErrTooManyAttachments
	}
	totalBytes := 0
	for _, file := range files {
		size := len(file.Data)
		if size > config.MaxFileBytes {
			return ErrAttachmentTooLarge
		}
		totalBytes += size
		if totalBytes > config.MaxTotalBytes {
			return ErrAttachmentTooLarge
		}
	}
	return nil
}

func buildAttachment(file UploadedFile, _ UploadConfig) (Attachment, error) {
	kind, err := classifyAttachment(file.Filename, file.MIMEType)
	if err != nil {
		return Attachment{}, err
	}
	return Attachment{
		ID:       newRuntimeID("att"),
		Filename: filepath.Base(file.Filename),
		MIMEType: strings.TrimSpace(file.MIMEType),
		Kind:     kind,
		Size:     int64(len(file.Data)),
		Data:     base64.StdEncoding.EncodeToString(file.Data),
	}, nil
}

func classifyAttachment(filename string, mimeType string) (AttachmentKind, error) {
	extension := strings.ToLower(filepath.Ext(filename))
	normalizedMIME := strings.ToLower(strings.TrimSpace(mimeType))
	switch {
	case normalizedMIME == "application/pdf" && extension == ".pdf":
		return AttachmentKindPDF, nil
	case (normalizedMIME == "image/png" && extension == ".png") ||
		(normalizedMIME == "image/jpeg" && (extension == ".jpg" || extension == ".jpeg")):
		return AttachmentKindImage, nil
	case normalizedMIME == "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet" && extension == ".xlsx":
		return AttachmentKindSpreadsheet, nil
	case normalizedMIME == "text/csv" && extension == ".csv":
		return AttachmentKindCSV, nil
	default:
		return "", fmt.Errorf("%w: %s %s", ErrUnsupportedAttachmentType, mimeType, extension)
	}
}

func attachmentPromptView(attachment Attachment) map[string]any {
	return map[string]any{
		"id":        attachment.ID,
		"filename":  attachment.Filename,
		"mime_type": attachment.MIMEType,
		"kind":      attachment.Kind,
		"size":      attachment.Size,
		"data":      "[omitted]",
		"file_id":   attachment.FileID,
	}
}
```

- [ ] **Step 5: Run attachment tests**

Run:

```bash
go test ./internal/agent -run 'TestBuildAttachment|TestValidateUploadedFiles|TestAttachmentPromptView'
```

Expected: tests pass.

- [ ] **Step 6: Commit**

```bash
git add internal/agent/types.go internal/agent/attachments.go internal/agent/attachments_test.go
git commit -m "Add agent attachment validation"
```

## Task 3: Persist Attachment Metadata

**Files:**
- Modify: `internal/database/models.go`
- Modify: `internal/agent/repository.go`
- Modify: `internal/agent/repository_test.go`

- [ ] **Step 1: Write failing repository test**

Add to `internal/agent/repository_test.go`:

```go
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
```

- [ ] **Step 2: Run failing test**

Run:

```bash
go test ./internal/agent -run TestRepositoryPersistsRunAttachments
```

Expected: compile failure because `CreateRunRecord` and `database.AgentRunAttachment` do not exist, and `StartRun` still accepts a string.

- [ ] **Step 3: Add database model**

In `internal/database/models.go`, add:

```go
type AgentRunAttachment struct {
	ID             string    `gorm:"primaryKey;type:text"`
	RunID          string    `gorm:"not null;index"`
	Filename       string    `gorm:"not null"`
	MIMEType       string    `gorm:"not null"`
	Kind           string    `gorm:"not null;index"`
	SizeBytes      int64     `gorm:"not null"`
	ProviderFileID string    `gorm:"not null;default:''"`
	CreatedAt      time.Time `gorm:"not null"`
	Run            AgentRun  `gorm:"foreignKey:RunID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE"`
}
```

Add it to `Models()` after `AgentRun`:

```go
&AgentRunAttachment{},
```

- [ ] **Step 4: Add repository input record**

In `internal/agent/types.go`, add:

```go
type CreateRunRecord struct {
	Message     string
	Attachments []Attachment
}
```

Change `RunStore` in `internal/agent/service.go`:

```go
StartRun(ctx context.Context, record CreateRunRecord) (Run, error)
```

Update fake stores in `internal/agent/service_test.go` to accept `CreateRunRecord` and use `record.Message`.

- [ ] **Step 5: Update repository StartRun**

In `internal/agent/repository.go`, change:

```go
func (repository Repository) StartRun(ctx context.Context, record CreateRunRecord) (Run, error)
```

Inside the function:

```go
run := Run{
	ID:        newRuntimeID("run"),
	Message:   RedactText(record.Message),
	Status:    RunStatusRunning,
	StartedAt: time.Now().UTC(),
}
```

After creating the `AgentRun`, create attachment records:

```go
for _, attachment := range record.Attachments {
	attachmentRecord := database.AgentRunAttachment{
		ID:             attachment.ID,
		RunID:          run.ID,
		Filename:       RedactText(attachment.Filename),
		MIMEType:       attachment.MIMEType,
		Kind:           string(attachment.Kind),
		SizeBytes:      attachment.Size,
		ProviderFileID: attachment.FileID,
		CreatedAt:      time.Now().UTC(),
	}
	if err := typed.G[database.AgentRunAttachment](repository.database).Create(ctx, &attachmentRecord); err != nil {
		return Run{}, fmt.Errorf("save attachment: %w", err)
	}
}
```

- [ ] **Step 6: Update service call site**

In `internal/agent/service.go`, replace:

```go
run, err := service.runStore.StartRun(ctx, request.Message)
```

with:

```go
run, err := service.runStore.StartRun(ctx, CreateRunRecord{
	Message:     request.Message,
	Attachments: request.Attachments,
})
```

- [ ] **Step 7: Run repository and service tests**

Run:

```bash
go test ./internal/agent -run 'TestRepository|TestService'
```

Expected: tests pass.

- [ ] **Step 8: Commit**

```bash
git add internal/database/models.go internal/agent/types.go internal/agent/repository.go internal/agent/repository_test.go internal/agent/service.go internal/agent/service_test.go
git commit -m "Persist agent run attachment metadata"
```

## Task 4: Parse Multipart Agent Runs

**Files:**
- Modify: `internal/agent/handler.go`
- Modify: `internal/agent/handler_test.go`
- Modify: `internal/app/app.go`

- [ ] **Step 1: Update handler tests with multipart success**

Modify `fakeRunService` in `internal/agent/handler_test.go`:

```go
request CreateRunRequest
```

Update `Run`:

```go
func (service *fakeRunService) Run(_ context.Context, request CreateRunRequest) (RunResponse, error) {
	service.called = true
	service.request = request
	return service.response, service.err
}
```

Add:

```go
func TestHandlerCreateRunAcceptsMultipartFiles(t *testing.T) {
	service := &fakeRunService{response: RunResponse{RunID: testRunID, Status: RunStatusSucceeded, Answer: "preview ready"}}
	handler := NewHandler(service, UploadConfig{MaxFiles: 2, MaxFileBytes: 1024, MaxTotalBytes: 2048})
	app := fiber.New()
	app.Post(AgentRunsPath, handler.CreateRun)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("message", "根据我上传的 pdf，更新商家的目录"); err != nil {
		t.Fatalf("WriteField() error = %v", err)
	}
	part, err := writer.CreateFormFile("files[]", "merchant_catalog.pdf")
	if err != nil {
		t.Fatalf("CreateFormFile() error = %v", err)
	}
	if _, err := part.Write([]byte("%PDF-1.7")); err != nil {
		t.Fatalf("Write file error = %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close writer error = %v", err)
	}

	req, err := http.NewRequest(http.MethodPost, AgentRunsPath, &body)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Test() error = %v", err)
	}
	defer closeAgentResponseBody(t, resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("StatusCode = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if len(service.request.Attachments) != 1 {
		t.Fatalf("len(Attachments) = %d, want 1", len(service.request.Attachments))
	}
	if service.request.Attachments[0].Kind != AttachmentKindPDF {
		t.Fatalf("Kind = %q, want %q", service.request.Attachments[0].Kind, AttachmentKindPDF)
	}
}
```

Add imports:

```go
"mime/multipart"
```

- [ ] **Step 2: Add invalid file test**

Add:

```go
func TestHandlerCreateRunRejectsUnsupportedMultipartFile(t *testing.T) {
	service := &fakeRunService{}
	handler := NewHandler(service, UploadConfig{MaxFiles: 2, MaxFileBytes: 1024, MaxTotalBytes: 2048})
	app := fiber.New()
	app.Post(AgentRunsPath, handler.CreateRun)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("message", "read file"); err != nil {
		t.Fatalf("WriteField() error = %v", err)
	}
	part, err := writer.CreateFormFile("files[]", "script.sh")
	if err != nil {
		t.Fatalf("CreateFormFile() error = %v", err)
	}
	if _, err := part.Write([]byte("echo nope")); err != nil {
		t.Fatalf("Write file error = %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close writer error = %v", err)
	}

	req, err := http.NewRequest(http.MethodPost, AgentRunsPath, &body)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Test() error = %v", err)
	}
	defer closeAgentResponseBody(t, resp)

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("StatusCode = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
	if service.called {
		t.Fatal("Run() called for invalid file, want no call")
	}
}
```

- [ ] **Step 3: Run failing tests**

Run:

```bash
go test ./internal/agent -run 'TestHandlerCreateRunAcceptsMultipartFiles|TestHandlerCreateRunRejectsUnsupportedMultipartFile'
```

Expected: compile failure because `NewHandler` does not accept upload config and multipart parsing is not implemented.

- [ ] **Step 4: Update handler constructor**

In `internal/agent/handler.go`, change:

```go
type Handler struct {
	runner Runner
	uploadConfig UploadConfig
}

func NewHandler(runner Runner, uploadConfig UploadConfig) Handler {
	return Handler{runner: runner, uploadConfig: uploadConfig}
}
```

Update existing tests and `internal/app/app.go` to pass an `UploadConfig`.

- [ ] **Step 5: Parse request by content type**

In `CreateRun`, replace direct JSON bind with:

```go
request, err := handler.createRunRequest(c)
if err != nil {
	return c.Status(http.StatusBadRequest).JSON(fiber.Map{errorField: err.Error()})
}
```

Add:

```go
func (handler Handler) createRunRequest(c fiber.Ctx) (CreateRunRequest, error) {
	contentType := strings.ToLower(c.Get("Content-Type"))
	if strings.HasPrefix(contentType, "multipart/form-data") {
		return handler.multipartCreateRunRequest(c)
	}
	var request CreateRunRequest
	if err := c.Bind().Body(&request); err != nil {
		return CreateRunRequest{}, errors.New("invalid JSON request body")
	}
	return request, nil
}
```

Add `errors` import.

- [ ] **Step 6: Add multipart parsing helper**

Add:

```go
func (handler Handler) multipartCreateRunRequest(c fiber.Ctx) (CreateRunRequest, error) {
	form, err := c.MultipartForm()
	if err != nil {
		return CreateRunRequest{}, errors.New("invalid multipart request body")
	}
	message := strings.TrimSpace(c.FormValue("message"))
	files := uploadedFilesFromMultipart(form)
	attachments, err := buildAttachments(files, handler.uploadConfig)
	if err != nil {
		return CreateRunRequest{}, err
	}
	return CreateRunRequest{Message: message, Attachments: attachments}, nil
}

func uploadedFilesFromMultipart(form *multipart.Form) []UploadedFile {
	headers := append([]*multipart.FileHeader{}, form.File["files"]...)
	headers = append(headers, form.File["files[]"]...)
	files := make([]UploadedFile, 0, len(headers))
	for _, header := range headers {
		file, err := header.Open()
		if err != nil {
			continue
		}
		data, readErr := io.ReadAll(file)
		_ = file.Close()
		if readErr != nil {
			continue
		}
		files = append(files, UploadedFile{
			Filename: header.Filename,
			MIMEType: header.Header.Get("Content-Type"),
			Data:     data,
		})
	}
	return files
}
```

Add imports:

```go
"io"
"mime/multipart"
```

- [ ] **Step 7: Keep blank message validation**

After parsing:

```go
if strings.TrimSpace(request.Message) == "" {
	return c.Status(http.StatusBadRequest).JSON(fiber.Map{errorField: "message is required"})
}
```

- [ ] **Step 8: Run handler tests**

Run:

```bash
go test ./internal/agent -run TestHandlerCreateRun
```

Expected: tests pass.

- [ ] **Step 9: Commit**

```bash
git add internal/agent/handler.go internal/agent/handler_test.go internal/app/app.go
git commit -m "Accept multipart agent run attachments"
```

## Task 5: Pass Attachments To The Planner

**Files:**
- Modify: `internal/agent/service.go`
- Modify: `internal/agent/service_test.go`
- Modify: `internal/agent/llm.go`
- Modify: `internal/agent/llm_test.go`

- [ ] **Step 1: Capture planner requests in service tests**

Modify `fakePlanner` in `internal/agent/service_test.go`:

```go
requests []PlanRequest
```

In `NextAction`:

```go
planner.requests = append(planner.requests, request)
```

Add:

```go
func TestServiceRunPassesAttachmentsToPlanner(t *testing.T) {
	planner := &fakePlanner{actions: []PlannerAction{{Type: ActionTypeFinalAnswer, Answer: "preview ready"}}}
	store := &memoryRunStore{}
	service := NewService(ServiceConfig{
		Planner: planner,
		Catalog: fakeCatalog{instructions: toolcatalog.Instructions{Content: "use evidence"}},
		Executor: &fakeExecutor{},
		RunStore: store,
		MaxSteps: 1,
		TotalTimeout: time.Second,
	})

	_, err := service.Run(context.Background(), CreateRunRequest{
		Message: "update catalog",
		Attachments: []Attachment{{
			ID: "att_1", Filename: "merchant_catalog.pdf", MIMEType: "application/pdf", Kind: AttachmentKindPDF, Size: 8, Data: "base64",
		}},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(planner.requests) != 1 {
		t.Fatalf("len(planner.requests) = %d, want 1", len(planner.requests))
	}
	if len(planner.requests[0].Attachments) != 1 {
		t.Fatalf("len(Attachments) = %d, want 1", len(planner.requests[0].Attachments))
	}
}
```

- [ ] **Step 2: Run failing service test**

Run:

```bash
go test ./internal/agent -run TestServiceRunPassesAttachmentsToPlanner
```

Expected: compile failure because `PlanRequest.Attachments` does not exist.

- [ ] **Step 3: Add attachments to run state and plan request**

In `internal/agent/llm.go`, update:

```go
type PlanRequest struct {
	Instructions string
	Message      string
	Attachments []Attachment
	Tools        []toolcatalog.Tool
	Observations []Observation
}
```

In `internal/agent/service.go`, add to `runState`:

```go
attachments []Attachment
```

Set it in `newRunState`:

```go
attachments: request.Attachments,
```

Pass it in `runStep`:

```go
Attachments: state.attachments,
```

- [ ] **Step 4: Add planner prompt test**

Add to `internal/agent/llm_test.go`:

```go
func TestBuildPlannerPromptIncludesAttachmentMetadata(t *testing.T) {
	prompt, err := buildPlannerPrompt(PlanRequest{
		Instructions: "use evidence",
		Message: "update catalog",
		Attachments: []Attachment{{
			ID: "att_1", Filename: "merchant_catalog.pdf", MIMEType: "application/pdf", Kind: AttachmentKindPDF, Size: 8, Data: "base64-must-not-appear",
		}},
	})
	if err != nil {
		t.Fatalf("buildPlannerPrompt() error = %v", err)
	}
	var body map[string]any
	if err := json.Unmarshal(prompt, &body); err != nil {
		t.Fatalf("Unmarshal prompt error = %v", err)
	}
	attachments, ok := body["attachments"].([]any)
	if !ok || len(attachments) != 1 {
		t.Fatalf("attachments = %#v, want one attachment", body["attachments"])
	}
	raw, _ := json.Marshal(body)
	if strings.Contains(string(raw), "base64-must-not-appear") {
		t.Fatalf("prompt leaked attachment data: %s", raw)
	}
}
```

Add `strings` import to `llm_test.go`.

- [ ] **Step 5: Update prompt builder**

In `buildPlannerPrompt`, add:

```go
"attachments": plannerAttachments(request.Attachments),
```

Add:

```go
func plannerAttachments(attachments []Attachment) []map[string]any {
	result := make([]map[string]any, 0, len(attachments))
	for _, attachment := range attachments {
		result = append(result, attachmentPromptView(attachment))
	}
	return result
}
```

- [ ] **Step 6: Run service and prompt tests**

Run:

```bash
go test ./internal/agent -run 'TestServiceRunPassesAttachmentsToPlanner|TestBuildPlannerPromptIncludesAttachmentMetadata'
```

Expected: tests pass.

- [ ] **Step 7: Commit**

```bash
git add internal/agent/service.go internal/agent/service_test.go internal/agent/llm.go internal/agent/llm_test.go
git commit -m "Pass agent attachments to planner"
```

## Task 6: Send Multimodal Content To OpenAI

**Files:**
- Modify: `internal/agent/llm.go`
- Modify: `internal/agent/llm_test.go`

- [ ] **Step 1: Write request payload assertion test**

Extend `assertPlannerRequest` in `internal/agent/llm_test.go` to decode `input` as JSON:

```go
var payload struct {
	Model string `json:"model"`
	Input []struct {
		Type string `json:"type"`
		Role string `json:"role"`
		Content []map[string]any `json:"content"`
	} `json:"input"`
	Text struct {
		Format struct {
			Type string `json:"type"`
		} `json:"format"`
	} `json:"text"`
}
```

Keep the existing model and text format assertions. Add:

```go
if len(payload.Input) != 1 {
	t.Errorf("len(input) = %d, want 1", len(payload.Input))
	http.Error(w, "wrong input", http.StatusBadRequest)
	return false
}
if payload.Input[0].Role != "user" {
	t.Errorf("input role = %q, want user", payload.Input[0].Role)
	http.Error(w, "wrong role", http.StatusBadRequest)
	return false
}
```

Update `TestOpenAIPlannerUsesBaseURL` request to include one PDF attachment:

```go
Attachments: []Attachment{{
	ID: "att_1", Filename: "merchant_catalog.pdf", MIMEType: "application/pdf", Kind: AttachmentKindPDF, Size: 8, Data: base64.StdEncoding.EncodeToString([]byte("%PDF-1.7")),
}},
```

Add `encoding/base64` import.

In `assertPlannerRequest`, assert the content includes text and file:

```go
hasText := false
hasFile := false
for _, content := range payload.Input[0].Content {
	switch content["type"] {
	case "input_text":
		hasText = true
	case "input_file":
		hasFile = true
		if content["filename"] != "merchant_catalog.pdf" {
			t.Errorf("filename = %v, want merchant_catalog.pdf", content["filename"])
		}
	}
}
if !hasText || !hasFile {
	t.Errorf("content hasText=%v hasFile=%v, want both", hasText, hasFile)
	http.Error(w, "missing multimodal content", http.StatusBadRequest)
	return false
}
```

- [ ] **Step 2: Run failing OpenAI planner test**

Run:

```bash
go test ./internal/agent -run TestOpenAIPlannerUsesBaseURL
```

Expected: test fails because planner still sends `Input.OfString`.

- [ ] **Step 3: Build multimodal Responses input**

In `internal/agent/llm.go`, import:

```go
"github.com/openai/openai-go/v3/packages/param"
```

Replace:

```go
Input: responses.ResponseNewParamsInputUnion{
	OfString: openai.String(string(prompt)),
},
```

with:

```go
Input: responses.ResponseNewParamsInputUnion{
	OfInputItemList: plannerInputItems(prompt, request.Attachments),
},
```

Add:

```go
func plannerInputItems(prompt []byte, attachments []Attachment) responses.ResponseInputParam {
	content := responses.ResponseInputMessageContentListParam{{
		OfInputText: &responses.ResponseInputTextParam{Text: string(prompt)},
	}}
	for _, attachment := range attachments {
		content = append(content, plannerAttachmentContent(attachment))
	}
	return responses.ResponseInputParam{{
		OfMessage: &responses.ResponseInputItemMessageParam{
			Type: "message",
			Role: "user",
			Content: content,
		},
	}}
}

func plannerAttachmentContent(attachment Attachment) responses.ResponseInputContentUnionParam {
	if attachment.Kind == AttachmentKindImage {
		return responses.ResponseInputContentUnionParam{
			OfInputImage: &responses.ResponseInputImageParam{
				Detail: responses.ResponseInputImageDetailAuto,
				ImageURL: param.NewOpt("data:" + attachment.MIMEType + ";base64," + attachment.Data),
			},
		}
	}
	return responses.ResponseInputContentUnionParam{
		OfInputFile: &responses.ResponseInputFileParam{
			Filename: param.NewOpt(attachment.Filename),
			FileData: param.NewOpt(attachment.Data),
			Detail: responses.ResponseInputFileDetailLow,
		},
	}
}
```

If `responses.ResponseInputParam` is not the exact alias accepted by the SDK, use the compiler error to select the generated type from `responses/response.go`; do not replace this with ad hoc raw JSON.

- [ ] **Step 4: Run OpenAI planner tests**

Run:

```bash
go test ./internal/agent -run 'TestOpenAIPlannerUsesBaseURL|TestBuildPlannerPromptIncludesAttachmentMetadata'
```

Expected: tests pass.

- [ ] **Step 5: Commit**

```bash
git add internal/agent/llm.go internal/agent/llm_test.go
git commit -m "Send agent attachments to OpenAI"
```

## Task 7: Add Confirmation Endpoint

**Files:**
- Modify: `internal/agent/types.go`
- Modify: `internal/agent/handler.go`
- Modify: `internal/agent/handler_test.go`
- Modify: `internal/agent/service.go`
- Modify: `internal/agent/service_test.go`
- Modify: `internal/httpapi/router.go`
- Modify: `internal/httpapi/router_test.go`

- [ ] **Step 1: Add runner interface and handler test**

In `internal/agent/handler.go`, update:

```go
type Runner interface {
	Run(ctx context.Context, request CreateRunRequest) (RunResponse, error)
	Confirm(ctx context.Context, runID string, request ConfirmRunRequest) (RunResponse, error)
}
```

Update `fakeRunService`:

```go
confirmRunID string
confirmRequest ConfirmRunRequest
```

Add:

```go
func (service *fakeRunService) Confirm(_ context.Context, runID string, request ConfirmRunRequest) (RunResponse, error) {
	service.confirmRunID = runID
	service.confirmRequest = request
	return service.response, service.err
}
```

Add test:

```go
func TestHandlerConfirmRunReturnsResponse(t *testing.T) {
	service := &fakeRunService{response: RunResponse{RunID: testRunID, Status: RunStatusSucceeded, Answer: "applied"}}
	handler := NewHandler(service, UploadConfig{MaxFiles: 1, MaxFileBytes: 1024, MaxTotalBytes: 1024})
	app := fiber.New()
	app.Post(AgentRunConfirmPath, handler.ConfirmRun)

	req, err := http.NewRequest(http.MethodPost, "/api/agent/runs/"+testRunID+"/confirm", bytes.NewReader([]byte(`{"preview_id":"preview_123"}`)))
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Test() error = %v", err)
	}
	defer closeAgentResponseBody(t, resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("StatusCode = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if service.confirmRunID != testRunID {
		t.Fatalf("confirmRunID = %q, want %s", service.confirmRunID, testRunID)
	}
	if service.confirmRequest.PreviewID != "preview_123" {
		t.Fatalf("PreviewID = %q, want preview_123", service.confirmRequest.PreviewID)
	}
}
```

- [ ] **Step 2: Run failing handler test**

Run:

```bash
go test ./internal/agent -run TestHandlerConfirmRunReturnsResponse
```

Expected: compile failure because `ConfirmRun` is missing.

- [ ] **Step 3: Implement handler confirmation**

In `internal/agent/handler.go`, add:

```go
func (handler Handler) ConfirmRun(c fiber.Ctx) error {
	var request ConfirmRunRequest
	if err := c.Bind().Body(&request); err != nil {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{errorField: "invalid JSON request body"})
	}
	if strings.TrimSpace(request.PreviewID) == "" {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{errorField: "preview_id is required"})
	}
	response, err := handler.runner.Confirm(c.Context(), c.Params("run_id"), request)
	if err != nil {
		return writeRunError(c, response)
	}
	return c.Status(http.StatusOK).JSON(response)
}
```

- [ ] **Step 4: Add service confirmation behavior**

In `internal/agent/service.go`, add:

```go
func (service Service) Confirm(ctx context.Context, runID string, request ConfirmRunRequest) (RunResponse, error) {
	return service.Run(ctx, CreateRunRequest{
		Message: "Apply confirmed catalog update preview " + request.PreviewID + " for run " + runID + ". Call the apply tool with only the preview_id.",
	})
}
```

This keeps V1 simple and reuses the existing tool loop. A future implementation can bypass the planner and call a typed apply service directly.

- [ ] **Step 5: Add router method**

In `internal/httpapi/router.go`, update `AgentHandler`:

```go
ConfirmRun(c fiber.Ctx) error
```

Register:

```go
app.Post(agent.AgentRunConfirmPath, config.AgentHandler.ConfirmRun)
```

Update `fakeAgentHandler` in `internal/httpapi/router_test.go`:

```go
func (handler fakeAgentHandler) ConfirmRun(c fiber.Ctx) error {
	return c.SendStatus(http.StatusOK)
}
```

Add:

```go
func TestAgentRunConfirmRouteIsRegistered(t *testing.T) {
	assertRouteStatus(t, http.MethodPost, "/api/agent/runs/run_123/confirm", http.StatusOK)
}
```

- [ ] **Step 6: Run handler and router tests**

Run:

```bash
go test ./internal/agent ./internal/httpapi
```

Expected: tests pass.

- [ ] **Step 7: Commit**

```bash
git add internal/agent/types.go internal/agent/handler.go internal/agent/handler_test.go internal/agent/service.go internal/agent/service_test.go internal/httpapi/router.go internal/httpapi/router_test.go
git commit -m "Add agent run confirmation endpoint"
```

## Task 8: Wire Upload Config In App

**Files:**
- Modify: `internal/app/app.go`
- Modify: `internal/app/app_test.go`

- [ ] **Step 1: Update agent handler construction**

In `internal/app/app.go`, update `newAgentHandler`:

```go
func newAgentHandler(cfg config.Config, repository agent.Repository, catalog agent.Catalog) agent.Handler {
	planner := agent.NewOpenAIPlanner(cfg.OpenAIAPIKey, cfg.OpenAIModel, cfg.OpenAIBaseURL)
	executor := agent.NewCLIExecutor()
	service := agent.NewService(agent.ServiceConfig{
		Planner:      planner,
		Catalog:      catalog,
		Executor:     executor,
		RunStore:     repository,
		MaxSteps:     cfg.AgentMaxSteps,
		TotalTimeout: time.Duration(cfg.AgentTotalTimeoutMS) * time.Millisecond,
	})
	return agent.NewHandler(service, agent.UploadConfig{
		MaxFiles:      cfg.AgentMaxFilesPerRun,
		MaxFileBytes:  cfg.AgentMaxFileBytes,
		MaxTotalBytes: cfg.AgentMaxTotalFileBytes,
	})
}
```

- [ ] **Step 2: Run app tests**

Run:

```bash
go test ./internal/app
```

Expected: tests pass.

- [ ] **Step 3: Commit**

```bash
git add internal/app/app.go internal/app/app_test.go
git commit -m "Wire agent upload limits"
```

## Task 9: Full Verification

**Files:**
- All modified files

- [ ] **Step 1: Format changed Go files**

Run:

```bash
gofmt -w internal/config/config.go internal/config/config_test.go internal/database/models.go internal/agent/types.go internal/agent/attachments.go internal/agent/attachments_test.go internal/agent/repository.go internal/agent/repository_test.go internal/agent/handler.go internal/agent/handler_test.go internal/agent/service.go internal/agent/service_test.go internal/agent/llm.go internal/agent/llm_test.go internal/httpapi/router.go internal/httpapi/router_test.go internal/app/app.go
```

- [ ] **Step 2: Run full guard**

Run:

```bash
./scripts/repo-guard.sh
```

Expected:

```text
repo guard passed
```

The command may show the existing `gomodguard` deprecation warning; that warning is acceptable only if the final exit code is 0 and the output ends with `repo guard passed`.

- [ ] **Step 3: Inspect final diff**

Run:

```bash
git status --short
git diff --stat HEAD
```

Expected: only files from this plan are modified.

- [ ] **Step 4: Final commit**

If any verification-only formatting changes remain:

```bash
git add .
git commit -m "Verify multimodal agent input"
```

If no changes remain, do not create an empty commit.

## Self-Review

- Spec coverage: The plan covers run-scoped attachments, AI file understanding, upload validation, prompt metadata, OpenAI multimodal input, attachment audit metadata, preview/apply confirmation, and repo guard verification.
- Scope control: The plan does not add deterministic parsers, vector stores, async queues, raw file persistence, or new dependencies.
- Type consistency: `Attachment`, `UploadedFile`, `UploadConfig`, `CreateRunRecord`, and `ConfirmRunRequest` are introduced before use.
- Risk note: Task 6 uses generated OpenAI SDK union types. If the SDK type alias name differs during implementation, use compiler errors and the SDK source to adjust the type name while keeping the same structured SDK API approach.
