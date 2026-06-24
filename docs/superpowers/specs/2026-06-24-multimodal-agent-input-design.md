# Multimodal Agent Input Design

## Summary

Add first-stage multimodal input support to the agent so users can submit a natural-language prompt with files such as PDFs, images, Excel workbooks, or CSVs.

The first version intentionally lets AI perform file understanding and structured extraction. The backend does not implement deep PDF parsing, OCR, or spreadsheet interpretation in this slice. It owns the upload boundary, file validation, run audit, schema-constrained model output, tool execution, and write confirmation flow.

Target user flow:

```text
User: 根据我上传的 pdf，更新商家的目录
File: merchant_catalog.pdf
```

The backend accepts the prompt and file, sends both to a multimodal model, asks the model to extract structured catalog changes with evidence, and lets the agent call business tools to preview and then apply the update.

## Goals

- Accept prompt-plus-file agent runs.
- Support PDFs, images, Excel workbooks, and CSVs as initial file types.
- Let AI read and understand uploaded files in the first version.
- Require structured model output for extracted intent and file-derived data.
- Preserve the existing controlled tool loop: the model selects tools, but the backend executes only registered tools.
- Require preview before database-changing catalog updates.
- Store enough attachment metadata for audit and debugging.
- Keep the design upgradeable so later versions can add deterministic Excel parsing, PDF page extraction, OCR, file search, or vector storage without replacing the agent API.

## Non-Goals

- Building a long-term document knowledge base.
- Implementing backend-native PDF parsing, OCR, or Excel semantic interpretation in V1.
- Letting the model directly write database rows.
- Letting users upload arbitrary file types.
- Exposing raw uploaded file contents to CLI tools by default.
- Supporting asynchronous queues, background workers, or distributed file storage in the first implementation.
- Implementing full JSON Schema validation for every business object if the current codebase still avoids that dependency.

## External Guidance

Current agent and LLM platform guidance points toward a constrained workflow rather than an unconstrained autonomous agent:

- OpenAI's Responses API is the right API family for multimodal, tool-using agent interactions.
- OpenAI file search and vector stores are better suited to reusable or long-lived document corpora than one-off prompt attachments.
- Function/tool calling should keep side effects in application-owned tools with typed inputs and validation.
- Anthropic's agent guidance recommends starting with the simplest workflow that solves the task, then adding autonomy only where it is needed.

For this repository, the simplest useful workflow is run-scoped attachments plus AI extraction plus controlled business tools.

References:

- https://platform.openai.com/docs/guides/responses
- https://platform.openai.com/docs/guides/function-calling
- https://platform.openai.com/docs/guides/tools-file-search
- https://www.anthropic.com/research/building-effective-agents

## Existing Repository Context

The current backend has a synchronous agent run endpoint:

```json
{
  "message": "导出 Acme 合作伙伴 2026-05 报表"
}
```

The run path is:

1. `internal/httpapi` routes `POST /api/agent/runs`.
2. `internal/agent.Handler` binds a JSON body into `CreateRunRequest`.
3. `internal/agent.Service` starts an audited run.
4. `internal/agent.OpenAIPlanner` builds a text JSON prompt and calls OpenAI.
5. The planner returns either `call_tool` or `final_answer`.
6. The service executes registered CLI tools and stores redacted step audit.

There is no current attachment model, multipart handler, file storage, or file parsing layer.

## Required Boundary Change

The current `message string` request shape is too narrow. Since the project is still early and refactor cost is low, the run input should become a richer domain object instead of adding ad hoc fields to planner prompts.

### Agent input

`internal/agent` should own the run input contract:

```go
type CreateRunRequest struct {
	Message     string       `json:"message"`
	Attachments []Attachment `json:"attachments,omitempty"`
}
```

`Attachment` is run-scoped context that the planner can see:

```go
type Attachment struct {
	ID       string `json:"id"`
	Filename string `json:"filename"`
	MIMEType string `json:"mime_type"`
	Kind     string `json:"kind"`
	Size     int64  `json:"size"`
	FileID   string `json:"file_id,omitempty"`
}
```

`FileID` represents the model-provider file handle when the backend uploads or passes the file to OpenAI. If implementation chooses direct inline file input for small files, this can be omitted or adapted behind the planner.

### HTTP input

The API should support `multipart/form-data` for direct file submission:

```text
POST /api/agent/runs
Content-Type: multipart/form-data

message=根据我上传的 pdf，更新商家的目录
files[]=merchant_catalog.pdf
```

