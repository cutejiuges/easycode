package runtime

import (
	"context"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/session"
	"easycode/internal/tool"
)

// seedRuntimeToolCall 为独立于 sample 编排的单元测试提供明确的已准入 ready 来源。
func seedRuntimeToolCall(t *testing.T, r *Runtime, turn domain.TurnID, inv tool.Invocation, sample, call uint32) {
	t.Helper()
	if !r.sessionID.Valid() {
		r.sessionID = runtimeSessionID
	}
	if !r.threadID.Valid() {
		r.threadID = runtimeThreadID
	}
	origin, err := tool.NewToolCallOrigin(r.sessionID, r.threadID, turn, sample, call)
	if err != nil {
		t.Fatal(err)
	}
	if existing := r.toolTurn.toolCalls[inv.InvocationID()]; existing != nil {
		if existing.origin != origin || !existing.original.Equal(inv) {
			t.Fatal("fixture call source mismatch")
		}
		return
	}
	if err := r.registerToolCall(origin, inv); err != nil {
		t.Fatal(err)
	}
}

// executeReadyFixtureCalls 给独立调度测试显式提供真实调用形状及组内位置。
// 它不重置已存在调用，重复身份仍由生产重入与准入规则拒绝。
func executeReadyFixtureCalls(t *testing.T, r *Runtime, ctx context.Context, turn domain.TurnID, invocations []tool.Invocation, emitters ...Emitter) ([]tool.InvocationResult, error) {
	t.Helper()
	var sample uint32
	for _, state := range r.toolTurn.toolCalls {
		sample = max(sample, state.origin.SampleIndex+1)
	}
	for i, inv := range invocations {
		if r.toolTurn.toolCalls[inv.InvocationID()] == nil {
			seedRuntimeToolCall(t, r, turn, inv, sample, uint32(i))
		}
	}
	return r.executeToolCalls(ctx, turn, invocations, emitters...)
}

func runtimeFixtureOrigin(t *testing.T, sample, call uint32) tool.ToolCallOrigin {
	t.Helper()
	origin, err := tool.NewToolCallOrigin(runtimeSessionID, runtimeThreadID, runtimeTurnID, sample, call)
	if err != nil {
		t.Fatal(err)
	}
	return origin
}

// bindRuntimeRecoveryFixture 让独立恢复测试使用固定 journal 的真实会话身份。
func bindRuntimeRecoveryFixture(t *testing.T, r *Runtime, plan session.ToolRecoveryPlan) {
	t.Helper()
	calls := plan.Calls()
	if len(calls) == 0 {
		return
	}
	if calls[0].Origin.Validate() != nil {
		t.Fatal("recovery fixture has no origin")
	}
	r.sessionID = calls[0].Origin.SessionID
	r.threadID = calls[0].Origin.ThreadID
}
