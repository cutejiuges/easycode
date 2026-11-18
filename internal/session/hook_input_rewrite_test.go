package session

import (
	"bytes"
	"context"
	"os"
	"testing"
)

func TestInputRewriteSemanticCorruptionPrecedesRepairAndKeepsBytes(t *testing.T) {
	for _, name := range []string{"digest", "profile", "expansion", "partial_candidate", "late_cancel", "recovery_disguise", "old_shape", "version", "metadata", "admission"} {
		t.Run(name, func(t *testing.T) {
			content := append(readCurrentFixture(t, "rewrite_invalid_"+name+".jsonl"), []byte(`{"seq":`)...)
			repo, lease := installCurrentFixture(t, content)
			defer repo.Close()
			defer lease.Close()
			loaded, err := NewLoader().Load(context.Background(), lease)
			if err == nil {
				_, err = NewReplayPlanner().Plan(loaded)
			}
			if err == nil {
				t.Fatal("forged current input evidence accepted")
			}
			path, _ := repo.JournalPath(testThreadID)
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(content, after) {
				t.Fatal("semantic failure repaired append-only bytes", err)
			}
		})
	}
}
