package agent

import (
	"context"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strings"

	"github.com/gofiber/fiber/v3"
)

const errorField = "error"

var (
	errInvalidJSONRequestBody      = errors.New("invalid JSON request body")
	errInvalidMultipartRequestBody = errors.New("invalid multipart request body")
	errInvalidMultipartFile        = errors.New("invalid multipart file")
)

type Runner interface {
	Run(ctx context.Context, request CreateRunRequest) (RunResponse, error)
}

type Handler struct {
	runner       Runner
	uploadConfig UploadConfig
}

func NewHandler(runner Runner, uploadConfigs ...UploadConfig) Handler {
	config := UploadConfig{MaxFiles: 5, MaxFileBytes: 10 * 1024 * 1024, MaxTotalBytes: 25 * 1024 * 1024}
	if len(uploadConfigs) > 0 {
		config = uploadConfigs[0]
	}

	return Handler{runner: runner, uploadConfig: config}
}

func (handler Handler) CreateRun(c fiber.Ctx) error {
	request, err := handler.createRunRequest(c)
	if err != nil {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{errorField: err.Error()})
	}
	if strings.TrimSpace(request.Message) == "" {
		return c.Status(http.StatusBadRequest).JSON(fiber.Map{errorField: "message is required"})
	}

	response, err := handler.runner.Run(c.Context(), request)
	if err != nil {
		return writeRunError(c, response)
	}

	return c.Status(http.StatusOK).JSON(response)
}

func (handler Handler) createRunRequest(c fiber.Ctx) (CreateRunRequest, error) {
	contentType, err := mediaType(c.Get("Content-Type"))
	if err != nil {
		return CreateRunRequest{}, err
	}
	if contentType == "multipart/form-data" {
		return handler.multipartCreateRunRequest(c)
	}

	var request CreateRunRequest
	if err := c.Bind().Body(&request); err != nil {
		return CreateRunRequest{}, errInvalidJSONRequestBody
	}
	attachments, err := normalizeJSONAttachments(request.Attachments, handler.uploadConfig)
	if err != nil {
		return CreateRunRequest{}, err
	}
	request.Attachments = attachments
	return request, nil
}

func mediaType(contentType string) (string, error) {
	if strings.TrimSpace(contentType) == "" {
		return "", nil
	}
	parsed, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return "", errInvalidJSONRequestBody
	}
	return parsed, nil
}

func (handler Handler) multipartCreateRunRequest(c fiber.Ctx) (CreateRunRequest, error) {
	form, err := c.MultipartForm()
	if err != nil {
		return CreateRunRequest{}, errInvalidMultipartRequestBody
	}

	files, err := uploadedFilesFromForm(form)
	if err != nil {
		return CreateRunRequest{}, err
	}
	attachments, err := buildAttachments(files, handler.uploadConfig)
	if err != nil {
		return CreateRunRequest{}, err
	}

	return CreateRunRequest{Message: c.FormValue("message"), Attachments: attachments}, nil
}

func uploadedFilesFromForm(form *multipart.Form) ([]UploadedFile, error) {
	headers := append([]*multipart.FileHeader{}, form.File["files[]"]...)
	headers = append(headers, form.File["files"]...)
	files := make([]UploadedFile, 0, len(headers))
	for _, header := range headers {
		file, err := header.Open()
		if err != nil {
			return nil, errInvalidMultipartFile
		}
		data, readErr := io.ReadAll(file)
		closeErr := file.Close()
		if readErr != nil {
			return nil, errInvalidMultipartFile
		}
		if closeErr != nil {
			return nil, errInvalidMultipartFile
		}
		mimeType := header.Header.Get("Content-Type")
		if strings.TrimSpace(mimeType) == "" {
			mimeType = http.DetectContentType(data)
		}
		files = append(files, UploadedFile{Filename: header.Filename, MIMEType: mimeType, Data: data})
	}

	return files, nil
}

func writeRunError(c fiber.Ctx, response RunResponse) error {
	if response.Status == RunStatusFailed {
		return c.Status(http.StatusBadRequest).JSON(response)
	}

	return c.Status(http.StatusInternalServerError).JSON(fiber.Map{errorField: "agent run failed"})
}