JSON requests remain useful for API clients that have already created attachment records:

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
      "file_id": "file_abc"
    }
  ]
}
```

## V1 File Understanding Strategy

V1 delegates file understanding to AI.

The backend does:

1. Validate file count, size, MIME type, and extension.
2. Create attachment metadata.
3. Pass the user message and file references to the model.
4. Ask the model to produce structured extraction before calling write-related tools.
5. Validate that planner actions are syntactically valid.
6. Execute only registered tools.
7. Require preview before applying catalog updates.

The AI does:

1. Read PDFs, images, Excel workbooks, and CSVs.
2. Infer the user's intent.
3. Extract business data such as catalog items, prices, categories, and merchant names.
4. Include evidence such as filename, page number, sheet name, row number, or source text when available.
5. Decide which registered tool to call next.

This keeps V1 fast to implement while leaving clear future upgrade points for deterministic parsers.

## Planner Contract

The planner prompt should include:

- global business instructions;
- enabled tool descriptions and schemas;
- user message;
- attachment metadata;
- model-visible file references;
- previous observations;
- an output contract requiring exactly one JSON action.

The action contract remains:

```json
{
  "type": "call_tool",
  "tool": "preview_catalog_update",
  "inputs": {
    "merchant_name": "某某商家",
    "changes": [
      {
        "action": "update",
        "item_name": "牛肉饭",
        "price": 28,
        "currency": "CNY",
        "category": "主食",
        "evidence": {
          "filename": "merchant_catalog.pdf",
          "page": 2,
          "text": "牛肉饭 28元"
        }
      }
    ]
  }
}
```

For image evidence, `page` can be omitted:

```json
{
  "filename": "menu-photo.jpg",
  "text": "牛肉饭 28元"
}
```

For spreadsheet evidence:

```json
{
  "filename": "merchant_catalog.xlsx",
  "sheet": "菜单",
  "row": 12,
  "text": "牛肉饭 | 主食 | 28"
}
```

## Catalog Update Flow

Database-changing work must use a two-step tool flow.

### Preview

The agent first calls a registered preview tool:

```json
{
  "tool": "preview_catalog_update",
  "inputs": {
    "merchant_name": "某某商家",
    "changes": [
      {
        "action": "update",
        "item_name": "牛肉饭",
        "price": 28,
        "currency": "CNY",
        "category": "主食",
        "evidence": {
          "filename": "merchant_catalog.pdf",
          "page": 2,
          "text": "牛肉饭 28元"
        }
      }
    ]
  }
}
```

The preview tool returns a durable preview ID and a user-facing diff:

```json
{
  "preview_id": "preview_123",
  "changes": [
    {
      "item_name": "牛肉饭",
      "old_price": 25,
      "new_price": 28,
      "status": "will_update"
    }
  ]
}
```

### Apply

The final write uses the preview ID:

```json
{
  "tool": "apply_catalog_update",
  "inputs": {
    "preview_id": "preview_123"
  }
}
```

The backend or business tool must not accept arbitrary AI-generated write payloads for the apply step. It should apply the previously previewed change set.

## Confirmation Policy

V1 should support an explicit confirmation boundary.

The first implementation can represent confirmation in one of two ways:

1. Return the preview to the caller and stop the run with a final answer that asks for confirmation.
2. Add a follow-up confirmation endpoint that applies a `preview_id`.

The preferred API shape is a follow-up endpoint because it keeps side effects out of conversational ambiguity:

```text
POST /api/agent/runs/{run_id}/confirm
```

Request:

```json
{
  "preview_id": "preview_123"
}
```

If the repository wants to keep only one agent endpoint in the first slice, the alternative is a second agent run message such as `确认更新 preview_123`. That is faster but weaker because confirmation is parsed through natural language.

## Attachment Storage

V1 should store metadata for each uploaded file:

- ID;
- run ID;
- original filename;
- MIME type;
- file kind;
- byte size;
- provider file ID, if used;
- created timestamp.

The first implementation does not need to persist raw file bytes in the database. It may use provider-hosted file inputs, local temporary storage, or local durable storage depending on implementation constraints.

If raw files are stored locally, the storage path must not be exposed to the model or external clients. Tools should receive attachment IDs or structured extracted data, not filesystem paths, unless a specific trusted tool is designed to read files.

## File Validation

V1 should allow only:

- `application/pdf`;
- `image/png`;
- `image/jpeg`;
- `application/vnd.openxmlformats-officedocument.spreadsheetml.sheet`;
- `text/csv`.

The first implementation should enforce configurable limits:

- maximum file count per run;
- maximum bytes per file;
- maximum total bytes per run.

These should be config values with conservative defaults.

## Error Handling

Invalid upload requests return `400`:

- missing message;
- unsupported file type;
- file too large;
- too many files;
- invalid multipart form.

Provider upload or model-read failures fail the run with a redacted error summary.

Planner action validation failures should continue to use the existing failed-run behavior.

Preview tool failures should be recorded as failed steps and returned in the agent response.

Apply failures should never mark unconfirmed preview changes as applied.

## Security And Audit

The backend must not treat model-extracted data as trusted.

V1 guardrails:

- Registered tools remain the only way to mutate business systems.
- Apply tools use a preview ID rather than a raw model-generated payload.
- Attachment metadata is stored for audit.
- Planner outputs and tool inputs are redacted using existing redaction helpers where appropriate.
- File names must be treated as user input and never interpolated into shell commands.
- The model must never choose a command path or file path.

## Future Upgrades

The V1 contract should leave room for these improvements:

- backend-native Excel parsing to preserve rows, columns, sheet names, and formulas;
- PDF page text extraction for better evidence and lower token cost;
- OCR fallback for scanned PDFs;
- vector storage for long-lived merchant documents;
- async run processing for large files;
- confidence scoring and human review thresholds;
- stronger JSON Schema validation dependency if business schemas become complex;
- per-tool authorization policies for high-risk writes.

## Approval Status

The selected V1 direction is:

```text
AI reads and standardizes files in the initial version.
Backend controls uploads, schemas, tools, preview, confirmation, and audit.
```

This design reflects the confirmed user preference for speed and flexibility in the initial product stage.
