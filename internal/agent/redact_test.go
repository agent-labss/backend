package agent

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRedactTextRemovesBearerTokensAndCookies(t *testing.T) {
	input := "Authorization: Bearer abc.def.ghi\nCookie: session_id=secret-value; theme=light"

	redacted := RedactText(input)

	if strings.Contains(redacted, "abc.def.ghi") || strings.Contains(redacted, "secret-value") {
		t.Fatalf("RedactText() = %q, want secrets removed", redacted)
	}
}

func TestRedactJSONValueRemovesSensitiveKeys(t *testing.T) {
	value := map[string]any{
		"password": "secret",
		"nested": map[string]any{
			"token": "abc",
			"name":  "Acme",
		},
	}

	redacted := RedactJSONValue(value)
	encoded, err := json.Marshal(redacted)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	text := string(encoded)

	if strings.Contains(text, "secret") || strings.Contains(text, "abc") {
		t.Fatalf("RedactJSONValue() = %s, want secrets removed", text)
	}
	if !strings.Contains(text, "Acme") {
		t.Fatalf("RedactJSONValue() = %s, want non-sensitive value preserved", text)
	}
}
