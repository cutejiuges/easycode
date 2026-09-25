package fault

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestErrorSupportsCodeAndCauseMatching(t *testing.T) {
	err := Wrap(CodeUserCancelled, "turn was cancelled", context.Canceled)
	if !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation cause is not discoverable")
	}
	if !errors.Is(err, &Error{Code: CodeUserCancelled}) {
		t.Fatal("fault code is not discoverable")
	}
}

func TestErrorMessageDoesNotAddSensitiveContext(t *testing.T) {
	err := New(CodeProviderRequest, "provider request failed")
	if strings.Contains(err.Error(), "Authorization") || strings.Contains(err.Error(), "api_key") {
		t.Fatalf("unexpected sensitive context: %s", err)
	}
}
