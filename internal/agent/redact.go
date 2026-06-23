package agent

import (
	"regexp"
	"strings"
)

const redactedValue = "[REDACTED]"

var sensitiveKeyFragments = []string{
	"authorization",
	"cookie",
	"credential",
	"key",
	"password",
	"secret",
	"token",
}

var bearerPattern = regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9._~+/=-]+`)
var cookiePairPattern = regexp.MustCompile(`(?i)(cookie:\s*)[^\n\r]+`)

func RedactText(value string) string {
	redacted := bearerPattern.ReplaceAllString(value, "Bearer "+redactedValue)
	redacted = cookiePairPattern.ReplaceAllString(redacted, "${1}"+redactedValue)
	return redacted
}

func RedactJSONValue[T any](value T) T {
	switch typed := any(value).(type) {
	case map[string]any:
		redacted, ok := any(redactMap(typed)).(T)
		if ok {
			return redacted
		}
	case []any:
		redacted, ok := any(redactSlice(typed)).(T)
		if ok {
			return redacted
		}
	case string:
		redacted, ok := any(RedactText(typed)).(T)
		if ok {
			return redacted
		}
	default:
		return value
	}

	return value
}

func redactMap(value map[string]any) map[string]any {
	redacted := make(map[string]any, len(value))
	for key, nested := range value {
		if isSensitiveKey(key) {
			redacted[key] = redactedValue
			continue
		}
		redacted[key] = RedactJSONValue(nested)
	}

	return redacted
}

func redactSlice(value []any) []any {
	redacted := make([]any, 0, len(value))
	for _, nested := range value {
		redacted = append(redacted, RedactJSONValue(nested))
	}

	return redacted
}

func isSensitiveKey(key string) bool {
	normalized := strings.ToLower(key)
	for _, fragment := range sensitiveKeyFragments {
		if strings.Contains(normalized, fragment) {
			return true
		}
	}

	return false
}
