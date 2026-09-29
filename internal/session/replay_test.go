package session

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"easycode/internal/codec"
	"easycode/internal/domain"
)

const secondTurnID = domain.TurnID("00000000-0005-7000-8000-000000000006")

func TestReplayPlannerBuildsValidTextPlanAndSkipsOptional(t *testing.T) {
	t.Parallel()
	fixture := newReplayFixture(t)
	fixture.appendMetadata()
	fixture.appendBatch(mustDraft(t, EventTurnStarted, testTurnID, TurnStartedPayload{}))
	fixture.appendBatch(mustDraft(t, EventTurnFailed, testTurnID, TurnFailedPayload{Code: "user_cancelled"}))
	fixture.appendBatch(mustDraft(t, EventTurnStarted, secondTurnID, TurnStartedPayload{}))
	fixture.appendBatch(
		mustDraft(t, EventProviderNativeCommit, secondTurnID, validNativeCommit()),
		mustDraft(t, EventTurnCompleted, secondTurnID, TurnCompletedPayload{}),
	)
	fixture.appendUnknown(ReplayOptional)

	plan, err := NewReplayPlanner().Plan(fixture.loaded())
	if err != nil {
		t.Fatal(err)
	}
	if plan.Identity.SessionID != testSessionID || plan.Identity.ThreadID != testThreadID ||
		len(plan.NativeCommits) != 1 || plan.NativeCommits[0].TurnID != secondTurnID ||
		len(plan.Turns) != 2 || plan.Turns[0].State != ReplayedTurnFailed ||
		plan.Turns[1].State != ReplayedTurnCompleted || len(plan.OptionalRecords) != 1 ||
		plan.InterruptedTail != nil {
		t.Fatalf("Plan() = %#v", plan)
	}
	plan.NativeCommits[0].Commit.Payload[0] = '['
	if string(fixture.records[5].Payload) == "[" {
		t.Fatal("ReplayPlan shares mutable payload bytes with loaded records")
	}
}

func TestReplayPlannerReportsCommittedInterruptedTail(t *testing.T) {
	t.Parallel()
	fixture := newReplayFixture(t)
	fixture.appendMetadata()
	fixture.appendBatch(mustDraft(t, EventTurnStarted, testTurnID, TurnStartedPayload{}))
	plan, err := NewReplayPlanner().Plan(fixture.loaded())
	if err != nil {
		t.Fatal(err)
	}
	if plan.InterruptedTail == nil || plan.InterruptedTail.TurnID != testTurnID || plan.InterruptedTail.Sequence != 3 {
		t.Fatalf("InterruptedTail = %#v", plan.InterruptedTail)
	}
}

func TestReplayPlannerRejectsUnsupportedRequiredRevision(t *testing.T) {
	t.Parallel()
	fixture := newReplayFixture(t)
	fixture.appendMetadata()
	fixture.appendUnknown(ReplayRequired)
	if _, err := NewReplayPlanner().Plan(fixture.loaded()); err == nil || !strings.Contains(err.Error(), "required") {
		t.Fatalf("Plan() error = %v", err)
	}
}

func TestReplayPlannerRejectsIllegalLifecycleTransitions(t *testing.T) {
	t.Parallel()
	fixtures := map[string]func(*replayFixture){
		"duplicate metadata": func(fixture *replayFixture) {
			fixture.appendBatch(mustDraft(t, EventSessionMeta, "", fixture.sessionMetadata()))
		},
		"commit outside turn": func(fixture *replayFixture) {
			fixture.appendBatch(
				mustDraft(t, EventProviderNativeCommit, testTurnID, validNativeCommit()),
				mustDraft(t, EventTurnCompleted, testTurnID, TurnCompletedPayload{}),
			)
		},
		"duplicate terminal": func(fixture *replayFixture) {
			fixture.appendBatch(mustDraft(t, EventTurnStarted, testTurnID, TurnStartedPayload{}))
			fixture.appendBatch(mustDraft(t, EventTurnFailed, testTurnID, TurnFailedPayload{Code: "failed"}))
			fixture.appendBatch(mustDraft(t, EventTurnFailed, testTurnID, TurnFailedPayload{Code: "failed_again"}))
		},
		"completion missing terminal": func(fixture *replayFixture) {
			fixture.appendBatch(mustDraft(t, EventTurnStarted, testTurnID, TurnStartedPayload{}))
			fixture.appendBatch(mustDraft(t, EventProviderNativeCommit, testTurnID, validNativeCommit()))
		},
		"new turn covers active": func(fixture *replayFixture) {
			fixture.appendBatch(mustDraft(t, EventTurnStarted, testTurnID, TurnStartedPayload{}))
			fixture.appendBatch(mustDraft(t, EventTurnStarted, secondTurnID, TurnStartedPayload{}))
		},
		"optional follows active turn": func(fixture *replayFixture) {
			fixture.appendBatch(mustDraft(t, EventTurnStarted, testTurnID, TurnStartedPayload{}))
			fixture.appendUnknown(ReplayOptional)
		},
	}
	for name, mutate := range fixtures {
		name, mutate := name, mutate
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fixture := newReplayFixture(t)
			fixture.appendMetadata()
			mutate(fixture)
			if _, err := NewReplayPlanner().Plan(fixture.loaded()); err == nil || !strings.Contains(err.Error(), "corrupted") {
				t.Fatalf("Plan() error = %v", err)
			}
		})
	}
}

