package context

import (
	stdcontext "context"
	"strings"
	"testing"

	"easycode/internal/context/estimate"
	"easycode/internal/domain"
	"easycode/internal/tool"
)

type planningReadExecutor struct{}

func (planningReadExecutor) Execute(stdcontext.Context, tool.ReadInvocation) tool.ReadExecution {
	return tool.ReadExecution{}
}

type planningGlobExecutor struct{}

func (planningGlobExecutor) Execute(stdcontext.Context, tool.GlobInvocation, tool.TextOutputCapture) tool.SearchExecution {
	return tool.SearchExecution{}
}

type planningGrepExecutor struct{}

func (planningGrepExecutor) Execute(stdcontext.Context, tool.GrepInvocation, tool.TextOutputCapture) tool.SearchExecution {
	return tool.SearchExecution{}
}

func TestPlannerBudgetBoundaryAndOverLimit(t *testing.T) {
	t.Parallel()
	base, err := NewPlanner().Plan(mustPlanningInput(t, emptyHistory(domain.ProviderOpenAI), estimatedFootprint(t, domain.ProviderOpenAI, 0, 0), "", DisabledBudget()))
	if err != nil {
		t.Fatal(err)
	}
	baseTokens, _ := base.TotalEstimate().Tokens()
	budget, err := NewBudget(baseTokens+10, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	within, err := NewPlanner().Plan(mustPlanningInput(t, emptyHistory(domain.ProviderOpenAI), estimatedFootprint(t, domain.ProviderOpenAI, 0, 0), strings.Repeat("x", 32), budget))
	if err != nil {
		t.Fatal(err)
	}
	if within.Decision().State() != BudgetWithinLimit {
		t.Fatalf("boundary decision = %q", within.Decision().State())
	}
	over, err := NewPlanner().Plan(mustPlanningInput(t, emptyHistory(domain.ProviderOpenAI), estimatedFootprint(t, domain.ProviderOpenAI, 0, 0), strings.Repeat("x", 33), budget))
	if err != nil {
		t.Fatal(err)
	}
	if over.Decision().State() != BudgetOverLimit {
		t.Fatalf("over decision = %q", over.Decision().State())
	}
}

func TestPlannerCacheFingerprintIgnoresVolatileInputAndSecrets(t *testing.T) {
	t.Parallel()
	profile, err := NewProviderProfile(domain.ProviderAnthropic, "claude-test")
	if err != nil {
		t.Fatal(err)
	}
	history := emptyHistory(domain.ProviderAnthropic)
	footprint := estimatedFootprint(t, domain.ProviderAnthropic, 0, 0)
	empty := emptyProjectInstructions(t)
	catalog := testToolCatalog(t)
	firstInput, _ := testPlanningInput(profile, catalog, empty, planningHistoryWithInput(history, "first secret-input"), footprint, NewSamplingInput(), DisabledBudget())
	secondInput, _ := testPlanningInput(profile, catalog, empty, planningHistoryWithInput(history, "second secret-input"), footprint, NewSamplingInput(), DisabledBudget())
	first, _ := NewPlanner().Plan(firstInput)
	second, _ := NewPlanner().Plan(secondInput)
	firstFingerprint, _ := first.CachePlan().StablePrefixFingerprint()
	secondFingerprint, _ := second.CachePlan().StablePrefixFingerprint()
	if firstFingerprint != secondFingerprint {
		t.Fatal("volatile input changed stable prefix")
	}
	stableJSON := string(first.CachePlan().Segments()[0].CanonicalJSON())
	for _, forbidden := range []string{"api-key", "base_url", "cwd", "session"} {
		if strings.Contains(stableJSON, forbidden) {
			t.Fatalf("stable profile contains %q: %s", forbidden, stableJSON)
		}
	}
}

func TestPlannerProjectInstructionFingerprintUsesContentAndRelativeSource(t *testing.T) {
	t.Parallel()
	first := planWithProjectInstructions(t, projectInstructions(t, "AGENTS.md", "same"), "input", DisabledBudget())
	changedContent := planWithProjectInstructions(t, projectInstructions(t, "AGENTS.md", "changed"), "input", DisabledBudget())
	changedSource := planWithProjectInstructions(t, projectInstructions(t, "nested/AGENTS.md", "same"), "input", DisabledBudget())
	changedInput := planWithProjectInstructions(t, projectInstructions(t, "AGENTS.md", "same"), "different input", DisabledBudget())

	firstFingerprint, _ := first.CachePlan().StablePrefixFingerprint()
	contentFingerprint, _ := changedContent.CachePlan().StablePrefixFingerprint()
	sourceFingerprint, _ := changedSource.CachePlan().StablePrefixFingerprint()
	inputFingerprint, _ := changedInput.CachePlan().StablePrefixFingerprint()
	if firstFingerprint == contentFingerprint || firstFingerprint == sourceFingerprint {
		t.Fatal("project content or relative source did not invalidate the stable prefix")
	}
	if firstFingerprint != inputFingerprint {
		t.Fatal("volatile current input invalidated the project-stable prefix")
	}
	for _, segment := range first.CachePlan().Segments() {
		if strings.Contains(string(segment.CanonicalJSON()), "/absolute/") || strings.Contains(string(segment.CanonicalJSON()), "mtime") {
			t.Fatalf("ambient path metadata entered cache segment: %s", segment.CanonicalJSON())
		}
	}
}

func mustPlanningInput(t *testing.T, history domain.SemanticHistoryView, footprint domain.NativeHistoryFootprint, current string, budget Budget) PlanningInput {
	t.Helper()
	profile, err := NewProviderProfile(history.Provider, "fixture-model")
	if err != nil {
		t.Fatal(err)
	}
	input, err := testPlanningInput(profile, testToolCatalog(t), emptyProjectInstructions(t), planningHistoryWithInput(history, current), footprint, NewSamplingInput(), budget)
	if err != nil {
		t.Fatal(err)
	}
	return input
}

func emptyProjectInstructions(t *testing.T) domain.ProjectInstructionsSnapshot {
	t.Helper()
	snapshot, err := domain.NewEmptyProjectInstructionsSnapshot(32 << 10)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func projectInstructions(t *testing.T, source string, content string) domain.ProjectInstructionsSnapshot {
	t.Helper()
	document, err := domain.NewProjectInstructionDocument(source, content)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := domain.NewProjectInstructionsSnapshot([]domain.ProjectInstructionDocument{document}, 32<<10)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func planWithProjectInstructions(
	t *testing.T,
	snapshot domain.ProjectInstructionsSnapshot,
	current string,
	budget Budget,
) ContextPlan {
	t.Helper()
	profile, _ := NewProviderProfile(domain.ProviderOpenAI, "gpt-test")
	input, err := testPlanningInput(
		profile, testToolCatalog(t), snapshot, planningHistoryWithInput(emptyHistory(domain.ProviderOpenAI), current),
		estimatedFootprint(t, domain.ProviderOpenAI, 0, 0), NewSamplingInput(), budget,
	)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := NewPlanner().Plan(input)
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func estimatedFootprint(t *testing.T, family domain.ProviderFamily, revision, tokens uint64) domain.NativeHistoryFootprint {
	t.Helper()
	value, err := domain.NewEstimatedTokenEstimate(estimate.MethodByteHeuristic, tokens)
	if err != nil {
		t.Fatal(err)
	}
	footprint, err := domain.NewNativeHistoryFootprint(family, revision, value)
	if err != nil {
		t.Fatal(err)
	}
	return footprint
}

func emptyHistory(family domain.ProviderFamily) domain.SemanticHistoryView {
	return domain.SemanticHistoryView{Provider: family, Turns: make([]domain.SemanticTurn, 0)}
}

func testToolCatalog(t *testing.T) tool.CatalogSnapshot {
	t.Helper()
	catalog, err := tool.NewCatalogSnapshot(planningReadExecutor{}, planningGlobExecutor{}, planningGrepExecutor{}, catalogPatchPreparer{}, catalogPatchExecutor{}, catalogWritePreparer{}, catalogWriteExecutor{}, catalogExecFixture{}, catalogExecFixture{}, tool.RenderExecOutput, tool.NewApprovalPolicy(), catalogArtifactExecutor{}, catalogSkillExecutor{}, catalogQuestionFixture{})
	if err != nil {
		t.Fatal(err)
	}
	return catalog
}

// catalogPatchPreparer 只用于验证目录装配，测试不会通过它访问文件系统。
type catalogPatchPreparer struct{}

func (catalogPatchPreparer) Prepare(stdcontext.Context, tool.PatchInvocation) (tool.PatchCandidate, error) {
	return tool.PatchCandidate{}, nil
}

type catalogPatchExecutor struct{}

func (catalogPatchExecutor) Execute(stdcontext.Context, tool.PatchCandidate) tool.InvocationResult {
	return tool.InvocationResult{}
}

// catalogWritePreparer 只用于验证目录装配，测试不会通过它访问文件系统。
type catalogWritePreparer struct{}

func (catalogWritePreparer) Prepare(stdcontext.Context, tool.WriteInvocation, tool.ReadObservation) (tool.WriteCandidate, error) {
	return tool.WriteCandidate{}, nil
}

type catalogWriteExecutor struct{}

func (catalogWriteExecutor) Execute(stdcontext.Context, tool.WriteCandidate) tool.InvocationResult {
	return tool.InvocationResult{}
}

func planningTurn(user, assistant string) domain.SemanticTurn {
	return domain.SemanticTurn{TurnID: "01900000-0000-7000-8000-000000000001", Messages: []domain.SemanticMessage{
		{Role: domain.SemanticUser, InputID: "01900000-0000-7000-8000-000000000002", Text: user},
		{Role: domain.SemanticAssistant, Text: assistant},
	}}
}

func planningHistoryWithInput(history domain.SemanticHistoryView, text string) domain.SemanticHistoryView {
	copy := history.Clone()
	if text != "" {
		copy.Turns = append(copy.Turns, planningTurn(text, ""))
	}
	return copy
}

// testPlanningInput 将无 checkpoint 的历史 fixture 转成真实有效窗口。
func testPlanningInput(profile ProviderProfile, catalog tool.CatalogSnapshot, project domain.ProjectInstructionsSnapshot, history domain.SemanticHistoryView, footprint domain.NativeHistoryFootprint, current CurrentInput, budget Budget) (PlanningInput, error) {
	view, err := domain.NewActiveContextView(history, domain.ContextSummary{}, footprint.Revision())
	if err != nil {
		return PlanningInput{}, err
	}
	return NewPlanningInput(profile, catalog, project, view, footprint, current, budget)
}
