package session

import (
	"bytes"
	"context"
	"os"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/provider"
)

func TestSessionStartActivationFixtureRetainsOnlyCompleteDelivery(t *testing.T) {
	ctx := context.Background()
	content := readCurrentFixture(t, "session_start_applied.jsonl")
	rows := bytes.Split(bytes.TrimSuffix(content, []byte{'\n'}), []byte{'\n'})
	for _, test := range []struct {
		name    string
		torn    bool
		records []byte
	}{
		{name: "durable", records: content},
		{name: "torn", torn: true, records: append(bytes.Join(rows[:8], []byte{'\n'}), '\n')},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo, lease := installCurrentFixture(t, test.records)
			defer repo.Close()
			defer lease.Close()
			loader := NewLoader()
			loaded, err := loader.Load(ctx, lease)
			if err != nil || loaded.NeedsRepair() != test.torn {
				t.Fatalf("load repair status: %v, %v", loaded.NeedsRepair(), err)
			}
			plan, err := NewReplayPlanner().Plan(loaded)
			if err != nil || plan.InterruptedTail == nil {
				t.Fatalf("initial turn replay: %v", err)
			}
			if !test.torn {
				if plan.InterruptedSessionStart != nil || len(plan.NativeCommits) != 2 ||
					plan.NativeCommits[0].Sequence != 7 || plan.NativeCommits[0].Role != domain.NativeCommitSessionStartContext ||
					plan.NativeCommits[1].Sequence != 9 || plan.NativeCommits[1].Role != domain.NativeCommitUserInputs {
					t.Fatal("durable activation did not preserve both original native positions")
				}
				close, err := DecodeSessionStartHookClosed(loaded.Records[6])
				if err != nil || close.Native == nil || len(close.Proofs) != 1 || close.Target.Source != domain.SessionStartStartup {
					t.Fatalf("applied activation evidence: %v", err)
				}
				return
			}
			if len(plan.NativeCommits) != 0 || plan.InterruptedSessionStart == nil || loaded.NextSequence != 7 {
				t.Fatal("torn batch retained some of its native entries or lost its own open plan")
			}
			if report, err := loader.Repair(ctx, lease, loaded); err != nil || !report.Repaired {
				t.Fatalf("repair did not remove the incomplete batch: %v", err)
			}
			draft, err := plan.InterruptedSessionStart.CloseDraft()
			if err != nil {
				t.Fatal(err)
			}
			recovered := appendRecoveryRecord(t, loaded, draft, recoveryRecordExpectation{finished: 1, state: SessionStartInterrupted})
			after, err := NewReplayPlanner().Plan(recovered)
			if err != nil || after.InterruptedSessionStart != nil || after.InterruptedTail == nil || len(after.NativeCommits) != 0 {
				t.Fatalf("recovery did not close just the SessionStart prefix: %v", err)
			}
		})
	}
	t.Run("wrong completed second line is corruption", func(t *testing.T) {
		first, err := DecodeRecord(rows[6])
		if err != nil {
			t.Fatal(err)
		}
		draft, err := NewTurnFailedDraft(first.TurnID, TurnFailedPayload{Code: "session_interrupted", Message: "session turn was interrupted"})
		if err != nil {
			t.Fatal(err)
		}
		record, err := BuildRecord(Identity{SessionID: first.SessionID, ThreadID: first.ThreadID}, draft, 8, first.Timestamp, 7, 1, 3)
		if err != nil {
			t.Fatal(err)
		}
		_, wrong, err := EncodeRecord(record)
		if err != nil {
			t.Fatal(err)
		}
		invalid := append(append(bytes.Join(rows[:7], []byte{'\n'}), '\n'), append(wrong, '\n')...)
		repo, lease := installCurrentFixture(t, invalid)
		defer repo.Close()
		defer lease.Close()
		if _, err := NewLoader().Load(ctx, lease); err == nil {
			t.Fatal("a complete wrong event in a partial activation batch was treated as repairable")
		}
	})
}

func TestSessionStartStartupCannotFollowAnEarlierActivationOrInput(t *testing.T) {
	ctx := context.Background()
	activation := readCurrentFixture(t, "session_start_applied.jsonl")
	rows := bytes.Split(activation, []byte{'\n'})
	planRecord, err := DecodeRecord(rows[3])
	if err != nil {
		t.Fatal(err)
	}
	plan, err := DecodeSessionStartHookPlanned(planRecord)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name     string
		prefix   []byte
		closed   bool
		planned  bool
		activate bool
	}{
		{name: "cancelled before a plan", prefix: bytes.Join(rows[:3], []byte{'\n'}), activate: true},
		{name: "prior initial input", prefix: readCurrentFixture(t, "root.jsonl"), closed: true},
		{name: "prior startup without initial input", prefix: bytes.Join(rows[:6], []byte{'\n'}), planned: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			prefix := append(bytes.TrimSuffix(test.prefix, []byte{'\n'}), '\n')
			if test.planned {
				skipped, err := NewSessionStartHookClosedDraft(plan, 1, SessionStartSkipped, domain.HookFeedbackBudgetExceeded, nil, provider.NativeCommitEnvelope{})
				if err != nil {
					t.Fatal(err)
				}
				prefix = appendStartupTestRecord(t, prefix, planRecord, skipped)
			}
			if !test.closed {
				draft, err := NewTurnFailedDraft(plan.target.TurnID, TurnFailedPayload{Code: "cancelled", Message: "turn was cancelled", Cancelled: true})
				if err != nil {
					t.Fatal(err)
				}
				prefix = appendStartupTestRecord(t, prefix, planRecord, draft)
			}
			target := domain.SessionStartTarget{Source: domain.SessionStartStartup, TurnID: "00000000-0010-7000-8000-000000000010"}
			started, err := NewTurnStartedDraft(target.TurnID)
			if err != nil {
				t.Fatal(err)
			}
			next, err := buildSessionStartPlan(sessionStartPlanFields{Target: target, Checks: plan.checks})
			if err != nil {
				t.Fatal(err)
			}
			planned, err := NewSessionStartHookPlannedDraft(next)
			if err != nil {
				t.Fatal(err)
			}
			prefix = appendStartupTestRecord(t, prefix, planRecord, started)
			prefix = appendStartupTestRecord(t, prefix, planRecord, planned)
			repo, lease := installCurrentFixture(t, prefix)
			defer repo.Close()
			defer lease.Close()
			loaded, err := NewLoader().Load(ctx, lease)
			if err != nil {
				t.Fatal(err)
			}
			replayed, err := NewReplayPlanner().Plan(loaded)
			if test.activate && (err != nil || replayed.InterruptedSessionStart == nil) || !test.activate && err == nil {
				t.Fatalf("unexpected startup replay admission: %v", err)
			}
		})
	}
}