func TestReplayPlannerRejectsInvalidMetadataAndKnownPayload(t *testing.T) {
	t.Parallel()
	fixture := newReplayFixture(t)
	fixture.appendMetadata()
	fixture.records[0].Payload = json.RawMessage(`{"root_thread_id":"00000000-0001-7000-8000-000000000002","created_at":"1970-01-01T00:00:00Z","provider":"openai","provider_wire":"responses","model":"fixture","schema_revision":1,"creation_cwd":"/workspace","unknown":true}`)
	if _, err := NewReplayPlanner().Plan(fixture.loaded()); err == nil || !strings.Contains(err.Error(), "metadata") {
		t.Fatalf("Plan() error = %v", err)
	}

	fixture = newReplayFixture(t)
	fixture.appendMetadata()
	metadata, _ := DecodeSessionMetaPayload(fixture.records[0])
	metadata.CreationCWD = "relative/path"
	replacement := fixture.records[0]
	encodedPayload, err := codec.MarshalStable(metadata)
	if err != nil {
		t.Fatal(err)
	}
	replacement.Payload = encodedPayload
	fixture.records[0], _, err = EncodeRecord(replacement)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewReplayPlanner().Plan(fixture.loaded()); err == nil || !strings.Contains(err.Error(), "cwd") {
		t.Fatalf("Plan() error = %v", err)
	}
}

type replayFixture struct {
	t       *testing.T
	records []Record
	next    uint64
	cwd     string
}

func newReplayFixture(t *testing.T) *replayFixture {
	t.Helper()
	return &replayFixture{t: t, next: 1, cwd: filepath.Clean(t.TempDir())}
}

func (fixture *replayFixture) appendMetadata() {
	fixture.t.Helper()
	fixture.appendBatch(
		mustDraft(fixture.t, EventSessionMeta, "", fixture.sessionMetadata()),
		mustDraft(fixture.t, EventThreadMeta, "", ThreadMetaPayload{Root: true}),
	)
}

func (fixture *replayFixture) sessionMetadata() SessionMetaPayload {
	return SessionMetaPayload{
		RootThreadID: testThreadID, CreatedAt: time.Unix(0, 0).UTC(),
		Provider: domain.ProviderOpenAI, ProviderWire: "responses", Model: "fixture-model",
		SchemaRevision: 1, CreationCWD: fixture.cwd,
	}
}

func (fixture *replayFixture) appendBatch(drafts ...RecordDraft) {
	fixture.t.Helper()
	batchID := fixture.next
	for index, draft := range drafts {
		record, err := BuildRecord(
			Identity{SessionID: testSessionID, ThreadID: testThreadID}, draft,
			fixture.next, time.Unix(0, 0).UTC(), batchID, uint32(index), uint32(len(drafts)),
		)
		if err != nil {
			fixture.t.Fatal(err)
		}
		sealed, _, err := EncodeRecord(record)
		if err != nil {
			fixture.t.Fatal(err)
		}
		fixture.records = append(fixture.records, sealed)
		fixture.next++
	}
}

func (fixture *replayFixture) appendUnknown(requirement ReplayRequirement) {
	fixture.t.Helper()
	record := Record{
		SchemaVersion: EnvelopeVersion, PayloadVersion: 99, ReplayRequirement: requirement,
		Sequence: fixture.next, Timestamp: time.Unix(0, 0).UTC(), SessionID: testSessionID,
		ThreadID: testThreadID, EventKind: "future_event", BatchID: fixture.next,
		BatchIndex: 0, BatchSize: 1, Payload: json.RawMessage(`{"opaque":true}`),
	}
	sealed, _, err := EncodeRecord(record)
	if err != nil {
		fixture.t.Fatal(err)
	}
	fixture.records = append(fixture.records, sealed)
	fixture.next++
}

func (fixture *replayFixture) loaded() LoadResult {
	return LoadResult{
		Identity: Identity{SessionID: testSessionID, ThreadID: testThreadID},
		Records:  cloneRecords(fixture.records), NextSequence: fixture.next,
	}
}

func validNativeCommit() NativeCommitPayload {
	return NativeCommitPayload{
		Provider: domain.ProviderOpenAI, Wire: "responses", PayloadVersion: 1,
		Payload: json.RawMessage(`{"shape":"text_sample"}`),
	}
}
