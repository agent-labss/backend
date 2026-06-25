package database

import (
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

var (
	ErrInvalidJSON     = errors.New("invalid JSON")
	ErrUnsupportedJSON = errors.New("unsupported JSON source")
)

type JSON []byte

func (value JSON) Value() (driver.Value, error) {
	if len(value) == 0 {
		return "{}", nil
	}
	if !json.Valid(value) {
		return nil, ErrInvalidJSON
	}
	return string(value), nil
}

func (value *JSON) Scan(raw any) error {
	switch typed := raw.(type) {
	case nil:
		*value = nil
	case []byte:
		*value = append((*value)[0:0], typed...)
	case string:
		*value = append((*value)[0:0], typed...)
	default:
		return fmt.Errorf("%w: %T", ErrUnsupportedJSON, raw)
	}
	return nil
}

type Tool struct {
	ID           string    `gorm:"primaryKey;type:text"`
	Name         string    `gorm:"uniqueIndex;not null"`
	Description  string    `gorm:"not null"`
	CommandPath  string    `gorm:"not null"`
	InputSchema  JSON      `gorm:"type:json;not null"`
	OutputSchema JSON      `gorm:"type:json;not null"`
	TimeoutMS    int       `gorm:"not null"`
	Status       string    `gorm:"not null;index"`
	CreatedAt    time.Time `gorm:"not null"`
	UpdatedAt    time.Time `gorm:"not null"`
}

type AgentInstruction struct {
	ID        int       `gorm:"primaryKey"`
	Content   string    `gorm:"not null"`
	UpdatedAt time.Time `gorm:"not null"`
}

type AgentRun struct {
	ID               string    `gorm:"primaryKey;type:text"`
	SessionID        string    `gorm:"not null;default:'';index"`
	TriggerMessageID string    `gorm:"not null;default:'';index"`
	Status           string    `gorm:"not null"`
	ErrorSummary     string    `gorm:"not null;default:''"`
	StartedAt        time.Time `gorm:"not null"`
	FinishedAt       sql.NullTime
}

type ChatSession struct {
	ID        string    `gorm:"primaryKey;type:text"`
	Title     string    `gorm:"not null;default:''"`
	Status    string    `gorm:"not null;index"`
	CreatedAt time.Time `gorm:"not null"`
	UpdatedAt time.Time `gorm:"not null"`
}

type ChatMessage struct {
	ID           string    `gorm:"primaryKey;type:text"`
	SessionID    string    `gorm:"not null;index"`
	RunID        string    `gorm:"not null;default:'';index"`
	Role         string    `gorm:"not null;index"`
	Content      string    `gorm:"not null"`
	Status       string    `gorm:"not null;index"`
	Sequence     int       `gorm:"not null;index"`
	CreatedAt    time.Time `gorm:"not null"`
	CompletedAt  sql.NullTime
	ErrorSummary string      `gorm:"not null;default:''"`
	Session      ChatSession `gorm:"foreignKey:SessionID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE"`
}

type ChatAttachment struct {
	ID             string      `gorm:"primaryKey;type:text"`
	SessionID      string      `gorm:"not null;index"`
	MessageID      string      `gorm:"not null;index"`
	Filename       string      `gorm:"not null"`
	MIMEType       string      `gorm:"not null"`
	Kind           string      `gorm:"not null;index"`
	SizeBytes      int64       `gorm:"not null"`
	ProviderFileID string      `gorm:"not null;default:''"`
	CreatedAt      time.Time   `gorm:"not null"`
	Session        ChatSession `gorm:"foreignKey:SessionID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE"`
	Message        ChatMessage `gorm:"foreignKey:MessageID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE"`
}

type AgentInterruption struct {
	ID                string    `gorm:"primaryKey;type:text"`
	SessionID         string    `gorm:"not null;index"`
	RunID             string    `gorm:"not null;index"`
	Type              string    `gorm:"not null;index"`
	Status            string    `gorm:"not null;index"`
	Message           string    `gorm:"not null"`
	Payload           JSON      `gorm:"type:json;not null;default:'{}'"`
	ResponseMessageID string    `gorm:"not null;default:'';index"`
	CreatedAt         time.Time `gorm:"not null"`
	ResolvedAt        sql.NullTime
	Session           ChatSession `gorm:"foreignKey:SessionID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE"`
	Run               AgentRun    `gorm:"foreignKey:RunID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE"`
}

type AgentRunObservation struct {
	ID        string    `gorm:"primaryKey;type:text"`
	RunID     string    `gorm:"not null;index"`
	StepOrder int       `gorm:"not null"`
	Payload   JSON      `gorm:"type:json;not null"`
	CreatedAt time.Time `gorm:"not null"`
	Run       AgentRun  `gorm:"foreignKey:RunID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE"`
}

type AgentRunStep struct {
	ID            string    `gorm:"primaryKey;type:text"`
	RunID         string    `gorm:"not null;index"`
	StepOrder     int       `gorm:"not null"`
	ToolID        string    `gorm:"not null;index"`
	InputSummary  JSON      `gorm:"type:json;not null"`
	OutputSummary JSON      `gorm:"type:json;not null"`
	DurationMS    int64     `gorm:"not null"`
	Status        string    `gorm:"not null"`
	ErrorSummary  string    `gorm:"not null;default:''"`
	CreatedAt     time.Time `gorm:"not null"`
	Run           AgentRun  `gorm:"foreignKey:RunID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE"`
}

func Models() []any {
	return []any{
		&Tool{},
		&AgentInstruction{},
		&ChatSession{},
		&ChatMessage{},
		&ChatAttachment{},
		&AgentRun{},
		&AgentInterruption{},
		&AgentRunObservation{},
		&AgentRunStep{},
	}
}
