package agent

import (
	"encoding/base64"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

var (
	ErrTooManyAttachments        = errors.New("too many attachments")
	ErrAttachmentTooLarge        = errors.New("attachment too large")
	ErrUnsupportedAttachmentType = errors.New("unsupported attachment type")
	ErrInvalidAttachmentData     = errors.New("invalid attachment data")
	ErrUnsupportedAttachmentRef  = errors.New("unsupported attachment reference")
)

var supportedAttachmentTypes = map[string]AttachmentKind{
	"application/pdf|.pdf": AttachmentKindPDF,
	"image/png|.png":       AttachmentKindImage,
	"image/jpeg|.jpg":      AttachmentKindImage,
	"image/jpeg|.jpeg":     AttachmentKindImage,
	"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet|.xlsx": AttachmentKindSpreadsheet,
	"text/csv|.csv": AttachmentKindCSV,
}

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

func normalizeJSONAttachments(attachments []Attachment, config UploadConfig) ([]Attachment, error) {
	if len(attachments) == 0 {
		return nil, nil
	}
	if len(attachments) > config.MaxFiles {
		return nil, ErrTooManyAttachments
	}

	files := make([]UploadedFile, 0, len(attachments))
	for _, attachment := range attachments {
		file, err := uploadedFileFromAttachment(attachment)
		if err != nil {
			return nil, err
		}
		files = append(files, file)
	}

	return buildAttachments(files, config)
}

func uploadedFileFromAttachment(attachment Attachment) (UploadedFile, error) {
	if strings.TrimSpace(attachment.FileID) != "" {
		return UploadedFile{}, ErrUnsupportedAttachmentRef
	}
	if strings.TrimSpace(attachment.Data) == "" {
		return UploadedFile{}, ErrInvalidAttachmentData
	}
	data, err := base64.StdEncoding.DecodeString(attachment.Data)
	if err != nil {
		return UploadedFile{}, fmt.Errorf("%w: %w", ErrInvalidAttachmentData, err)
	}

	return UploadedFile{
		Filename: attachment.Filename,
		MIMEType: attachment.MIMEType,
		Data:     data,
	}, nil
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
	if kind, ok := supportedAttachmentTypes[normalizedMIME+"|"+extension]; ok {
		return kind, nil
	}

	return "", fmt.Errorf("%w: %s %s", ErrUnsupportedAttachmentType, mimeType, extension)
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

func attachmentMetadata(attachment Attachment) Attachment {
	attachment.Data = ""
	attachment.FileID = ""
	return attachment
}

func attachmentMetadataList(attachments []Attachment) []Attachment {
	if len(attachments) == 0 {
		return nil
	}
	metadata := make([]Attachment, 0, len(attachments))
	for _, attachment := range attachments {
		metadata = append(metadata, attachmentMetadata(attachment))
	}
	return metadata
}
