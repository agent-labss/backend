package agent

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	"ai/backend/internal/database"
	"ai/backend/internal/database/generated"
)

func (repository Repository) CreateChatSession(ctx context.Context, record CreateChatSessionRecord) (ChatSession, error) {
	if repository.database == nil {
		return ChatSession{}, ErrDatabaseMissing
	}

	now := time.Now().UTC()
	session := ChatSession{
		ID:        newRuntimeID("chat"),
		Title:     RedactText(record.Title),
		Status:    ChatSessionStatusOpen,
		CreatedAt: now,
		UpdatedAt: now,
	}
	sessionRecord := database.ChatSession{
		ID:        session.ID,
		Title:     session.Title,
		Status:    string(session.Status),
		CreatedAt: session.CreatedAt,
		UpdatedAt: session.UpdatedAt,
	}
	if err := generated.ChatSessionQueries[database.ChatSession](repository.database).Create(ctx, &sessionRecord); err != nil {
		return ChatSession{}, fmt.Errorf("create chat session: %w", err)
	}

	return session, nil
}

func (repository Repository) GetChatSession(ctx context.Context, sessionID string) (ChatSession, error) {
	if repository.database == nil {
		return ChatSession{}, ErrDatabaseMissing
	}

	record, err := generated.ChatSessionQueries[database.ChatSession](repository.database).GetByID(ctx, sessionID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ChatSession{}, ErrChatSessionNotFound
		}
		return ChatSession{}, fmt.Errorf("get chat session: %w", err)
	}
	if record.ID == "" {
		return ChatSession{}, ErrChatSessionNotFound
	}

	return chatSessionFromRecord(record), nil
}

func (repository Repository) ListChatMessages(ctx context.Context, sessionID string) ([]ChatMessage, error) {
	if repository.database == nil {
		return nil, ErrDatabaseMissing
	}

	records, err := generated.ChatMessageQueries[database.ChatMessage](repository.database).ListBySessionID(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("list chat messages: %w", err)
	}
	attachments, err := repository.chatAttachments(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	messages := make([]ChatMessage, 0, len(records))
	for _, record := range records {
		message := chatMessageFromRecord(record)
		message.Attachments = attachmentMetadataList(attachments[record.ID])
		messages = append(messages, message)
	}
	return messages, nil
}

func (repository Repository) CreateChatMessage(ctx context.Context, record CreateChatMessageRecord) (ChatMessage, error) {
	if repository.database == nil {
		return ChatMessage{}, ErrDatabaseMissing
	}

	now := time.Now().UTC()
	var message ChatMessage
	if err := repository.database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		messageQueries := generated.ChatMessageQueries[database.ChatMessage](tx)
		maxSequence, err := messageQueries.MaxSequenceBySessionID(ctx, record.SessionID)
		if err != nil {
			return fmt.Errorf("next chat message sequence: %w", err)
		}
		message = ChatMessage{
			ID:          newRuntimeID("msg"),
			SessionID:   record.SessionID,
			ExecutionID: record.ExecutionID,
			Role:        record.Role,
			Content:     RedactText(record.Content),
			Status:      ChatMessageStatusCompleted,
			Sequence:    maxSequence + 1,
			Attachments: attachmentMetadataList(record.Attachments),
			CreatedAt:   now,
			CompletedAt: now,
		}
		messageRecord := database.ChatMessage{
			ID:          message.ID,
			SessionID:   message.SessionID,
			ExecutionID: message.ExecutionID,
			Role:        string(message.Role),
			Content:     message.Content,
			Status:      string(message.Status),
			Sequence:    message.Sequence,
			CreatedAt:   message.CreatedAt,
			CompletedAt: sql.NullTime{Time: message.CompletedAt, Valid: true},
		}
		if err := messageQueries.Create(ctx, &messageRecord); err != nil {
			return fmt.Errorf("create chat message: %w", err)
		}
		if err := saveChatAttachments(ctx, tx, message, record.Attachments); err != nil {
			return err
		}
		return generated.ChatSessionQueries[database.ChatSession](tx).UpdateUpdatedAtByID(ctx, now, record.SessionID)
	}); err != nil {
		return ChatMessage{}, fmt.Errorf("create chat message transaction: %w", err)
	}

	return message, nil
}

func saveChatAttachments(ctx context.Context, db *gorm.DB, message ChatMessage, attachments []Attachment) error {
	for _, attachment := range attachments {
		record := database.ChatAttachment{
			ID:             attachment.ID,
			SessionID:      message.SessionID,
			MessageID:      message.ID,
			Filename:       RedactText(attachment.Filename),
			MIMEType:       attachment.MIMEType,
			Kind:           string(attachment.Kind),
			SizeBytes:      attachment.Size,
			ProviderFileID: attachment.FileID,
			CreatedAt:      time.Now().UTC(),
		}
		if err := generated.ChatAttachmentQueries[database.ChatAttachment](db).Create(ctx, &record); err != nil {
			return fmt.Errorf("save chat attachment: %w", err)
		}
	}

	return nil
}

func (repository Repository) chatAttachments(ctx context.Context, sessionID string) (map[string][]Attachment, error) {
	records, err := generated.ChatAttachmentQueries[database.ChatAttachment](repository.database).ListBySessionID(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("list chat attachments: %w", err)
	}
	attachments := make(map[string][]Attachment)
	for _, record := range records {
		attachments[record.MessageID] = append(attachments[record.MessageID], Attachment{
			ID:       record.ID,
			Filename: record.Filename,
			MIMEType: record.MIMEType,
			Kind:     AttachmentKind(record.Kind),
			Size:     record.SizeBytes,
			FileID:   record.ProviderFileID,
		})
	}
	return attachments, nil
}

func chatSessionFromRecord(record database.ChatSession) ChatSession {
	return ChatSession{
		ID:        record.ID,
		Title:     record.Title,
		Status:    ChatSessionStatus(record.Status),
		CreatedAt: record.CreatedAt,
		UpdatedAt: record.UpdatedAt,
	}
}

func chatMessageFromRecord(record database.ChatMessage) ChatMessage {
	message := ChatMessage{
		ID:          record.ID,
		SessionID:   record.SessionID,
		ExecutionID: record.ExecutionID,
		Role:        ChatMessageRole(record.Role),
		Content:     record.Content,
		Status:      ChatMessageStatus(record.Status),
		Sequence:    record.Sequence,
		CreatedAt:   record.CreatedAt,
		Error:       record.ErrorSummary,
	}
	if record.CompletedAt.Valid {
		message.CompletedAt = record.CompletedAt.Time
	}
	return message
}
