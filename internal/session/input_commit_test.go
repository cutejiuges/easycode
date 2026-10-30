package session

import (
	"context"
	"easycode/internal/domain"
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"
)

func TestInputAssociationStrictPayload(t *testing.T) {
	id := domain.InputID(testTurnID)
	a, _ := domain.NewInputAssociation(testTurnID, domain.InputInitial, 0, []domain.InputID{id})
	draft, err := NewTurnInputsCommittedDraft(a)
	if err != nil {
		t.Fatal(err)
	}
	record := Record{EventKind: EventTurnInputsCommitted, TurnID: testTurnID, PayloadVersion: EnvelopeVersion, Payload: draft.PayloadBytes()}
	if p, err := DecodeTurnInputsPayload(record); err != nil || len(p.InputIDs) != 1 || p.InputIDs[0] != id {
		t.Fatalf("roundtrip: %v", err)
	}
	for _, body := range []string{
		`{"placement":"initial","target_sample_index":1,"input_ids":["00000000-0005-7000-8000-000000000006"]}`,
		`{"placement":"steering","target_sample_index":0,"input_ids":["00000000-0005-7000-8000-000000000006"]}`,
		`{"placement":"initial","target_sample_index":0,"input_ids":[]}`,
		`{"placement":"initial","target_sample_index":0,"input_ids":["00000000-0005-7000-8000-000000000006"],"text":"secret"}`,
		`{"placement":"initial","target_sample_index":0,"input_ids":["00000000-0005-7000-8000-000000000006"],"request_id":"req"}`,
	} {
		record.Payload = json.RawMessage(body)
		if _, err := DecodeTurnInputsPayload(record); err == nil {
			t.Fatalf("accepted %s", body)
		}
	}
}

func TestReplayInputGrammarAndInterruptedTail(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*replayFixture)
	}{
		{"duplicate initial", func(f *replayFixture) { f.appendInitialInputs(testTurnID) }},
		{"after terminal", func(f *replayFixture) {
			f.appendBatch(mustDraft(t, EventTurnFailed, testTurnID, TurnFailedPayload{Code: "cancelled"}))
			f.appendInitialInputs(testTurnID)
		}},
		{"steering before outputs", func(f *replayFixture) {
			a, _ := domain.NewInputAssociation(testTurnID, domain.InputSteering, 1, []domain.InputID{domain.InputID(secondTurnID)})
			draft, _ := NewTurnInputsCommittedDraft(a)
			f.appendBatch(draft, mustDraft(t, EventProviderNativeCommit, testTurnID, validNativeCommit()))
		}},
		{"reused identity", func(f *replayFixture) {
			f.appendBatch(mustDraft(t, EventTurnFailed, testTurnID, TurnFailedPayload{Code: "cancelled"}))
			f.appendBatch(mustDraft(t, EventTurnStarted, secondTurnID, TurnStartedPayload{}))
			a, _ := domain.NewInputAssociation(secondTurnID, domain.InputInitial, 0, []domain.InputID{domain.InputID(testTurnID)})
			draft, _ := NewTurnInputsCommittedDraft(a)
			f.appendBatch(draft, mustDraft(t, EventProviderNativeCommit, secondTurnID, validNativeCommit()))
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newReplayFixture(t)
			f.appendMetadata()
			f.appendBatch(mustDraft(t, EventTurnStarted, testTurnID, TurnStartedPayload{}))
			f.appendInitialInputs(testTurnID)
			plan, err := NewReplayPlanner().Plan(f.loaded())
			if err != nil || plan.InterruptedTail == nil || len(plan.NativeCommits) != 1 || plan.NativeCommits[0].Role != domain.NativeCommitUserInputs {
				t.Fatalf("input-only recovery: %v", err)
			}
			tc.mutate(f)
			if _, err := NewReplayPlanner().Plan(f.loaded()); err == nil {
				t.Fatal("invalid input grammar accepted")
			}
		})
	}
}

func TestCurrentInputOnlyAndSteeringFixtures(t *testing.T) {
	for _, family := range []string{"openai", "anthropic"} {
		for _, prefix := range []string{"input_only_", "steering_tail_"} {
			t.Run(prefix+family, func(t *testing.T) {
				original := readCurrentFixture(t, prefix+family+".jsonl")
				_, lease := installCurrentFixture(t, original)
				loaded, err := NewLoader().Load(context.Background(), lease)
				if err != nil {
					t.Fatal(err)
				}
				plan, err := NewReplayPlanner().Plan(loaded)
				if err != nil {
					t.Fatal(err)
				}
				commits := plan.NativeCommits
				if len(commits) == 0 || commits[len(commits)-1].Role != domain.NativeCommitUserInputs {
					t.Fatal("input native commit missing")
				}
				if prefix == "input_only_" {
					if plan.InterruptedTail == nil || len(commits) != 1 {
						t.Fatal("input-only tail not recovered")
					}
				} else {
					if plan.ToolRecovery == nil || len(commits) != 4 || commits[3].Inputs.Placement() != domain.InputSteering || len(commits[3].Inputs.IDs()) != 2 {
						t.Fatal("steering tail association lost")
					}
				}
			})
		}
	}
}

func TestInputDraftBudgetIncludesCanonicalEnvelopeAndNewlines(t *testing.T) {
	makeDraft := func(size int) RecordDraft {
		t.Helper()
		draft, err := NewProviderNativeCommitDraft(testTurnID, NativeCommitPayload{Provider: domain.ProviderOpenAI, Wire: "responses", PayloadVersion: 2, Payload: json.RawMessage(`{"text":"` + strings.Repeat("x", size) + `"}`)})
		if err != nil {
			t.Fatal(err)
		}
		return draft
	}
	small := makeDraft(0)
	const id = "00000000-0000-7000-8000-000000000001"
	record, err := BuildRecord(Identity{SessionID: domain.SessionID(id), ThreadID: domain.ThreadID(id)}, small, math.MaxUint64, time.Date(9999, 12, 31, 23, 59, 59, 999999999, time.UTC), math.MaxUint64, math.MaxUint32-1, math.MaxUint32)
	if err != nil {
		t.Fatal(err)
	}
	_, encoded, err := EncodeRecord(record)
	if err != nil {
		t.Fatal(err)
	}
	exact := makeDraft(MaxRecordBytes - len(encoded))
	if err := ValidateDraftBatchSize([]RecordDraft{exact}); err != nil {
		t.Fatalf("exact record limit rejected: %v", err)
	}
	over := makeDraft(MaxRecordBytes - len(encoded) + 1)
	if err := ValidateDraftBatchSize([]RecordDraft{over}); err == nil {
		t.Fatal("record envelope overhead ignored")
	}
	if err := ValidateDraftBatchSize([]RecordDraft{exact, exact, exact}); err != nil {
		t.Fatalf("valid batch rejected: %v", err)
	}
	if err := ValidateDraftBatchSize([]RecordDraft{exact, exact, exact, exact}); err == nil {
		t.Fatal("64 MiB batch ignored JSONL newlines")
	}
}
