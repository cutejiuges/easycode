package session

import (
	"context"
	"errors"
	"testing"
	"time"

	"easycode/internal/tool"
)

func TestExecLedgerWriteAndSyncFailuresPoisonBeforeAcknowledgement(t *testing.T) {
	invocation := replayExecInvocation(t)
	ready, err := NewToolCallReadyDraft(testTurnID, invocation, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	approval := mustPatchApprovalDraft(t, invocation, tool.NewCandidateFingerprint([]byte("candidate")), tool.ApprovalOutcomeAllowOnce)
	started, err := NewToolExecutionStartedDraft(testTurnID, invocation.InvocationID())
	if err != nil {
		t.Fatal(err)
	}
	result := replayExecResult(t, invocation, tool.ResultSuccess, "ok")
	for _, draft := range []RecordDraft{ready, approval, started, result} {
		for _, syncFailure := range []bool{false, true} {
			t.Run(string(draft.EventKind())+map[bool]string{false: "/write", true: "/sync"}[syncFailure], func(t *testing.T) {
				file := &memoryJournalFile{shortWrite: !syncFailure}
				if syncFailure {
					file.syncErr = errors.New("injected Sync failure")
				}
				writer, err := startJournalWriter(file, Identity{SessionID: testSessionID, ThreadID: testThreadID}, 1, func() time.Time { return time.Unix(0, 0).UTC() })
				if err != nil {
					t.Fatal(err)
				}
				defer writer.Close(context.Background())
				if records, err := writer.AppendBatch(context.Background(), []RecordDraft{draft}); err == nil || len(records) != 0 || !writer.Poisoned() {
					t.Fatal("failed durable fact was acknowledged")
				}
				before := file.writeCount
				if _, err := writer.AppendBatch(context.Background(), []RecordDraft{draft}); err == nil || file.writeCount != before {
					t.Fatal("poisoned writer retried a fact")
				}
			})
		}
	}
}
