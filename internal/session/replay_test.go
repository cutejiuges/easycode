package session

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"easycode/internal/domain"
	"easycode/internal/tool"
)

func TestReplayPlannerRejectsParallelCompletionOrderPersistence(t *testing.T) {
	for _, test := range []struct {
		name      string
		want      string
		lateStart bool
	}{
		{name: "out-of-order-result", want: "order"},
		{name: "late-start-before-committed-outputs", want: "read result preceded group admission", lateStart: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newReplayFixture(t)
			fixture.appendMetadata()
			fixture.appendBatch(mustDraft(t, EventTurnStarted, testTurnID, TurnStartedPayload{}))
			fixture.appendInitialInputs(testTurnID)
			read := testReplayInvocation(t, "01890f3e-7bcd-7abc-8abc-0123456789ab", "call-0")
			glob := testReplayGlobInvocation(t, "01890f3e-7bcd-7abc-8abc-0123456789ac", "call-1")
			fixture.appendBatch(
				mustDraft(t, EventProviderNativeCommit, testTurnID, validNativeCommit()),
				mustDraft(t, EventSampleUsage, testTurnID, testSampleUsage(t)),
				mustReadyDraftForReplay(t, read, 0, 0),
				mustReadyDraftForReplay(t, glob, 0, 1),
			)
			fixture.appendBatch(mustStartedDraftForReplay(t, read))
			if test.lateStart {
				fixture.appendBatch(mustResultDraftForReplay(t, read))
			}
			fixture.appendBatch(mustStartedDraftForReplay(t, glob))
			fixture.appendBatch(mustResultDraftForReplay(t, glob))
			if test.lateStart {
				fixture.appendBatch(mustDraft(t, EventProviderNativeCommit, testTurnID, validToolOutputsCommit()))
			}
			if _, err := NewReplayPlanner().Plan(fixture.loaded()); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("unexpected replay error: %v", err)
			}
		})
	}
}

