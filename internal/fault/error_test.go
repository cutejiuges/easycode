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

func TestProjectDropsNestedSensitiveCause(t *testing.T) {
	err := Wrap(
		CodeProviderRequest,
		"provider request failed",
		errors.New("Authorization: Bearer secret-key body=private-prompt https://user:pass@example.com"),
	)
	summary := Project(err)
	if summary.Code != CodeProviderRequest || summary.Message != "provider request failed" || summary.Cancelled {
		t.Fatalf("summary = %#v", summary)
	}
	for _, secret := range []string{"secret-key", "private-prompt", "user:pass"} {
		if strings.Contains(summary.Message, secret) {
			t.Fatalf("summary leaked %q: %#v", secret, summary)
		}
	}
}
