package agent

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"testing"

	"github.com/gofiber/fiber/v3"
)

type fakeRunService struct {
	response RunResponse
	err      error
	called   bool
	request  CreateRunRequest
}

func (service *fakeRunService) Run(_ context.Context, request CreateRunRequest) (RunResponse, error) {
	service.called = true
	service.request = request
	return service.response, service.err
}

func TestHandlerCreateRunReturnsResponse(t *testing.T) {
	service := &fakeRunService{response: RunResponse{RunID: testRunID, Status: RunStatusSucceeded, Answer: "done"}}
	handler := NewHandler(service)
	app := fiber.New()
	app.Post(AgentRunsPath, handler.CreateRun)

	req, err := http.NewRequest(http.MethodPost, AgentRunsPath, bytes.NewReader([]byte(`{"message":"export report"}`)))
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Test() error = %v", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Fatalf("Body.Close() error = %v", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("StatusCode = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var body RunResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if body.RunID != testRunID {
		t.Fatalf("RunID = %q, want %s", body.RunID, testRunID)
	}
	if !service.called {
		t.Fatal("Run() was not called")
	}
}

func TestHandlerCreateRunAcceptsMultipartFiles(t *testing.T) {
	service := &fakeRunService{response: RunResponse{RunID: testRunID, Status: RunStatusSucceeded, Answer: "done"}}
	resp := performMultipartCreateRun(t, service, "根据我上传的 pdf，更新商家的目录", "merchant_catalog.pdf", "application/pdf", []byte("%PDF-1.7"))
	defer closeAgentResponseBody(t, resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("StatusCode = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	requireMultipartPDFRequest(t, service.request)
}

func requireMultipartPDFRequest(t *testing.T, request CreateRunRequest) {
	t.Helper()

	if request.Message != "根据我上传的 pdf，更新商家的目录" {
		t.Fatalf("Message = %q, want uploaded message", request.Message)
	}
	if len(request.Attachments) != 1 {
		t.Fatalf("len(Attachments) = %d, want 1", len(request.Attachments))
	}
	attachment := request.Attachments[0]
	if attachment.Filename != "merchant_catalog.pdf" {
		t.Fatalf("Filename = %q, want merchant_catalog.pdf", attachment.Filename)
	}
	if attachment.Kind != AttachmentKindPDF {
		t.Fatalf("Kind = %q, want %q", attachment.Kind, AttachmentKindPDF)
	}
	if attachment.Data == "" {
		t.Fatal("Data is empty, want base64 file bytes")
	}
}

func TestHandlerCreateRunRejectsUnsupportedMultipartFile(t *testing.T) {
	service := &fakeRunService{}
	resp := performMultipartCreateRun(t, service, "update catalog", "script.sh", "text/x-shellscript", []byte("echo nope"))
	defer closeAgentResponseBody(t, resp)

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("StatusCode = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
	if service.called {
		t.Fatal("Run() called for unsupported file, want no call")
	}
}

func TestHandlerCreateRunAcceptsValidatedJSONAttachments(t *testing.T) {
	service := &fakeRunService{response: RunResponse{RunID: testRunID, Status: RunStatusSucceeded, Answer: "done"}}
	body := []byte(`{
		"message":"update catalog",
		"attachments":[{
			"filename":"merchant_catalog.pdf",
			"mime_type":"application/pdf",
			"size":8,
			"data":"` + base64.StdEncoding.EncodeToString([]byte("%PDF-1.7")) + `"
		}]
	}`)

	resp := testCreateRunRequestWithUploadConfig(t, service, body, UploadConfig{MaxFiles: 1, MaxFileBytes: 1024, MaxTotalBytes: 1024})
	defer closeAgentResponseBody(t, resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("StatusCode = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if len(service.request.Attachments) != 1 {
		t.Fatalf("len(Attachments) = %d, want 1", len(service.request.Attachments))
	}
	attachment := service.request.Attachments[0]
	if attachment.Kind != AttachmentKindPDF {
		t.Fatalf("Kind = %q, want %q", attachment.Kind, AttachmentKindPDF)
	}
	if attachment.Size != 8 {
		t.Fatalf("Size = %d, want 8", attachment.Size)
	}
	if attachment.ID == "" {
		t.Fatal("ID is empty, want generated attachment ID")
	}
}

func TestHandlerCreateRunRejectsOversizedJSONAttachment(t *testing.T) {
	service := &fakeRunService{}
	body := []byte(`{
		"message":"update catalog",
		"attachments":[{
			"filename":"merchant_catalog.pdf",
			"mime_type":"application/pdf",
			"size":8,
			"data":"` + base64.StdEncoding.EncodeToString([]byte("%PDF-1.7")) + `"
		}]
	}`)

	resp := testCreateRunRequestWithUploadConfig(t, service, body, UploadConfig{MaxFiles: 1, MaxFileBytes: 2, MaxTotalBytes: 1024})
	defer closeAgentResponseBody(t, resp)

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("StatusCode = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
	if service.called {
		t.Fatal("Run() called for oversized JSON attachment, want no call")
	}
}

func TestHandlerCreateRunRejectsJSONAttachmentFileID(t *testing.T) {
	service := &fakeRunService{}
	resp := testCreateRunRequestWithUploadConfig(t, service, []byte(`{
		"message":"update catalog",
		"attachments":[{
			"filename":"merchant_catalog.pdf",
			"mime_type":"application/pdf",
			"kind":"pdf",
			"size":8,
			"file_id":"file_123"
		}]
	}`), UploadConfig{MaxFiles: 1, MaxFileBytes: 1024, MaxTotalBytes: 1024})
	defer closeAgentResponseBody(t, resp)

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("StatusCode = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
	if service.called {
		t.Fatal("Run() called for JSON file_id attachment, want no call")
	}
}

func performMultipartCreateRun(t *testing.T, service *fakeRunService, message string, filename string, mimeType string, data []byte) *http.Response {
	t.Helper()

	handler := NewHandler(service, UploadConfig{MaxFiles: 2, MaxFileBytes: 1024, MaxTotalBytes: 2048})
	app := fiber.New()
	app.Post(AgentRunsPath, handler.CreateRun)

	body, contentType := multipartRunBody(t, message, filename, mimeType, data)
	req, err := http.NewRequest(http.MethodPost, AgentRunsPath, body)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	req.Header.Set("Content-Type", contentType)

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Test() error = %v", err)
	}

	return resp
}

func TestHandlerCreateRunRejectsBadJSON(t *testing.T) {
	service := &fakeRunService{}
	resp := testCreateRunRequest(t, service, []byte(`{`))
	defer closeAgentResponseBody(t, resp)

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("StatusCode = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
	if service.called {
		t.Fatal("Run() called for bad JSON, want no call")
	}
}

func TestHandlerCreateRunRejectsBlankMessage(t *testing.T) {
	service := &fakeRunService{}
	resp := testCreateRunRequest(t, service, []byte(`{"message":"   "}`))
	defer closeAgentResponseBody(t, resp)

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("StatusCode = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
	if service.called {
		t.Fatal("Run() called for blank message, want no call")
	}
}

func testCreateRunRequest(t *testing.T, service *fakeRunService, body []byte) *http.Response {
	t.Helper()

	return testCreateRunRequestWithUploadConfig(t, service, body, UploadConfig{MaxFiles: 5, MaxFileBytes: 10 * 1024 * 1024, MaxTotalBytes: 25 * 1024 * 1024})
}

func testCreateRunRequestWithUploadConfig(t *testing.T, service *fakeRunService, body []byte, uploadConfig UploadConfig) *http.Response {
	t.Helper()

	handler := NewHandler(service, uploadConfig)
	app := fiber.New()
	app.Post(AgentRunsPath, handler.CreateRun)

	req, err := http.NewRequest(http.MethodPost, AgentRunsPath, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Test() error = %v", err)
	}

	return resp
}

func multipartRunBody(t *testing.T, message string, filename string, mimeType string, data []byte) (*bytes.Buffer, string) {
	t.Helper()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("message", message); err != nil {
		t.Fatalf("WriteField(message) error = %v", err)
	}

	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", `form-data; name="files[]"; filename="`+filename+`"`)
	header.Set("Content-Type", mimeType)
	part, err := writer.CreatePart(header)
	if err != nil {
		t.Fatalf("CreatePart() error = %v", err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatalf("Write(file) error = %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close multipart writer error = %v", err)
	}

	return &body, writer.FormDataContentType()
}

func closeAgentResponseBody(t *testing.T, resp *http.Response) {
	t.Helper()

	if err := resp.Body.Close(); err != nil {
		t.Fatalf("Body.Close() error = %v", err)
	}
}
