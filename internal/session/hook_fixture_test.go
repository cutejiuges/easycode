package session

import (
	"context"
	"testing"
)

func TestFixedHookCorruptionPrecedesTornTailRepair(t *testing.T) {
	for _, test := range []struct {
		fixture     string
		loadRejects bool
	}{
		{"hook_invalid", false},
		{"post_hook_invalid", false},
		{"stop_invalid", false},
		{"stop_invalid_missing_delivery", false},
		{"rewrite_invalid_digest", false},
		{"rewrite_invalid_profile", false},
		{"rewrite_invalid_expansion", true},
		{"rewrite_invalid_partial_candidate", false},
		{"rewrite_invalid_late_cancel", true},
		{"rewrite_invalid_recovery_disguise", false},
		{"rewrite_invalid_old_shape", true},
		{"rewrite_invalid_version", true},
		{"rewrite_invalid_metadata", false},
		{"rewrite_invalid_admission", false},
	} {
		t.Run(test.fixture, func(t *testing.T) {
			content := append(readCurrentFixture(t, test.fixture+".jsonl"), []byte(`{"seq":`)...)
			repo, lease := installCurrentFixture(t, content)
			defer repo.Close()
			defer lease.Close()
			loaded, err := NewLoader().Load(context.Background(), lease)
			if (err != nil) != test.loadRejects {
				t.Fatalf("unexpected rejection stage: load=%v", err)
			}
			if !test.loadRejects {
				if !loaded.NeedsRepair() {
					t.Fatal("missing torn tail")
				}
				if _, err := NewReplayPlanner().Plan(loaded); err == nil {
					t.Fatal("corrupt hook evidence reached repair")
				}
			}
			assertJournalBytes(t, repo, content)
		})
	}
}
