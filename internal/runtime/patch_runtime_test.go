package runtime

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"easycode/internal/protocol"
	"easycode/internal/session"
	"easycode/internal/tool"
)

type fixedPatchPreparer struct {
	candidate tool.PatchCandidate
	calls     int
	onPrepare func()
}

func (preparer *fixedPatchPreparer) Prepare(context.Context, tool.PatchInvocation) (tool.PatchCandidate, error) {
	preparer.calls++
	if preparer.onPrepare != nil {
		preparer.onPrepare()
	}
	return preparer.candidate, nil
}

type fixedPatchExecutor struct {
	outcome   tool.ResultStatus
	calls     int
	onExecute func()
}

func (executor *fixedPatchExecutor) Execute(_ context.Context, candidate tool.PatchCandidate) tool.InvocationResult {
	executor.calls++
	if executor.onExecute != nil {
		executor.onExecute()
	}
	if executor.outcome != "" {
		return postPatchOutcome{executor.outcome}.Execute(context.Background(), candidate)
	}
	return tool.NewPatchSuccessResult(candidate)
}

type allowPatchPolicy struct{}

func (allowPatchPolicy) Decide(call tool.ReadyCall) tool.PolicyDecision {
	if call.Validate() == nil {
		return tool.PolicyAllow
	}
	return tool.PolicyDeny
}

type timelineReadExecutor struct {
	mu       *sync.Mutex
	timeline *[]string
}

func (executor timelineReadExecutor) Execute(_ context.Context, invocation tool.ReadInvocation) tool.ReadExecution {
	executor.mu.Lock()
	*executor.timeline = append(*executor.timeline, "read:"+string(invocation.ProviderCallID()))
	executor.mu.Unlock()
	result, _ := tool.NewReadExecution(tool.RenderReadSuccess(invocation, "README.md", []string{"content"}, 1), tool.UnavailableReadObservation())
	return result
}

func runtimePatchInvocation(t *testing.T) (tool.Invocation, tool.PatchCandidate) {
	t.Helper()
	replacement, _ := tool.NewPatchLineReplacement([]string{"old"}, []string{"new"})
	input, _ := tool.NewPatchInput("main.go", []tool.PatchReplacement{replacement})
	callID, _ := tool.ParseProviderCallID("call-patch")
	ready, _ := tool.NewPatchReadyCall(callID, tool.WireCustomFreeform, input)
	invocationID, _ := tool.ParseInvocationID("018f1d8a-7b5c-7def-8123-456789abcdef")
	invocation, _ := tool.NewInvocation(invocationID, ready)
	patch, _ := invocation.Patch()
	base := []byte("old\n")
	evidence, _ := tool.NewFileBaseEvidence(tool.NewContentDigest(base), int64(len(base)), 0o644, 1, 2)
	metadata, _ := tool.NewPatchResultMetadata("main.go", 1, 1, 1)
	final := []byte("new\n")
	fingerprint, _ := tool.PatchFingerprint(input, evidence, final)
	candidate, err := tool.NewPatchCandidate(patch, evidence, final, "--- a/main.go\n+++ b/main.go\n-old\n+new\n", fingerprint, metadata)
	if err != nil {
		t.Fatal(err)
	}
	return invocation, candidate
}

func TestPatchApprovalDurabilityPrecedesExecution(t *testing.T) {
	invocation, candidate := runtimePatchInvocation(t)
	journal := &fakeJournal{}
	preparer := &fixedPatchPreparer{candidate: candidate}
	executor := &fixedPatchExecutor{}
	approvalID, _ := tool.ParseApprovalRequestID("018f1d8a-7b5c-7def-8abc-456789abcdef")
	runtime := &Runtime{journal: journal, sessionID: runtimeSessionID, threadID: runtimeThreadID, tools: tool.Components{PreparePatch: preparer, Patch: executor, Policy: tool.NewApprovalPolicy()}, newApprovalRequestID: func() (tool.ApprovalRequestID, error) { return approvalID, nil }, interactiveApprovals: true}
	events := make(chan protocol.Event, 2)
	type outcome struct {
		results []tool.InvocationResult
		err     error
	}
	done := make(chan outcome, 1)
	go func() {
		results, err := executeReadyFixtureCalls(t, runtime, context.Background(), runtimeTurnID, []tool.Invocation{invocation}, func(event protocol.Event) {
			if event.Kind == protocol.EventToolApprovalRequested || event.Kind == protocol.EventToolApprovalResolved {
				events <- event
			}
		})
		done <- outcome{results, err}
	}()
	requested := <-events
	payload, err := protocol.DecodeToolApprovalRequested(requested)
	if err != nil {
		t.Fatal(err)
	}
	response, err := tool.NewApprovalResponse(approvalID, runtimeTurnID, invocation.InvocationID(), candidate.Fingerprint(), tool.ApprovalAllowOnce, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.ResolveToolApproval(response); err != nil {
		t.Fatal(err)
	}
	resolved := <-events
	if _, err := protocol.DecodeToolApprovalResolved(resolved); err != nil {
		t.Fatal(err)
	}
	result := <-done
	if result.err != nil || len(result.results) != 1 || result.results[0].Status() != tool.ResultSuccess {
		t.Fatalf("result=%#v error=%v", result.results, result.err)
	}
	if payload.Fingerprint != string(candidate.Fingerprint()) || preparer.calls != 1 || executor.calls != 1 {
		t.Fatalf("payload=%#v calls=%d/%d", payload, preparer.calls, executor.calls)
	}
	batches := journal.snapshot()
	want := []session.EventKind{session.EventToolApprovalDecided, session.EventToolExecutionStarted, session.EventToolCallResult}
	if len(batches) != len(want) {
		t.Fatalf("batches=%#v", batches)
	}
	for index, kind := range want {
		if len(batches[index]) != 1 || batches[index][0].EventKind() != kind {
			t.Fatalf("batch[%d]=%#v", index, batches[index])
		}
	}
}

func TestPatchCreatesExclusiveBarrierBetweenReadBatches(t *testing.T) {
	patchInvocation, candidate := runtimePatchInvocation(t)
	reads := testRuntimeInvocations(t, 2)
	invocations := []tool.Invocation{reads[0], patchInvocation, reads[1]}
	var mu sync.Mutex
	var timeline []string
	appendTimeline := func(value string) {
		mu.Lock()
		timeline = append(timeline, value)
		mu.Unlock()
	}
	runtime := &Runtime{
		journal: &fakeJournal{}, sessionID: runtimeSessionID, threadID: runtimeThreadID,
		tools: tool.Components{
			Read:         timelineReadExecutor{mu: &mu, timeline: &timeline},
			PreparePatch: &fixedPatchPreparer{candidate: candidate, onPrepare: func() { appendTimeline("prepare") }},
			Patch:        &fixedPatchExecutor{onExecute: func() { appendTimeline("patch") }},
			Policy:       allowPatchPolicy{},
		},
		newApprovalRequestID: tool.GenerateApprovalRequestID,
	}
	results, err := executeReadyFixtureCalls(t, runtime, context.Background(), runtimeTurnID, invocations)
	if err != nil || len(results) != 3 {
		t.Fatalf("results=%#v error=%v", results, err)
	}
	want := []string{"read:call-0", "prepare", "patch", "read:call-1"}
	if fmt.Sprint(timeline) != fmt.Sprint(want) {
		t.Fatalf("timeline=%#v want=%#v", timeline, want)
	}
}
