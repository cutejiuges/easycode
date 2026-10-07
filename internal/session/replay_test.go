package session

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"easycode/internal/codec"
	"easycode/internal/domain"
	"easycode/internal/tool"
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
		mustDraft(t, EventSampleUsage, secondTurnID, testSampleUsage(t)),
		mustDraft(t, EventTurnCompleted, secondTurnID, TurnCompletedPayload{}),
	)
	fixture.appendUnknown(ReplayOptional)

	plan, err := NewReplayPlanner().Plan(fixture.loaded())
	if err != nil {
		t.Fatal(err)
	}
	if plan.Identity.SessionID != testSessionID || plan.Identity.ThreadID != testThreadID ||
		len(plan.NativeCommits) != 1 || plan.NativeCommits[0].TurnID != secondTurnID ||
		len(plan.SampleUsages) != 1 || plan.SampleUsages[0].TurnID != secondTurnID ||
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
				mustDraft(t, EventSampleUsage, testTurnID, testSampleUsage(t)),
				mustDraft(t, EventTurnCompleted, testTurnID, TurnCompletedPayload{}),
			)
		},
		"completion without usage": func(fixture *replayFixture) {
			fixture.appendBatch(mustDraft(t, EventTurnStarted, testTurnID, TurnStartedPayload{}))
			fixture.appendBatch(
				mustDraft(t, EventProviderNativeCommit, testTurnID, validNativeCommit()),
				mustDraft(t, EventTurnCompleted, testTurnID, TurnCompletedPayload{}),
			)
		},
		"usage before commit": func(fixture *replayFixture) {
			fixture.appendBatch(mustDraft(t, EventTurnStarted, testTurnID, TurnStartedPayload{}))
			fixture.appendBatch(
				mustDraft(t, EventSampleUsage, testTurnID, testSampleUsage(t)),
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

func TestReplayPlannerBuildsCompletedToolLoop(t *testing.T) {
	fixture := newReplayFixture(t)
	fixture.appendMetadata()
	fixture.appendBatch(mustDraft(t, EventTurnStarted, testTurnID, TurnStartedPayload{}))
	invocation := testReplayInvocation(t, "01890f3e-7bcd-7abc-8abc-0123456789ab", "call-1")
	fixture.appendBatch(
		mustDraft(t, EventProviderNativeCommit, testTurnID, validNativeCommit()),
		mustDraft(t, EventSampleUsage, testTurnID, testSampleUsage(t)),
		mustReadyDraftForReplay(t, invocation, 0, 0),
	)
	fixture.appendBatch(mustStartedDraftForReplay(t, invocation))
	fixture.appendBatch(mustResultDraftForReplay(t, invocation))
	fixture.appendBatch(mustDraft(t, EventProviderNativeCommit, testTurnID, validToolOutputsCommit()))
	fixture.appendBatch(
		mustDraft(t, EventProviderNativeCommit, testTurnID, validNativeCommit()),
		mustDraft(t, EventSampleUsage, testTurnID, testSampleUsage(t)),
		mustDraft(t, EventTurnCompleted, testTurnID, TurnCompletedPayload{}),
	)

	plan, err := NewReplayPlanner().Plan(fixture.loaded())
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.NativeCommits) != 3 || len(plan.SampleUsages) != 2 || len(plan.Turns) != 1 ||
		plan.Turns[0].State != ReplayedTurnCompleted || plan.ToolRecovery != nil || plan.InterruptedTail != nil {
		t.Fatalf("tool loop replay plan = %#v", plan)
	}
}

func TestReplayPlannerReportsToolRecoveryStates(t *testing.T) {
	for _, test := range []struct {
		name             string
		appendTail       func(*replayFixture, tool.ReadInvocation)
		wantState        ReplayedToolCallState
		outputsCommitted bool
		wantCalls        int
	}{
		{name: "ready", wantState: ReplayedToolCallReady, wantCalls: 1},
		{name: "started", wantState: ReplayedToolCallStarted, wantCalls: 1, appendTail: func(f *replayFixture, invocation tool.ReadInvocation) {
			f.appendBatch(mustStartedDraftForReplay(t, invocation))
		}},
		{name: "result", wantState: ReplayedToolCallResult, wantCalls: 1, appendTail: func(f *replayFixture, invocation tool.ReadInvocation) {
			f.appendBatch(mustStartedDraftForReplay(t, invocation))
			f.appendBatch(mustResultDraftForReplay(t, invocation))
		}},
		{name: "outputs committed", outputsCommitted: true, appendTail: func(f *replayFixture, invocation tool.ReadInvocation) {
			f.appendBatch(mustStartedDraftForReplay(t, invocation))
			f.appendBatch(mustResultDraftForReplay(t, invocation))
			f.appendBatch(mustDraft(t, EventProviderNativeCommit, testTurnID, validToolOutputsCommit()))
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newReplayFixture(t)
			fixture.appendMetadata()
			fixture.appendBatch(mustDraft(t, EventTurnStarted, testTurnID, TurnStartedPayload{}))
			invocation := testReplayInvocation(t, "01890f3e-7bcd-7abc-8abc-0123456789ab", "call-1")
			fixture.appendBatch(
				mustDraft(t, EventProviderNativeCommit, testTurnID, validNativeCommit()),
				mustDraft(t, EventSampleUsage, testTurnID, testSampleUsage(t)),
				mustReadyDraftForReplay(t, invocation, 0, 0),
			)
			if test.appendTail != nil {
				test.appendTail(fixture, invocation)
			}
			plan, err := NewReplayPlanner().Plan(fixture.loaded())
			if err != nil {
				t.Fatal(err)
			}
			if plan.ToolRecovery == nil || plan.ToolRecovery.TurnID() != testTurnID ||
				plan.ToolRecovery.NextSampleIndex() != 1 || plan.ToolRecovery.ToolOutputsCommitted() != test.outputsCommitted ||
				len(plan.ToolRecovery.Calls()) != test.wantCalls {
				t.Fatalf("tool recovery = %#v", plan.ToolRecovery)
			}
			calls := plan.ToolRecovery.Calls()
			if test.wantCalls == 1 && calls[0].State != test.wantState {
				t.Fatalf("tool recovery call = %#v", calls[0])
			}
		})
	}
}

func TestReplayPlannerAcceptsCancellationBeforeExecutorStart(t *testing.T) {
	fixture := newReplayFixture(t)
	fixture.appendMetadata()
	fixture.appendBatch(mustDraft(t, EventTurnStarted, testTurnID, TurnStartedPayload{}))
	invocation := testReplayInvocation(t, "01890f3e-7bcd-7abc-8abc-0123456789ab", "call-1")
	appendReplayCallSample(t, fixture, invocation)
	cancelled := tool.NewReadErrorResult(invocation, tool.ResultCancelled, "cancelled", "Read cancelled", ".")
	draft, err := NewToolCallResultDraft(testTurnID, cancelled)
	if err != nil {
		t.Fatal(err)
	}
	fixture.appendBatch(draft)

	plan, err := NewReplayPlanner().Plan(fixture.loaded())
	if err != nil {
		t.Fatal(err)
	}
	if plan.ToolRecovery == nil {
		t.Fatal("tool recovery is missing")
	}
	calls := plan.ToolRecovery.Calls()
	if len(calls) != 1 ||
		calls[0].State != ReplayedToolCallResult ||
		calls[0].Result.Status() != tool.ResultCancelled {
		t.Fatalf("tool recovery = %#v", plan.ToolRecovery)
	}
}

func TestToolRecoveryPlanOwnsAndValidatesCalls(t *testing.T) {
	invocation := testReplayInvocation(t, "01890f3e-7bcd-7abc-8abc-0123456789ab", "call-1")
	calls := []ReplayedToolCall{{
		ReadySequence: 4, SampleIndex: 0, CallIndex: 0,
		Invocation: invocation, State: ReplayedToolCallReady,
	}}
	plan, err := NewToolRecoveryPlan(testTurnID, 1, calls, false)
	if err != nil {
		t.Fatal(err)
	}
	calls[0].State = ReplayedToolCallStarted
	got := plan.Calls()
	got[0].State = ReplayedToolCallResult
	if plan.Calls()[0].State != ReplayedToolCallReady {
		t.Fatal("tool recovery plan shares caller-owned calls")
	}
	if plan.Clone().Calls()[0].Invocation.InvocationID() != invocation.InvocationID() {
		t.Fatal("tool recovery clone changed invocation identity")
	}

	invalid := []struct {
		name      string
		calls     []ReplayedToolCall
		committed bool
	}{
		{name: "missing calls"},
		{name: "committed with calls", calls: calls, committed: true},
		{name: "started without sequence", calls: []ReplayedToolCall{{
			ReadySequence: 4, SampleIndex: 0, CallIndex: 0,
			Invocation: invocation, State: ReplayedToolCallStarted,
		}}},
	}
	for _, test := range invalid {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewToolRecoveryPlan(testTurnID, 1, test.calls, test.committed); err == nil {
				t.Fatal("invalid recovery plan was accepted")
			}
		})
	}
}

