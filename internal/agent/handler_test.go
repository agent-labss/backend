package agent

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
)

type fakeChatService struct {
	chat        ChatSession
	messages    []ChatMessage
	chatMessage SubmitChatMessageResponse
	err         error
	called      bool
	chatRequest CreateChatMessageRequest
	chatID      string
}

type fakeChatEventService struct {
	subscribeChatEvents func(ctx context.Context, chatID string) (<-chan ChatEvent, func(), error)
}

func (service fakeChatEventService) SubscribeChatEvents(ctx context.Context, chatID string) (<-chan ChatEvent, func(), error) {
	return service.subscribeChatEvents(ctx, chatID)
}

func (service *fakeChatService) CreateChat(ctx context.Context, request CreateChatRequest) (ChatSession, error) {
	_ = ctx
	service.chat = ChatSession{ID: "chat_test", Title: request.Title, Status: ChatSessionStatusOpen}
	return service.chat, service.err
}

func (service *fakeChatService) GetChat(_ context.Context, chatID string) (ChatSession, error) {
	service.chatID = chatID
	return service.chat, service.err
}

func (service *fakeChatService) ListChatMessages(_ context.Context, chatID string) ([]ChatMessage, error) {
	service.chatID = chatID
	return service.messages, service.err
}

func (service *fakeChatService) CreateChatMessage(_ context.Context, chatID string, request CreateChatMessageRequest) (SubmitChatMessageResponse, error) {
	service.called = true
	service.chatID = chatID
	service.chatRequest = request
	return service.chatMessage, service.err
}

func TestHandlerCreateChatReturnsSession(t *testing.T) {
	service := &fakeChatService{}
	handler := NewHandler(service, service, nil)
	app := fiber.New()
	app.Post(ChatSessionsPath, handler.CreateChat)

	req, err := http.NewRequest(http.MethodPost, ChatSessionsPath, bytes.NewReader([]byte(`{"title":"Reports"}`)))
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Test() error = %v", err)
	}
	defer closeAgentResponseBody(t, resp)

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("StatusCode = %d, want %d", resp.StatusCode, http.StatusCreated)
	}
	var body ChatSession
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if body.ID != "chat_test" || body.Title != "Reports" {
		t.Fatalf("body = %#v, want created chat", body)
	}
}

func TestHandlerCreateChatMessageAcceptsJSON(t *testing.T) {
	service := &fakeChatService{chatMessage: SubmitChatMessageResponse{
		ChatID:      "chat_test",
		UserMessage: ChatMessage{ID: "msg_user", SessionID: "chat_test", Role: ChatMessageRoleUser, Content: "hi"},
		ExecutionID: testExecutionID,
		Status:      AgentExecutionStatusRunning,
	}}
	resp := testCreateChatMessageRequest(t, service, []byte(`{"message":"hi"}`))
	defer closeAgentResponseBody(t, resp)

	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("StatusCode = %d, want %d", resp.StatusCode, http.StatusAccepted)
	}
	if service.chatID != "chat_test" || service.chatRequest.Message != "hi" {
		t.Fatalf("chat message call = %q %#v, want chat_test hi", service.chatID, service.chatRequest)
	}
	var body SubmitChatMessageResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if body.ExecutionID != testExecutionID || body.Status != AgentExecutionStatusRunning || body.UserMessage.ID != "msg_user" {
		t.Fatalf("body = %#v, want async submit response", body)
	}
	requireJSONExecutionID(t, body)
}

func TestHandlerCreateChatMessageReturnsConflictForActiveExecution(t *testing.T) {
	service := &fakeChatService{err: ErrAgentExecutionActive}
	resp := testCreateChatMessageRequest(t, service, []byte(`{"message":"second request"}`))
	defer closeAgentResponseBody(t, resp)

	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("StatusCode = %d, want %d", resp.StatusCode, http.StatusConflict)
	}
}