func TestReplayPlannerReportsToolRecoveryStates(t *testing.T) {
	for _, test := range []struct {
		name             string
		appendTail       func(*replayFixture, tool.Invocation)
		wantState        ReplayedToolCallState
		outputsCommitted bool
		wantCalls        int
	}{
		{name: "ready", wantState: ReplayedToolCallReady, wantCalls: 1},
		{name: "started", wantState: ReplayedToolCallStarted, wantCalls: 1, appendTail: func(f *replayFixture, invocation tool.Invocation) {
			f.appendBatch(mustStartedDraftForReplay(t, invocation))
		}},
		{name: "result", wantState: ReplayedToolCallResult, wantCalls: 1, appendTail: func(f *replayFixture, invocation tool.Invocation) {
			f.appendBatch(mustStartedDraftForReplay(t, invocation))
			f.appendBatch(mustResultDraftForReplay(t, invocation))
		}},
		{name: "outputs committed", outputsCommitted: true, appendTail: func(f *replayFixture, invocation tool.Invocation) {
			f.appendBatch(mustStartedDraftForReplay(t, invocation))
			f.appendBatch(mustResultDraftForReplay(t, invocation))
			f.appendBatch(mustDraft(t, EventProviderNativeCommit, testTurnID, validToolOutputsCommit()))
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newReplayFixture(t)
			fixture.appendMetadata()
			fixture.appendBatch(mustDraft(t, EventTurnStarted, testTurnID, TurnStartedPayload{}))
			fixture.appendInitialInputs(testTurnID)
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
			requireNoError(t, err)
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

func testReplayInvocation(t *testing.T, invocationValue string, callValue string) tool.Invocation {
	t.Helper()
	invocationID, err := tool.ParseInvocationID(invocationValue)
	requireNoError(t, err)
	callID, err := tool.ParseProviderCallID(callValue)
	requireNoError(t, err)
	input, _ := tool.NewReadInput("README.md", 1, 20)
	ready, _ := tool.NewReadReadyCall(callID, input)
	invocation, err := tool.NewInvocation(invocationID, ready)
	requireNoError(t, err)
	return invocation
}

func testReplayGlobInvocation(t *testing.T, invocationValue string, callValue string) tool.Invocation {
	t.Helper()
	invocationID, err := tool.ParseInvocationID(invocationValue)
	requireNoError(t, err)
	callID, err := tool.ParseProviderCallID(callValue)
	requireNoError(t, err)
	input, _ := tool.NewGlobInput("**/*.go", "", 100)
	ready, _ := tool.NewGlobReadyCall(callID, input)
	invocation, err := tool.NewInvocation(invocationID, ready)
	requireNoError(t, err)
	return invocation
}

func mustPatchApprovalDraft(t *testing.T, invocation tool.Invocation, fingerprint tool.CandidateFingerprint, outcome tool.ApprovalOutcome) RecordDraft {
	t.Helper()
	metadata, err := NewApprovalMetadata(invocation)
	requireNoError(t, err)
	source := ApprovalSourceInteractive
	if outcome == tool.ApprovalOutcomeAllowFromSession {
		source = ApprovalSourceSessionGrant
	}
	requestID, _ := tool.ParseApprovalRequestID("018f1d8a-7b5c-7def-8abc-456789abcdef")
	draft, err := NewToolApprovalDecidedDraft(testTurnID, ToolApprovalDecidedPayload{
		RequestID: requestID, InvocationID: invocation.InvocationID(), Fingerprint: fingerprint, Outcome: outcome, Source: source, Metadata: metadata,
	})
	requireNoError(t, err)
	return draft
}

func mustReadyDraftForReplay(t *testing.T, invocation tool.Invocation, sampleIndex uint32, callIndex uint32) RecordDraft {
	t.Helper()
	draft, err := NewToolCallReadyDraft(testTurnID, invocation, sampleIndex, callIndex)
	requireNoError(t, err)
	return draft
}

func mustStartedDraftForReplay(t *testing.T, invocation tool.Invocation) RecordDraft {
	t.Helper()
	draft, err := NewToolExecutionStartedDraft(testTurnID, invocation.InvocationID())
	requireNoError(t, err)
	return draft
}

func mustResultDraftForReplay(t *testing.T, invocation tool.Invocation) RecordDraft {
	t.Helper()
	var result tool.InvocationResult
	switch invocation.Capability() {
	case tool.CapabilityRead:
		readInvocation, _ := invocation.Read()
		result = tool.RenderReadSuccess(readInvocation, "README.md", []string{"content"}, 1)
	case tool.CapabilityGlob:
		globInvocation, _ := invocation.Glob()
		metadata, _ := tool.NewGlobResultMetadata([]string{"main.go"}, false, 0, 1, tool.SearchComplete, tool.SearchSkipCounts{})
		preview, _ := tool.NewModelPreview("main.go\n")
		result, _ = tool.NewGlobInvocationResult(globInvocation, tool.ResultSuccess, "ok", preview, metadata)
	default:
		t.Fatal("unsupported replay invocation capability")
	}
	draft, err := NewToolCallResultDraft(testTurnID, result)
	requireNoError(t, err)
	return draft
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
		CreationCWD: fixture.cwd,
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

func (fixture *replayFixture) loaded() LoadResult {
	return LoadResult{
		Identity: Identity{SessionID: testSessionID, ThreadID: testThreadID},
		Records:  cloneRecords(fixture.records), NextSequence: fixture.next,
	}
}

func validNativeCommit() NativeCommitPayload {
	return NativeCommitPayload{
		Provider: domain.ProviderOpenAI, Wire: "responses", Payload: json.RawMessage(`{"kind":"sample"}`),
	}
}

func validToolOutputsCommit() NativeCommitPayload {
	return NativeCommitPayload{
		Provider: domain.ProviderOpenAI, Wire: "responses", Payload: json.RawMessage(`{"kind":"tool_outputs"}`),
	}
}

func (fixture *replayFixture) appendInitialInputs(turnID domain.TurnID) {
	fixture.t.Helper()
	association, err := domain.NewInputAssociation(turnID, domain.InputInitial, 0, []domain.InputID{domain.InputID(turnID)})
	if err != nil {
		fixture.t.Fatal(err)
	}
	draft, err := NewTurnInputsCommittedDraft(association)
	if err != nil {
		fixture.t.Fatal(err)
	}
	fixture.appendBatch(draft, mustDraft(fixture.t, EventProviderNativeCommit, turnID, NativeCommitPayload{Provider: domain.ProviderOpenAI, Wire: "responses", Payload: json.RawMessage(`{"kind":"user_inputs"}`)}))
}
