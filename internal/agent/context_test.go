package agent

import "testing"

const testContextReference = "ctx://login/session"

func TestRunContextStoresSensitiveValuesAsReferences(t *testing.T) {
	ctx := NewRunContext()

	ref := ctx.Store("login", "session", "cookie-value")

	if ref != testContextReference {
		t.Fatalf("ref = %q, want %s", ref, testContextReference)
	}

	resolved, ok := ctx.Resolve(ref)
	if !ok {
		t.Fatal("Resolve() ok = false, want true")
	}
	if resolved.Value != "cookie-value" {
		t.Fatalf("Resolve() value = %q, want cookie-value", resolved.Value)
	}
}

func TestRunContextResolveLeavesPlainValuesUnresolved(t *testing.T) {
	ctx := NewRunContext()

	_, ok := ctx.Resolve("plain-value")

	if ok {
		t.Fatal("Resolve() ok = true, want false")
	}
}
