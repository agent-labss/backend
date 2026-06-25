package agent

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
)

var (
	ErrDatabaseMissing          = errors.New("agent database is missing")
	ErrAgentExecutionNotFound   = errors.New("agent execution not found")
	ErrAgentExecutionNotWaiting = errors.New("agent execution is not waiting for user")
	ErrAgentExecutionActive     = errors.New("agent execution is already active")
	ErrChatSessionNotFound      = errors.New("chat session not found")
	ErrNoActiveInterruption     = errors.New("active interruption not found")
)

type Repository struct {
	database *gorm.DB
}

func NewRepository(db *gorm.DB) Repository {
	if db == nil {
		return Repository{}
	}

	return Repository{database: db}
}

func newRuntimeID(prefix string) string {
	return fmt.Sprintf("%s_%d", prefix, time.Now().UTC().UnixNano())
}

func isUniqueConstraintError(err error) bool {
	return strings.Contains(err.Error(), "UNIQUE constraint failed")
}