func TestReplayPlannerRejectsIllegalToolLedgerTransitions(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*replayFixture, tool.ReadInvocation)
	}{
		{name: "started before ready", mutate: func(f *replayFixture, invocation tool.ReadInvocation) {
			f.appendBatch(mustStartedDraftForReplay(t, invocation))
		}},
		{name: "result before started", mutate: func(f *replayFixture, invocation tool.ReadInvocation) {
			appendReplayCallSample(t, f, invocation)
			f.appendBatch(mustResultDraftForReplay(t, invocation))
		}},
		{name: "output before result", mutate: func(f *replayFixture, invocation tool.ReadInvocation) {
			appendReplayCallSample(t, f, invocation)
			f.appendBatch(mustDraft(t, EventProviderNativeCommit, testTurnID, validToolOutputsCommit()))
		}},
		{name: "next sample before output", mutate: func(f *replayFixture, invocation tool.ReadInvocation) {
			appendReplayCallSample(t, f, invocation)
			f.appendBatch(mustStartedDraftForReplay(t, invocation))
			f.appendBatch(mustResultDraftForReplay(t, invocation))
			f.appendBatch(
				mustDraft(t, EventProviderNativeCommit, testTurnID, validNativeCommit()),
				mustDraft(t, EventSampleUsage, testTurnID, testSampleUsage(t)),
				mustDraft(t, EventTurnCompleted, testTurnID, TurnCompletedPayload{}),
			)
		}},
		{name: "terminal with pending calls", mutate: func(f *replayFixture, invocation tool.ReadInvocation) {
			appendReplayCallSample(t, f, invocation)
			f.appendBatch(mustDraft(t, EventTurnFailed, testTurnID, TurnFailedPayload{Code: "failed"}))
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newReplayFixture(t)
			fixture.appendMetadata()
			fixture.appendBatch(mustDraft(t, EventTurnStarted, testTurnID, TurnStartedPayload{}))
			invocation := testReplayInvocation(t, "01890f3e-7bcd-7abc-8abc-0123456789ab", "call-1")
			test.mutate(fixture, invocation)
			if _, err := NewReplayPlanner().Plan(fixture.loaded()); err == nil || !strings.Contains(err.Error(), "corrupted") {
				t.Fatalf("Plan() error = %v", err)
			}
		})
	}
}

