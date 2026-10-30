package runtime

import (
	"context"
	"fmt"
	"testing"

	"easycode/internal/provider"
	"easycode/internal/session"
	"easycode/internal/tool"
)

func completeObservation(t *testing.T, path string) (tool.InvocationResult, tool.ReadObservation) {
	t.Helper()
	id, _ := tool.GenerateInvocationID()
	input, _ := tool.NewReadInput(path, 1, 2000)
	call, _ := tool.NewReadReadyCall(tool.ProviderCallID("read-"+path), input)
	v, _ := tool.NewReadInvocation(id, call)
	r := tool.RenderReadSuccess(v, path, []string{"data"}, 1)
	evidence, _ := tool.NewFileBaseEvidence(tool.NewContentDigest([]byte("data")), 4, 0o600, 1, 2)
	o, err := tool.NewCompleteReadObservation(r, evidence)
	if err != nil {
		t.Fatal(err)
	}
	return r, o
}

func TestReadObservationPromotionGenerationAndEviction(t *testing.T) {
	var table readObservations
	r, o := completeObservation(t, "a")
	table.beginGroup()
	table.stage(r, o)
	if table.lookup("a").Kind() != tool.ReadObservationUnavailable {
		t.Fatal("同 sample 的 Read 提前生效")
	}
	table.invalidate("a")
	table.activate()
	if table.lookup("a").Kind() != tool.ReadObservationUnavailable {
		t.Fatal("Read 后 Patch 重新激活旧证据")
	}
	table.beginGroup()
	table.invalidate("a")
	table.stage(r, o)
	table.activate()
	if table.lookup("a") != o {
		t.Fatal("Patch 后 Read 没有生效")
	}
	table.beginGroup()
	table.stage(r, tool.UnavailableReadObservation())
	table.activate()
	if table.lookup("a").Kind() != tool.ReadObservationUnavailable {
		t.Fatal("失败或截断 Read 未使证据失效")
	}
	for i := 0; i < maxReadObservations+1; i++ {
		r, o := completeObservation(t, fmt.Sprintf("path-%d", i))
		table.beginGroup()
		table.stage(r, o)
		table.activate()
	}
	if len(table.current) != maxReadObservations || len(table.order) != maxReadObservations || table.lookup("path-0").Kind() != tool.ReadObservationUnavailable || table.lookup("path-1").Kind() != tool.ReadObservationComplete {
		t.Fatal("提交顺序淘汰错误")
	}
	r, o = completeObservation(t, "path-1")
	table.beginGroup()
	table.stage(r, o)
	table.activate()
	r, o = completeObservation(t, "another")
	table.beginGroup()
	table.stage(r, o)
	table.activate()
	if table.lookup("path-1").Kind() != tool.ReadObservationComplete || table.lookup("path-2").Kind() != tool.ReadObservationUnavailable {
		t.Fatal("最新完整 Read 未刷新提交顺序")
	}
	table.clear()
	if len(table.current) != 0 || len(table.pending) != 0 {
		t.Fatal("清理后保留证据")
	}
}

func TestReadObservationActivatesAfterOutputDurabilityAndFinalization(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprint(fail), func(t *testing.T) {
			journal := &startedFailureJournal{}
			if fail {
				journal.failAt = 2
			}
			conversation := &fakeToolConversation{fakeConversation: &fakeConversation{}}
			runtime := newTestRuntime(t, conversation, journal)
			r, o := completeObservation(t, "a")
			runtime.readObservations.beginGroup()
			if err := runtime.persistToolResult(context.Background(), runtimeTurnID, &r); err != nil {
				t.Fatal(err)
			}
			runtime.readObservations.stage(r, o)
			conversation.onFinalize = func() {
				if runtime.readObservations.lookup("a").Kind() != tool.ReadObservationUnavailable {
					t.Error("finalizer 返回前观察生效")
				}
			}
			err := runtime.commitToolOutputs(context.Background(), runtimeTurnID, []tool.InvocationResult{r})
			if (err != nil) != fail {
				t.Fatal("output 提交状态错误", err)
			}
			if fail {
				runtime.clearTurnObservations()
				if runtime.readObservations.lookup("a").Kind() != tool.ReadObservationUnavailable || len(runtime.readObservations.pending) != 0 {
					t.Fatal("output 失败仍保留观察")
				}
			} else if runtime.readObservations.lookup("a") != o {
				t.Fatal("durable/finalize 后未激活观察")
			}
		})
	}
}

type heldToolOutputsConversation struct {
	*fakeToolConversation
	prepared *provider.PreparedToolOutputs
}

func (c *heldToolOutputsConversation) PrepareToolOutputs(results []tool.InvocationResult) (*provider.PreparedToolOutputs, error) {
	p, err := c.fakeToolConversation.PrepareToolOutputs(results)
	c.prepared = p
	return p, err
}
func TestReadObservationDoesNotActivateWhenOutputFinalizeFails(t *testing.T) {
	journal := &fakeJournal{}
	conversation := &heldToolOutputsConversation{fakeToolConversation: &fakeToolConversation{fakeConversation: &fakeConversation{}}}
	r := newTestRuntime(t, conversation, journal)
	result, observation := completeObservation(t, "a")
	r.readObservations.beginGroup()
	r.readObservations.stage(result, observation)
	journal.hook = func(batch []session.RecordDraft) {
		if batch[0].EventKind() == session.EventProviderNativeCommit {
			if err := conversation.prepared.Discard(); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := r.commitToolOutputs(context.Background(), runtimeTurnID, []tool.InvocationResult{result}); err == nil {
		t.Fatal("finalizer failure 被忽略")
	}
	r.clearTurnObservations()
	if r.readObservations.lookup("a").Kind() != tool.ReadObservationUnavailable || len(r.readObservations.pending) != 0 || !r.poisoned.Load() {
		t.Fatal("finalizer 失败仍激活观察")
	}
}

func TestFailedReadWithoutTrustedPathInvalidatesAllObservations(t *testing.T) {
	var table readObservations
	active, activeObservation := completeObservation(t, "a")
	table.beginGroup()
	table.stage(active, activeObservation)
	table.activate()
	pending, pendingObservation := completeObservation(t, "b")
	table.beginGroup()
	table.stage(pending, pendingObservation)
	input, _ := tool.NewReadInput("a", 1, 2000)
	ready, _ := tool.NewReadReadyCall("cancelled-read", input)
	id, _ := tool.GenerateInvocationID()
	invocation, _ := tool.NewReadInvocation(id, ready)
	cancelled := tool.NewReadErrorResult(invocation, tool.ResultCancelled, "cancelled", "Read cancelled", ".")
	table.stage(cancelled, tool.UnavailableReadObservation())
	table.activate()
	if table.lookup("a").Kind() != tool.ReadObservationUnavailable || table.lookup("b").Kind() != tool.ReadObservationUnavailable {
		t.Fatal("没有规范路径的失败 Read 遗留了旧授权")
	}
}
