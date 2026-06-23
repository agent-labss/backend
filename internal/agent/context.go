package agent

import (
	"strings"
	"sync"
)

const contextReferencePrefix = "ctx://"

type RunContext struct {
	mutex  sync.RWMutex
	values map[string]any
}

type ContextValue struct {
	Value any
}

func NewRunContext() *RunContext {
	return &RunContext{values: make(map[string]any)}
}

func (context *RunContext) Store(stepID string, toolName string, outputName string, value any) string {
	ref := contextReferencePrefix + stepID + "/" + toolName + "/" + outputName
	context.mutex.Lock()
	defer context.mutex.Unlock()
	context.values[ref] = value
	return ref
}

func (context *RunContext) Resolve(value string) (ContextValue, bool) {
	if !strings.HasPrefix(value, contextReferencePrefix) {
		return ContextValue{}, false
	}

	context.mutex.RLock()
	defer context.mutex.RUnlock()
	resolved, ok := context.values[value]
	return ContextValue{Value: resolved}, ok
}
