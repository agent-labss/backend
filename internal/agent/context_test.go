package agent

import "testing"

const testContextReference = "ctx://step_1/login/session"

func TestRunContextStoresSensitiveValuesAsReferences(t *testing.T) {
	ctx := NewExecutionContext()

	ref := ctx.Store("step_1", "login", "session", "cookie-value")

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

func TestRunContextReferencesIncludeStepID(t *testing.T) {
	ctx := NewExecutionContext()

	firstRef := ctx.Store("step_1", "login", "session", "first-cookie")
	secondRef := ctx.Store("step_2", "login", "session", "second-cookie")

	if firstRef == secondRef {
		t.Fatalf("refs matched %q, want distinct refs per step", firstRef)
	}

	first, ok := ctx.Resolve(firstRef)
	if !ok || first.Value != "first-cookie" {
		t.Fatalf("first resolved = %v, %v; want first-cookie, true", first.Value, ok)
	}
	second, ok := ctx.Resolve(secondRef)
	if !ok || second.Value != "second-cookie" {
		t.Fatalf("second resolved = %v, %v; want second-cookie, true", second.Value, ok)
	}
}

func TestRunContextResolveLeavesPlainValuesUnresolved(t *testing.T) {
	ctx := NewExecutionContext()

	_, ok := ctx.Resolve("plain-value")

	if ok {
		t.Fatal("Resolve() ok = true, want false")
	}
}