func appendStartupTestRecord(t *testing.T, prefix []byte, identity Record, draft RecordDraft) []byte {
	t.Helper()
	sequence := uint64(bytes.Count(prefix, []byte{'\n'})) + 1
	record, err := BuildRecord(Identity{SessionID: identity.SessionID, ThreadID: identity.ThreadID},
		draft, sequence, identity.Timestamp, sequence, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	_, encoded, err := EncodeRecord(record)
	if err != nil {
		t.Fatal(err)
	}
	return append(prefix, append(encoded, '\n')...)
}

func TestSessionStartCompactFixturePrecedesPendingStop(t *testing.T) {
	base := readCurrentFixture(t, "stop_post_compact_completed.jsonl")
	ctx := context.Background()
	t.Run("applied", func(t *testing.T) {
		suffix := readCurrentFixture(t, "session_start_compact_suffix.jsonl")
		repo, lease := installCurrentFixture(t, append(base, suffix...))
		defer repo.Close()
		defer lease.Close()
		loaded, err := NewLoader().Load(ctx, lease)
		if err != nil || loaded.NeedsRepair() {
			t.Fatalf("read applied closure: %v", err)
		}
		planner := NewReplayPlanner()
		plan, err := planner.Plan(loaded)
		if err != nil || plan.InterruptedSessionStart != nil || plan.StopRecovery == nil ||
			!plan.StopRecovery.State().PendingFeedback() || len(plan.Compactions) != 1 || plan.Compactions[0].TerminalSequence != 13 ||
			len(plan.NativeCommits) != 3 || plan.NativeCommits[2].TurnID != "" || plan.NativeCommits[2].Sequence != 22 ||
			plan.NativeCommits[2].SessionStart.Target.Scope.TurnID != plan.StopRecovery.TurnID() {
			t.Fatalf("compact closure did not preserve the real checkpoint and Stop obligation: %v", err)
		}
		if _, err := planner.PlanMetadata(loaded); err != nil {
			t.Fatalf("catalog grammar disagrees with full replay: %v", err)
		}

		prefix := finalLoadResult(loaded.Identity, loaded.Records[:20], testThreadID, RepairReport{})
		interrupted, err := planner.Plan(prefix)
		if err != nil || interrupted.InterruptedSessionStart == nil || interrupted.StopRecovery == nil {
			t.Fatalf("started compact prefix did not preserve the pending Stop: %v", err)
		}
		draft, err := interrupted.InterruptedSessionStart.CloseDraft()
		if err != nil {
			t.Fatal(err)
		}
		recovered := appendRecoveryRecord(t, prefix, draft, recoveryRecordExpectation{state: SessionStartUncertain})
		after, err := planner.Plan(recovered)
		if err != nil || after.InterruptedSessionStart != nil || after.StopRecovery == nil || len(after.NativeCommits) != 2 {
			t.Fatalf("compact recovery synthesized context or discarded the Stop obligation: %v", err)
		}
	})

	t.Run("forged singleton cannot be repaired", func(t *testing.T) {
		content := append(base, readCurrentFixture(t, "session_start_compact_forged_arity_suffix.jsonl")...)
		repo, lease := installCurrentFixture(t, content)
		defer repo.Close()
		defer lease.Close()
		if _, err := NewLoader().Load(ctx, lease); err == nil {
			t.Fatal("complete compact closure masquerading as incomplete batch was accepted")
		}
		path, err := repo.JournalPath(testThreadID)
		if err != nil {
			t.Fatal(err)
		}
		actual, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(actual, content) {
			t.Fatalf("malformed journal was rewritten: %v", err)
		}
	})
}

type recoveryRecordExpectation struct {
	finished int
	state    SessionStartCloseState
}

func appendRecoveryRecord(t *testing.T, prefix LoadResult, draft RecordDraft, want recoveryRecordExpectation) LoadResult {
	t.Helper()
	sequence := prefix.NextSequence
	record, err := BuildRecord(prefix.Identity, draft, sequence, prefix.Records[0].Timestamp, sequence, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := DecodeSessionStartHookClosed(record)
	if err != nil || payload.Origin != "recovery" || payload.State != want.state || payload.Finished != want.finished || payload.Native != nil {
		t.Fatalf("invalid recovery closure %+v: %v", payload, err)
	}
	records := append(append([]Record(nil), prefix.Records...), record)
	return finalLoadResult(prefix.Identity, records, testThreadID, RepairReport{})
}
