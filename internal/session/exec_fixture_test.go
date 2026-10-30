package session

import (
	"bytes"
	"context"
	"os"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/tool"
)

func TestCurrentHeterogeneousExecFixturesUseProductionReplay(t *testing.T) {
	for _, family := range []domain.ProviderFamily{domain.ProviderOpenAI, domain.ProviderAnthropic} {
		t.Run(string(family), func(t *testing.T) {
			fixture := readCurrentFixture(t, "heterogeneous_exec_"+string(family)+".jsonl")
			repository, lease := installCurrentFixture(t, fixture)
			defer repository.Close()
			defer lease.Close()
			loaded, err := NewLoader().Load(context.Background(), lease)
			if err != nil {
				t.Fatal(err)
			}
			plan, err := NewReplayPlanner().Plan(loaded)
			if err != nil {
				t.Fatal(err)
			}
			if len(loaded.Records) != 36 || plan.NextSequence != 37 || len(plan.NativeCommits) != 6 || len(plan.SampleUsages) != 3 || plan.SessionMetadata.Provider != family {
				t.Fatal("heterogeneous fixture summary changed")
			}
			if len(plan.Commands) != 3 || !plan.Commands[0].HasResult || !plan.Commands[1].HasResult || plan.Commands[2].HasResult || plan.Commands[1].Result.Status() != tool.ResultDenied {
				t.Fatal("full command history was reduced to recovery tail")
			}
			for i := 1; i < len(plan.Commands); i++ {
				if plan.Commands[i-1].Sequence >= plan.Commands[i].Sequence {
					t.Fatal("command history order changed")
				}
			}
			calls := plan.ToolRecovery.Calls()
			if len(calls) != 1 || calls[0].State != ReplayedToolCallStarted || calls[0].Invocation.ProviderCallID() != "call-interrupted" {
				t.Fatal("interrupted Exec not preserved")
			}
			var capabilities []tool.CapabilityID
			var approvals []tool.ApprovalOutcome
			for index, record := range loaded.Records {
				_, encoded, err := EncodeRecord(record)
				if err != nil || !bytes.Equal(encoded, bytes.Split(bytes.TrimSpace(fixture), []byte{'\n'})[index]) {
					t.Fatal("immutable fixture bytes changed")
				}
				switch record.EventKind {
				case EventToolCallReady:
					payload, err := DecodeToolCallReadyPayload(record)
					if err != nil {
						t.Fatal(err)
					}
					capabilities = append(capabilities, payload.Capability)
					if payload.SampleIndex == 0 && payload.CallIndex != uint32(len(capabilities)-1) {
						t.Fatal("call order changed")
					}
				case EventToolApprovalDecided:
					payload, err := DecodeToolApprovalDecidedPayload(record)
					if err != nil || payload.Source != ApprovalSourceInteractive {
						t.Fatal("approval source lost")
					}
					approvals = append(approvals, payload.Outcome)
				}
			}
			want := []tool.CapabilityID{tool.CapabilityGlob, tool.CapabilityGrep, tool.CapabilityRead, tool.CapabilityPatch, tool.CapabilityExec, tool.CapabilityExec, tool.CapabilityExec}
			if len(capabilities) != len(want) || len(approvals) != 4 {
				t.Fatal("fixture lost capabilities or approvals")
			}
			for index, capability := range want {
				if capabilities[index] != capability {
					t.Fatal("heterogeneous call order changed")
				}
			}
			if approvals[0] != tool.ApprovalOutcomeAllowOnce || approvals[1] != tool.ApprovalOutcomeAllowOnce || approvals[2] != tool.ApprovalOutcomeDeny || approvals[3] != tool.ApprovalOutcomeAllowOnce {
				t.Fatal("approval order changed")
			}
		})
	}
}

func TestExecInterruptedRecoveryRetainsCrossProcessLeaseAndAppendOnlyBytes(t *testing.T) {
	prefix := readCurrentFixture(t, "exec_started_tail.jsonl")
	torn := append(bytes.Clone(prefix), []byte(`{"schema_version":3`)...)
	root := createLeaseProcessJournal(t, torn)
	repository, err := OpenOrCreateRepository(root)
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	path, err := repository.JournalPath(testThreadID)
	if err != nil {
		t.Fatal(err)
	}
	helper := startJournalLeaseHelper(t, root)
	if _, err := repository.Open(context.Background(), testThreadID); !IsJournalBusy(err) {
		t.Fatalf("competing recovery acquired lease: %v", err)
	}
	during, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(during, torn) {
		t.Fatal("busy recovery repaired or modified journal")
	}
	helper.kill(t)
	lease, err := repository.Open(context.Background(), testThreadID)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	loaded, err := NewLoader().Load(context.Background(), lease)
	if err != nil || !loaded.Repair.Repaired {
		t.Fatalf("crash release did not permit repair: %v", err)
	}
	plan, err := NewReplayPlanner().Plan(loaded)
	if err != nil {
		t.Fatal(err)
	}
	calls := plan.ToolRecovery.Calls()
	if len(calls) != 1 || calls[0].State != ReplayedToolCallStarted || calls[0].Invocation.Capability() != tool.CapabilityExec {
		t.Fatal("replay lost interrupted execution")
	}
	writer, err := StartJournalWriter(lease, loaded.Identity, loaded.NextSequence)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close(context.Background())
	result := tool.NewInvocationErrorResult(calls[0].Invocation, tool.ResultOutcomeUncertain, "outcome_uncertain", "Tool failed: execution outcome is uncertain")
	draft, err := NewToolCallResultDraft(testTurnID, result)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := writer.AppendBatch(ctx, []RecordDraft{draft}); err == nil {
		t.Fatal("pre-admission cancellation accepted recovery")
	}
	before, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, prefix) {
		t.Fatal("cancelled recovery changed journal bytes")
	}
	records, err := writer.AppendBatch(context.Background(), []RecordDraft{draft})
	if err != nil || len(records) != 1 || records[0].Sequence != loaded.NextSequence {
		t.Fatalf("recovery sequence is not monotonic: %v", err)
	}
	if _, err := repository.Open(context.Background(), testThreadID); !IsJournalBusy(err) {
		t.Fatal("writer released lease before close")
	}
	if err := writer.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	sealed, err := os.ReadFile(path)
	if err != nil || !bytes.HasPrefix(sealed, prefix) {
		t.Fatal("recovery rewrote durable prefix")
	}
	lease, err = repository.Open(context.Background(), testThreadID)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	loaded, err = NewLoader().Load(context.Background(), lease)
	if err != nil {
		t.Fatal(err)
	}
	plan, err = NewReplayPlanner().Plan(loaded)
	if err != nil {
		t.Fatal(err)
	}
	calls = plan.ToolRecovery.Calls()
	if len(calls) != 1 || calls[0].State != ReplayedToolCallResult || calls[0].Result.Status() != tool.ResultOutcomeUncertain || len(loaded.Records) != 11 {
		t.Fatal("recovery reran or fabricated command success")
	}
}
