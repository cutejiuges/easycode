package session

import (
	"bytes"
	"context"
	"os"
	"testing"
)

func TestStopFixedSemanticCorruptionPrecedesTornTailRepair(t *testing.T) {
	for _, name := range []string{"invalid", "invalid_missing_delivery"} {
		t.Run(name, func(t *testing.T) {
			content := append(readCurrentFixture(t, "stop_"+name+".jsonl"), []byte(`{"seq":`)...)
			repo, lease := installCurrentFixture(t, content)
			defer repo.Close()
			defer lease.Close()
			loaded, err := NewLoader().Load(context.Background(), lease)
			if err != nil || !loaded.NeedsRepair() {
				t.Fatal("missing torn tail", err)
			}
			if _, err := NewReplayPlanner().Plan(loaded); err == nil {
				t.Fatal("semantic corruption accepted")
			}
			path, _ := repo.JournalPath(testThreadID)
			after, _ := os.ReadFile(path)
			if !bytes.Equal(content, after) {
				t.Fatal("corrupt prefix was repaired")
			}
		})
	}
}
