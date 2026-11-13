package runtime

import (
	"bufio"
	"context"
	"easycode/internal/domain"
	"easycode/internal/session"
	"os"
	"testing"
)

// TestStopOfflineRecoveryUsesFrozenPrefix 不安装 Hooks，也不提供网络或执行器入口。
func TestStopOfflineRecoveryUsesFrozenPrefix(t *testing.T) {
	for _, phase := range []string{"planned", "started", "prefix", "prefix_started", "unavailable"} {
		t.Run(phase, func(t *testing.T) {
			file, err := os.Open("../session/testdata/current/stop_" + phase + ".jsonl")
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			var records []session.Record
			scanner := bufio.NewScanner(file)
			scanner.Buffer(make([]byte, 4096), 1<<20)
			for scanner.Scan() {
				record, err := session.DecodeRecord(scanner.Bytes())
				if err != nil {
					t.Fatal(err)
				}
				records = append(records, record)
			}
			if err := scanner.Err(); err != nil {
				t.Fatal(err)
			}
			last := records[len(records)-1]
			loaded := session.LoadResult{Identity: session.Identity{SessionID: last.SessionID, ThreadID: last.ThreadID}, Records: records, NextSequence: last.Sequence + 1}
			plan, err := session.NewReplayPlanner().Plan(loaded)
			if err != nil || plan.StopRecovery == nil {
				t.Fatal(err)
			}
			journal := &fakeJournal{sequence: last.Sequence}
			conversation := &fakeConversation{}
			r := newTestRuntime(t, conversation, journal)
			if err := r.ReconcileStopTurn(context.Background(), plan.StopRecovery.Clone()); err != nil {
				t.Fatal(err)
			}
			batches := journal.snapshot()
			if len(batches) != 1 || conversation.calls.Load() != 0 || r.hooks.runner != nil || len(r.HookEntries()) != 0 {
				t.Fatal("recovery crossed local frozen boundary")
			}
			wantProcessed := len(plan.StopRecovery.State().Finished)
			wantDisposition := domain.StopCancelled
			wantCode := domain.HookStopInterrupted
			if plan.StopRecovery.State().Started != nil {
				wantProcessed++
				wantDisposition, wantCode = domain.StopUncertain, domain.HookOutcomeUncertain
			}
			if phase == "unavailable" {
				wantDisposition, wantCode = domain.StopError, domain.HookConfigInvalid
			}
			var decision domain.StopDecision
			terminal := 0
			for _, draft := range batches[0] {
				if draft.EventKind() == session.EventTurnStopHookDecided {
					decision, err = domain.DecodeStopDecision(draft.PayloadBytes())
					if err != nil {
						t.Fatal(err)
					}
				}
				if draft.EventKind() == session.EventTurnFailed {
					terminal++
				}
			}
			if decision.Processed != wantProcessed || decision.Disposition != wantDisposition || decision.Code != wantCode || decision.Origin != domain.HookOriginRecovery || terminal != 1 {
				t.Fatal("recovery invented outcome", decision)
			}
		})
	}
}
