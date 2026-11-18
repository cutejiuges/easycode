package session

import (
	"bytes"
	"context"
	"os"
	"testing"
)

func TestPostSemanticCorruptionPrecedesTornTailRepair(t *testing.T) {
	testPostCorruptionBeforeRepair(t, "post_hook_invalid.jsonl")
}

func testPostCorruptionBeforeRepair(t *testing.T, name string) {
	content := append(readCurrentFixture(t, name), []byte(`{"seq":`)...)
	repo, lease := installCurrentFixture(t, content)
	defer repo.Close()
	defer lease.Close()
	path, _ := repo.JournalPath(testThreadID)
	loaded, err := NewLoader().Load(context.Background(), lease)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewReplayPlanner().Plan(loaded); err == nil {
		t.Fatal("forged terminal budget accepted")
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(content, after) {
		t.Fatal("semantic corruption repaired before validation")
	}
}
