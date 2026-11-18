package session

import (
	"bytes"
	"context"
	"os"
	"testing"
)

func TestHookSemanticCorruptionPrecedesTornTailRepair(t *testing.T) {
	content := append(readCurrentFixture(t, "hook_invalid.jsonl"), []byte(`{"seq":`)...)
	repo, lease := installCurrentFixture(t, content)
	defer repo.Close()
	defer lease.Close()
	loaded, err := NewLoader().Load(context.Background(), lease)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.NeedsRepair() {
		t.Fatal("missing torn tail")
	}
	if _, err := NewReplayPlanner().Plan(loaded); err == nil {
		t.Fatal("skipped ordinal accepted")
	}
	path, _ := repo.JournalPath(testThreadID)
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, content) {
		t.Fatal("semantic failure repaired bytes")
	}
}
