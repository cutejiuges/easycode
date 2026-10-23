package runtime

import (
	"context"
	"reflect"
	"sync"
	"testing"

	"easycode/internal/protocol"
	"easycode/internal/session"
	"easycode/internal/tool"
)

type barrierGrepExecutor struct{ onExecute func() }

func (executor barrierGrepExecutor) Execute(_ context.Context, invocation tool.GrepInvocation) tool.InvocationResult {
	executor.onExecute()
	metadata, _ := tool.NewGrepResultMetadata(invocation.Input().OutputMode(), nil, 0, false, 0, 0, 0, 0, tool.SearchComplete, tool.SearchSkipCounts{})
	preview, _ := tool.NewModelPreview("No matches")
	result, _ := tool.NewGrepInvocationResult(invocation, tool.ResultSuccess, "ok", preview, metadata)
	return result
}

func TestExecBarrierWaitsForPriorDurabilityAndPreservesResultOrder(t *testing.T) {
	for _, patchFirst := range []bool{false, true} {
		var mu sync.Mutex
		var timeline []string
		add := func(value string) { mu.Lock(); timeline = append(timeline, value); mu.Unlock() }
		journal := &fakeJournal{hook: func(drafts []session.RecordDraft) {
			for _, draft := range drafts {
				if draft.EventKind() != session.EventToolCallResult {
					continue
				}
				payload, err := session.DecodeToolCallResultPayload(session.Record{PayloadVersion: session.EnvelopeVersion, EventKind: session.EventToolCallResult, Payload: draft.PayloadBytes()})
				if err != nil {
					t.Error(err)
					continue
				}
				add("durable:" + string(payload.Capability))
			}
		}}
		runtime, execInvocation, executor := runtimeExecFixture(t, journal)
		executor.onExecute = func(context.Context) { add("exec") }
		reads := testRuntimeInvocations(t, 1)
		runtime.readExecutor = timelineReadExecutor{mu: &mu, timeline: &timeline}
		input, _ := tool.NewGrepInput("needle", "", "", tool.GrepOutputFilesWithMatches, false, 0, 0, 10)
		call, _ := tool.NewGrepReadyCall("call-grep", input)
		id, _ := tool.ParseInvocationID("018f1d8a-7b5c-7def-8abc-456789abcdee")
		grep, err := tool.NewInvocation(id, call)
		if err != nil {
			t.Fatal(err)
		}
		runtime.grepExecutor = barrierGrepExecutor{onExecute: func() { add("grep") }}
		invocations := []tool.Invocation{reads[0], execInvocation, grep}
		want := []string{"read:call-0", "durable:fs.read", "exec", "durable:process.exec", "grep", "durable:fs.grep"}
		if patchFirst {
			patch, candidate := runtimePatchInvocation(t)
			runtime.patchPreparer = &fixedPatchPreparer{candidate: candidate}
			runtime.patchExecutor = &fixedPatchExecutor{onExecute: func() { add("patch") }}
			runtime.toolPolicy = allowPatchPolicy{}
			invocations = []tool.Invocation{patch, execInvocation}
			want = []string{"patch", "durable:fs.patch", "exec", "durable:process.exec"}
		}
		var events []protocol.Event
		results, err := runtime.executeToolCalls(context.Background(), runtimeTurnID, invocations, execApprovalEmitter(t, runtime, tool.ApprovalAllowOnce, &events))
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(timeline, want) {
			t.Fatalf("barrier timeline = %v, want %v", timeline, want)
		}
		if len(results) != len(invocations) {
			t.Fatal("missing results")
		}
		for index, result := range results {
			if result.Validate() != nil || result.InvocationID() != invocations[index].InvocationID() || result.ProviderCallID() != invocations[index].ProviderCallID() {
				t.Fatal("result order or pairing changed")
			}
		}
	}
}

func TestExecAbortCancelsRemainingCallsWithoutPatchSpecificPreview(t *testing.T) {
	runtime, invocation, executor := runtimeExecFixture(t, &fakeJournal{})
	var mu sync.Mutex
	var reads []string
	runtime.readExecutor = timelineReadExecutor{mu: &mu, timeline: &reads}
	following := testRuntimeInvocations(t, 1)[0]
	var events []protocol.Event
	results, err := runtime.executeToolCalls(context.Background(), runtimeTurnID, []tool.Invocation{invocation, following}, execApprovalEmitter(t, runtime, tool.ApprovalAbort, &events))
	if err == nil || len(results) != 2 || executor.calls != 0 || len(reads) != 0 {
		t.Fatalf("abort did not close both calls without execution: %v, %v", results, err)
	}
	for index, result := range results {
		if result.Validate() != nil || result.Status() != tool.ResultCancelled || result.Code() != "user_aborted" {
			t.Fatalf("cancelled result %d is invalid: %v", index, result)
		}
	}
	if results[1].InvocationID() != following.InvocationID() || results[1].ProviderCallID() != following.ProviderCallID() || results[1].Preview().Text() != "Tool cancelled: user aborted tool approval" {
		t.Fatalf("following result lost pairing or described the wrong approval: %v", results[1])
	}
}
