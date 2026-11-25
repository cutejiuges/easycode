package runtime

import (
	"bytes"
	"strings"
	"testing"

	"easycode/internal/context/estimate"
	"easycode/internal/domain"
	"easycode/internal/provider"
	"easycode/internal/session"
)

func TestPostCompactCompletionFallsBackWhenOnlyThePlanExceedsRecordBudget(t *testing.T) {
	source, err := provider.NewCompactionSource(domain.ProviderOpenAI, domain.OpenAIResponsesWire, "model", 2, 2, provider.RootCompactionPredecessor(), strings.Repeat("a", 64))
	requireNoError(t, err)
	id, err := domain.ParseCompactionID("01900000-0000-7000-8000-000000000009")
	requireNoError(t, err)
	scope := domain.ManualCompactionScope()
	matcher, err := domain.ParseCompactionHookScope("manual")
	requireNoError(t, err)
	declarations := make([]domain.HookDeclaration, 0, 2)
	for _, name := range []string{"first", "second"} {
		declaration, err := domain.NewPostCompactHookDeclaration(domain.HookSourceProject, domain.HookFeedbackNone, strings.Repeat("b", 64), name, matcher, "true", 5)
		requireNoError(t, err)
		declarations = append(declarations, declaration)
	}
	usage := testRuntimeUsage(t)
	before, _ := domain.NewEstimatedTokenEstimate(estimate.MethodByteHeuristic, 100)
	after, _ := domain.NewEstimatedTokenEstimate(estimate.MethodByteHeuristic, 10)
	const prefix = `{"opaque":"`
	const suffix = `"}`
	small, err := provider.NewCheckpointEnvelope(scope, id, source, []byte(prefix+suffix))
	requireNoError(t, err)
	smallPlan, err := session.NewPostCompactPlan(small, declarations)
	requireNoError(t, err)
	smallDraft, err := session.NewCompactionCompletedDraft(scope, small, usage, before, after, smallPlan)
	requireNoError(t, err)
	// 正文仍可编码，只有附上完整记录信封后才超限。两条声明提供可回收的计划空间。
	padding := session.MaxRecordBytes - len(smallDraft.PayloadBytes()) - 16
	native := []byte(prefix + strings.Repeat("x", padding) + suffix)
	checkpoint, err := provider.NewCheckpointEnvelope(scope, id, source, native)
	requireNoError(t, err)
	plan, err := session.NewPostCompactPlan(checkpoint, declarations)
	requireNoError(t, err)
	check := postCompactCheck{plan: plan, declarations: declarations}
	draft, err := check.completionDraft(checkpoint, usage, before, after)
	requireNoError(t, err)
	stored := draft.PayloadBytes()
	if check.plan.Kind() != session.PostCompactPlanSkipped || check.plan.Code() != domain.HookPlanLimit ||
		len(check.declarations) != 0 || !bytes.Contains(stored, []byte(`"post_compact":{"kind":"skipped","code":"hook_plan_limit"}`)) ||
		!bytes.Contains(stored, native) {
		t.Fatal("oversized plan did not preserve the original checkpoint in the skipped completion")
	}
}
