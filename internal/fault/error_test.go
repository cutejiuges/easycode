package fault

import (
	"errors"
	"strings"
	"testing"
)

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
