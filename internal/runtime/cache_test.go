package runtime

import (
	"context"
	"testing"

	"easycode/internal/provider"
	"easycode/internal/session"
)

func TestCacheObservationOnlyAfterDurableFinalize(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "sync failure"}[fail], func(t *testing.T) {
			conversation := &fakeConversation{stream: fixedStream(completedProviderEvent(t))}
			journal := &fakeJournal{}
			runtime := newTestRuntime(t, conversation, journal)
			checked := false
			journal.hook = func(drafts []session.RecordDraft) {
				for _, draft := range drafts {
					if draft.EventKind() == session.EventSampleUsage {
						checked = true
						if runtime.cache.snapshot.LastSample != nil || runtime.cache.snapshot.LastTurn != nil {
							t.Error("observation advanced before durable result")
						}
					}
				}
			}
			if fail {
				journal.failAt = 3
			}
			_, err := collectTurn(runtime, context.Background(), nil)
			if fail {
				if err == nil || runtime.cache.snapshot.LastSample != nil || runtime.cache.snapshot.LastTurn != nil {
					t.Fatal("Sync failure published success")
				}
				return
			}
			if err != nil || !checked || runtime.cache.snapshot.LastSample == nil || runtime.cache.snapshot.LastTurn == nil {
				t.Fatalf("missing durable observations: checked=%t err=%v", checked, err)
			}
			if runtime.cache.snapshot.LastSummary != nil {
				t.Fatal("sample counted as summary")
			}
			if err := runtime.cache.snapshot.Validate(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

var _ provider.Conversation = (*controlledConversation)(nil)
