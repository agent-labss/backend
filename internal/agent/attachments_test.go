package agent

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

func TestBuildAttachmentAcceptsPDF(t *testing.T) {
	config := UploadConfig{MaxFiles: 2, MaxFileBytes: 1024, MaxTotalBytes: 2048}
	attachment, err := buildAttachment(UploadedFile{
		Filename: "merchant_catalog.pdf",
		MIMEType: "application/pdf",
		Data:     []byte("%PDF-1.7"),
	}, config)
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

func TestBuildAttachmentAcceptsSupportedImagesAndSheets(t *testing.T) {
	tests := []struct {
		name     string
		file     UploadedFile
		wantKind AttachmentKind
	}{
		{
			name:     "png",
			file:     UploadedFile{Filename: "menu.png", MIMEType: "image/png", Data: []byte("png")},
			wantKind: AttachmentKindImage,
		},
		{
			name:     "jpeg",
			file:     UploadedFile{Filename: "menu.jpeg", MIMEType: "image/jpeg", Data: []byte("jpeg")},
			wantKind: AttachmentKindImage,
		},
		{
			name:     "xlsx",
			file:     UploadedFile{Filename: "catalog.xlsx", MIMEType: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", Data: []byte("xlsx")},
			wantKind: AttachmentKindSpreadsheet,
		},
		{
			name:     "csv",
			file:     UploadedFile{Filename: "catalog.csv", MIMEType: "text/csv", Data: []byte("csv")},
			wantKind: AttachmentKindCSV,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			attachment, err := buildAttachment(test.file, UploadConfig{MaxFiles: 4, MaxFileBytes: 1024, MaxTotalBytes: 4096})
			if err != nil {
				t.Fatalf("buildAttachment() error = %v", err)
			}
			if attachment.Kind != test.wantKind {
				t.Fatalf("Kind = %q, want %q", attachment.Kind, test.wantKind)
			}
		})
	}
}

func TestBuildAttachmentRejectsUnsupportedType(t *testing.T) {
	config := UploadConfig{MaxFiles: 2, MaxFileBytes: 1024, MaxTotalBytes: 2048}
	_, err := buildAttachment(UploadedFile{
		Filename: "script.sh",
		MIMEType: "text/x-shellscript",
		Data:     []byte("echo nope"),
	}, config)
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
	data, ok := view["data"].(string)
	if !ok {
		t.Fatalf("data = %T, want string", view["data"])
	}
	if strings.Contains(data, "%PDF") {
		t.Fatalf("prompt view leaked raw data: %#v", view)
	}
}