func appendReplayCallSample(t *testing.T, fixture *replayFixture, invocation tool.ReadInvocation) {
	t.Helper()
	fixture.appendBatch(
		mustDraft(t, EventProviderNativeCommit, testTurnID, validNativeCommit()),
		mustDraft(t, EventSampleUsage, testTurnID, testSampleUsage(t)),
		mustReadyDraftForReplay(t, invocation, 0, 0),
	)
}

func testReplayInvocation(t *testing.T, invocationValue string, callValue string) tool.ReadInvocation {
	t.Helper()
	invocationID, err := tool.ParseInvocationID(invocationValue)
	if err != nil {
		t.Fatal(err)
	}
	callID, err := tool.ParseProviderCallID(callValue)
	if err != nil {
		t.Fatal(err)
	}
	input, _ := tool.NewReadInput("README.md", 1, 20)
	ready, _ := tool.NewReadyCall(callID, input)
	invocation, err := tool.NewReadInvocation(invocationID, ready)
	if err != nil {
		t.Fatal(err)
	}
	return invocation
}

func mustReadyDraftForReplay(t *testing.T, invocation tool.ReadInvocation, sampleIndex uint32, callIndex uint32) RecordDraft {
	t.Helper()
	draft, err := NewToolCallReadyDraft(testTurnID, invocation, sampleIndex, callIndex)
	if err != nil {
		t.Fatal(err)
	}
	return draft
}

func mustStartedDraftForReplay(t *testing.T, invocation tool.ReadInvocation) RecordDraft {
	t.Helper()
	draft, err := NewToolExecutionStartedDraft(testTurnID, invocation.InvocationID())
	if err != nil {
		t.Fatal(err)
	}
	return draft
}

func mustResultDraftForReplay(t *testing.T, invocation tool.ReadInvocation) RecordDraft {
	t.Helper()
	result := tool.RenderReadSuccess(invocation, "README.md", []string{"content"}, 1)
	draft, err := NewToolCallResultDraft(testTurnID, result)
	if err != nil {
		t.Fatal(err)
	}
	return draft
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
		Payload: json.RawMessage(`{"kind":"sample"}`),
	}
}

func validToolOutputsCommit() NativeCommitPayload {
	return NativeCommitPayload{
		Provider: domain.ProviderOpenAI, Wire: "responses", PayloadVersion: 1,
		Payload: json.RawMessage(`{"kind":"tool_outputs"}`),
	}
}