func TestHandlerGetChatReturnsSession(t *testing.T) {
	service := &fakeChatService{chat: ChatSession{ID: "chat_test", Title: "Reports", Status: ChatSessionStatusOpen}}
	handler := NewHandler(service, service, nil)
	app := fiber.New()
	app.Get(ChatSessionPath, handler.GetChat)

	req, err := http.NewRequest(http.MethodGet, ChatSessionsPath+"/chat_test", nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Test() error = %v", err)
	}
	defer closeAgentResponseBody(t, resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("StatusCode = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if service.chatID != "chat_test" {
		t.Fatalf("chatID = %q, want chat_test", service.chatID)
	}
}

func TestHandlerListChatMessagesReturnsMessages(t *testing.T) {
	service := &fakeChatService{messages: []ChatMessage{{ID: "msg_1", SessionID: "chat_test", Role: ChatMessageRoleUser, Content: "hi"}}}
	handler := NewHandler(service, service, nil)
	app := fiber.New()
	app.Get(ChatMessagesPath, handler.ListChatMessages)

	req, err := http.NewRequest(http.MethodGet, ChatSessionsPath+"/chat_test/messages", nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Test() error = %v", err)
	}
	defer closeAgentResponseBody(t, resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("StatusCode = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if service.chatID != "chat_test" {
		t.Fatalf("chatID = %q, want chat_test", service.chatID)
	}
}

func TestHandlerSubscribeChatEventsStreamsConnectedAndEvents(t *testing.T) {
	events := make(chan ChatEvent, 2)
	events <- ChatEvent{Type: EventTypeExecutionStarted, ChatID: "chat_1", ExecutionID: "exec_1"}
	close(events)
	handler := NewHandler(nil, nil, fakeChatEventService{
		subscribeChatEvents: func(_ context.Context, chatID string) (<-chan ChatEvent, func(), error) {
			if chatID != "chat_1" {
				t.Fatalf("chatID = %q, want chat_1", chatID)
			}
			return events, func() {}, nil
		},
	})
	app := fiber.New()
	app.Get(ChatEventsPath, handler.SubscribeChatEvents)

	request := httptest.NewRequest(http.MethodGet, "/api/chats/chat_1/events", nil)
	response, err := app.Test(request)
	if err != nil {
		t.Fatalf("app.Test() error = %v", err)
	}
	defer closeAgentResponseBody(t, response)

	if contentType := response.Header.Get("Content-Type"); !strings.HasPrefix(contentType, "text/event-stream") {
		t.Fatalf("Content-Type = %q, want text/event-stream", contentType)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	text := string(body)
	if !strings.Contains(text, "event: connected") {
		t.Fatalf("body = %q, want connected event", text)
	}
	requireSSEExecutionStarted(t, text)
}

func TestHandlerCreateChatMessageAcceptsMultipartFiles(t *testing.T) {
	service := &fakeChatService{chatMessage: SubmitChatMessageResponse{ChatID: "chat_test", ExecutionID: testExecutionID, Status: AgentExecutionStatusRunning}}
	resp := performMultipartCreateChatMessage(t, service, "update catalog", "merchant_catalog.pdf", "application/pdf", []byte("%PDF-1.7"))
	defer closeAgentResponseBody(t, resp)

	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("StatusCode = %d, want %d", resp.StatusCode, http.StatusAccepted)
	}
	requireMultipartPDFRequest(t, service.chatRequest)
}

func TestHandlerCreateChatMessageAllowsAttachmentWithoutMessage(t *testing.T) {
	service := &fakeChatService{chatMessage: SubmitChatMessageResponse{ChatID: "chat_test", ExecutionID: testExecutionID, Status: AgentExecutionStatusRunning}}
	resp := performMultipartCreateChatMessage(t, service, "", "accounts.csv", "text/csv", []byte("a,b\n"))
	defer closeAgentResponseBody(t, resp)

	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("StatusCode = %d, want %d", resp.StatusCode, http.StatusAccepted)
	}
	if len(service.chatRequest.Attachments) != 1 {
		t.Fatalf("len(Attachments) = %d, want 1", len(service.chatRequest.Attachments))
	}
}

func TestHandlerCreateChatMessageRejectsBlankMessageWithoutAttachments(t *testing.T) {
	service := &fakeChatService{}
	resp := testCreateChatMessageRequest(t, service, []byte(`{"message":"   "}`))
	defer closeAgentResponseBody(t, resp)

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("StatusCode = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
	if service.called {
		t.Fatal("CreateChatMessage() called for blank message without attachments, want no call")
	}
}

func TestHandlerCreateChatMessageRejectsBadJSON(t *testing.T) {
	service := &fakeChatService{}
	resp := testCreateChatMessageRequest(t, service, []byte(`{`))
	defer closeAgentResponseBody(t, resp)

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("StatusCode = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
	if service.called {
		t.Fatal("CreateChatMessage() called for bad JSON, want no call")
	}
}

func TestHandlerCreateChatMessageRejectsUnsupportedMultipartFile(t *testing.T) {
	service := &fakeChatService{}
	resp := performMultipartCreateChatMessage(t, service, "update catalog", "script.sh", "text/x-shellscript", []byte("echo nope"))
	defer closeAgentResponseBody(t, resp)

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("StatusCode = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
	if service.called {
		t.Fatal("CreateChatMessage() called for unsupported file, want no call")
	}
}

func TestHandlerCreateChatMessageAcceptsValidatedJSONAttachments(t *testing.T) {
	service := &fakeChatService{chatMessage: SubmitChatMessageResponse{ChatID: "chat_test", ExecutionID: testExecutionID, Status: AgentExecutionStatusRunning}}
	body := []byte(`{
		"message":"update catalog",
		"attachments":[{
			"filename":"merchant_catalog.pdf",
			"mime_type":"application/pdf",
			"size":8,
			"data":"` + base64.StdEncoding.EncodeToString([]byte("%PDF-1.7")) + `"
		}]
	}`)

	resp := testCreateChatMessageRequestWithUploadConfig(t, service, body, UploadConfig{MaxFiles: 1, MaxFileBytes: 1024, MaxTotalBytes: 1024})
	defer closeAgentResponseBody(t, resp)

	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("StatusCode = %d, want %d", resp.StatusCode, http.StatusAccepted)
	}
	if len(service.chatRequest.Attachments) != 1 {
		t.Fatalf("len(Attachments) = %d, want 1", len(service.chatRequest.Attachments))
	}
	attachment := service.chatRequest.Attachments[0]
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

func TestHandlerCreateChatMessageRejectsOversizedJSONAttachment(t *testing.T) {
	service := &fakeChatService{}
	body := []byte(`{
		"message":"update catalog",
		"attachments":[{
			"filename":"merchant_catalog.pdf",
			"mime_type":"application/pdf",
			"size":8,
			"data":"` + base64.StdEncoding.EncodeToString([]byte("%PDF-1.7")) + `"
		}]
	}`)

	resp := testCreateChatMessageRequestWithUploadConfig(t, service, body, UploadConfig{MaxFiles: 1, MaxFileBytes: 2, MaxTotalBytes: 1024})
	defer closeAgentResponseBody(t, resp)

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("StatusCode = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
	if service.called {
		t.Fatal("CreateChatMessage() called for oversized JSON attachment, want no call")
	}
}

func TestHandlerCreateChatMessageRejectsJSONAttachmentFileID(t *testing.T) {
	service := &fakeChatService{}
	resp := testCreateChatMessageRequestWithUploadConfig(t, service, []byte(`{
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
		t.Fatal("CreateChatMessage() called for JSON file_id attachment, want no call")
	}
}

func requireMultipartPDFRequest(t *testing.T, request CreateChatMessageRequest) {
	t.Helper()

	if request.Message != "update catalog" {
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

func requireJSONExecutionID(t *testing.T, value any) {
	t.Helper()

	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if strings.Contains(string(encoded), "run_id") {
		t.Fatalf("JSON = %s, want no run_id", encoded)
	}
	if !strings.Contains(string(encoded), "execution_id") {
		t.Fatalf("JSON = %s, want execution_id", encoded)
	}
}

func requireSSEExecutionStarted(t *testing.T, text string) {
	t.Helper()

	if strings.Contains(text, "run_id") {
		t.Fatalf("body = %q, want no run_id", text)
	}
	if !strings.Contains(text, "event: execution.started") || !strings.Contains(text, `"execution_id":"exec_1"`) {
		t.Fatalf("body = %q, want execution.started event with execution_id", text)
	}
}

func performMultipartCreateChatMessage(t *testing.T, service *fakeChatService, message string, filename string, mimeType string, data []byte) *http.Response {
	t.Helper()

	handler := NewHandler(service, service, nil, UploadConfig{MaxFiles: 2, MaxFileBytes: 1024, MaxTotalBytes: 2048})
	app := fiber.New()
	app.Post(ChatMessagesPath, handler.CreateChatMessage)

	body, contentType := multipartRunBody(t, message, filename, mimeType, data)
	req, err := http.NewRequest(http.MethodPost, ChatSessionsPath+"/chat_test/messages", body)
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

func testCreateChatMessageRequest(t *testing.T, service *fakeChatService, body []byte) *http.Response {
	t.Helper()

	return testCreateChatMessageRequestWithUploadConfig(t, service, body, UploadConfig{MaxFiles: 5, MaxFileBytes: 10 * 1024 * 1024, MaxTotalBytes: 25 * 1024 * 1024})
}

func testCreateChatMessageRequestWithUploadConfig(t *testing.T, service *fakeChatService, body []byte, uploadConfig UploadConfig) *http.Response {
	t.Helper()

	handler := NewHandler(service, service, nil, uploadConfig)
	app := fiber.New()
	app.Post(ChatMessagesPath, handler.CreateChatMessage)

	req, err := http.NewRequest(http.MethodPost, ChatSessionsPath+"/chat_test/messages", bytes.NewReader(body))
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
