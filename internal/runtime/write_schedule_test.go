package runtime

import (
	"context"
	"easycode/internal/protocol"
	"easycode/internal/tool"
	"fmt"
	"sync"
	"testing"
)

type observedTimelineRead struct {
	mu       *sync.Mutex
	timeline *[]string
}

func (e observedTimelineRead) Execute(_ context.Context, v tool.ReadInvocation) tool.ReadExecution {
	e.mu.Lock()
	*e.timeline = append(*e.timeline, "read:"+string(v.ProviderCallID()))
	e.mu.Unlock()
	result := tool.RenderReadSuccess(v, v.Input().FilePath(), []string{"data"}, 1)
	evidence, _ := tool.NewFileBaseEvidence(tool.NewContentDigest([]byte("data")), 4, 0600, 1, 2)
	observation, _ := tool.NewCompleteReadObservation(result, evidence)
	execution, _ := tool.NewReadExecution(result, observation)
	return execution
}

type timelineGrep struct{ append func(string) }

func (e timelineGrep) Execute(_ context.Context, v tool.GrepInvocation) tool.InvocationResult {
	e.append("grep")
	ready, _ := tool.NewGrepReadyCall(v.ProviderCallID(), v.Input())
	invocation, _ := tool.NewInvocation(v.InvocationID(), ready)
	return tool.NewInvocationErrorResult(invocation, tool.ResultError, "grep_failed", "Grep failed")
}

func TestWritePatchReadGrepBarriersPromoteOnlyFreshRead(t *testing.T) {
	for _, readAfterPatch := range []bool{false, true} {
		t.Run(fmt.Sprint(readAfterPatch), func(t *testing.T) {
			write, c := runtimeWriteInvocation(t)
			patch, pc := runtimePatchInvocation(t)
			read := func(id string) tool.Invocation {
				input, _ := tool.NewReadInput("main.go", 1, 2000)
				ready, _ := tool.NewReadReadyCall(tool.ProviderCallID(id), input)
				identity, _ := tool.GenerateInvocationID()
				v, _ := tool.NewInvocation(identity, ready)
				return v
			}
			gi, _ := tool.NewGrepInput("data", "", "", tool.GrepOutputContent, false, 0, 0, 10)
			gr, _ := tool.NewGrepReadyCall("grep", gi)
			id, _ := tool.GenerateInvocationID()
			grep, _ := tool.NewInvocation(id, gr)
			calls := []tool.Invocation{read("before"), write, patch}
			if readAfterPatch {
				calls = append(calls, read("after"))
			}
			calls = append(calls, grep)
			var mu sync.Mutex
			var timeline []string
			appendEvent := func(s string) { mu.Lock(); timeline = append(timeline, s); mu.Unlock() }
			executor := &fixedWriteExecutor{onExecute: func(context.Context) { appendEvent("write") }}
			runtime := writeTestRuntime(t, &fakeJournal{}, c, executor)
			runtime.toolPolicy = allowPatchPolicy{}
			runtime.readExecutor = observedTimelineRead{&mu, &timeline}
			runtime.grepExecutor = timelineGrep{appendEvent}
			runtime.patchPreparer = &fixedPatchPreparer{candidate: pc, onPrepare: func() { appendEvent("patch-prepare") }}
			runtime.patchExecutor = &fixedPatchExecutor{onExecute: func() { appendEvent("patch") }}
			results, err := runtime.executeToolCalls(context.Background(), runtimeTurnID, calls)
			if err != nil {
				t.Fatal(err)
			}
			for i, r := range results {
				if r.InvocationID() != calls[i].InvocationID() {
					t.Fatal("结果打乱 model call index")
				}
			}
			if len(timeline) < 5 || fmt.Sprint(timeline[:4]) != "[read:before write patch-prepare patch]" {
				t.Fatal("独占调用越过只读或互相重叠", timeline)
			}
			if runtime.readObservations.lookup("main.go").Kind() != tool.ReadObservationUnavailable {
				t.Fatal("同 sample 观察提前激活")
			}
			if err := runtime.commitToolOutputs(context.Background(), runtimeTurnID, results); err != nil {
				t.Fatal(err)
			}
			want := tool.ReadObservationUnavailable
			if readAfterPatch {
				want = tool.ReadObservationComplete
			}
			if runtime.readObservations.lookup("main.go").Kind() != want {
				t.Fatal("Patch generation 没有失效旧 Read/接受新 Read")
			}
			runtime.clearTurnObservations()
			if runtime.readObservations.lookup("main.go").Kind() != want {
				t.Fatal("正常跨 turn 丢失有效观察")
			}
			runtime.approvals.writeGrant.Store(true)
			runtime.approvals.patchGrant.Store(true)
			chat := NewChatSession(runtime)
			if err := chat.Start(context.Background()); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = chat.Shutdown(context.Background()) })
			if err := chat.Shutdown(context.Background()); err != nil {
				t.Fatal(err)
			}
			if len(runtime.readObservations.current) != 0 || runtime.approvals.writeGrant.Load() || runtime.approvals.patchGrant.Load() {
				t.Fatal("shutdown 保留文件状态")
			}
		})
	}
}

func TestWriteDenialAndConflictDoNotCancelIndependentSibling(t *testing.T) {
	for _, status := range []tool.ResultStatus{tool.ResultDenied, tool.ResultConflict} {
		t.Run(string(status), func(t *testing.T) {
			invocation, c := runtimeWriteInvocation(t)
			executor := &fixedWriteExecutor{status: status}
			runtime := writeTestRuntime(t, &fakeJournal{}, c, executor)
			emit := approveWrite(t, runtime, tool.ApprovalDeny)
			if status == tool.ResultConflict {
				runtime.toolPolicy = allowPatchPolicy{}
				emit = func(protocol.Event) {}
			}
			reader := &recordingReadExecutor{}
			runtime.readExecutor = reader
			result, observation := completeObservation(t, "new-file")
			runtime.readObservations.beginGroup()
			runtime.readObservations.stage(result, observation)
			runtime.readObservations.activate()
			results, err := runtime.executeToolCalls(context.Background(), runtimeTurnID, []tool.Invocation{invocation, testRuntimeInvocations(t, 1)[0]}, emit)
			if err != nil || len(results) != 2 || results[0].Status() != status || reader.calls.Load() != 1 || results[1].Status() != tool.ResultSuccess {
				t.Fatal("拒绝或冲突取消独立 sibling", err)
			}
			if status == tool.ResultConflict && runtime.readObservations.lookup("new-file").Kind() != tool.ReadObservationUnavailable {
				t.Fatal("Write started/conflict 未清除观察")
			}
		})
	}
}
