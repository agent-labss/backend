package agent

import (
	"strings"
	"sync"
)

const contextReferencePrefix = "ctx://"

type ExecutionContext struct {
	mutex  sync.RWMutex
	values map[string]any
}

type ContextValue struct {
	Value any
}

func NewExecutionContext() *ExecutionContext {
	return &ExecutionContext{values: make(map[string]any)}
}

func (context *ExecutionContext) Store(stepID string, toolName string, outputName string, value any) string {
	ref := contextReferencePrefix + stepID + "/" + toolName + "/" + outputName
	context.mutex.Lock()
	defer context.mutex.Unlock()
	context.values[ref] = value
	return ref
}

func (context *ExecutionContext) Resolve(value string) (ContextValue, bool) {
	if !isContextReference(value) {
		return ContextValue{}, false
	}

	context.mutex.RLock()
	defer context.mutex.RUnlock()
	resolved, ok := context.values[value]
	return ContextValue{Value: resolved}, ok
}

func isContextReference(value string) bool {
	return strings.HasPrefix(value, contextReferencePrefix)
}
