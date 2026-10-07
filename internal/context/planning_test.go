package context

import (
	stdcontext "context"
	"math"
	"strings"
	"testing"

	"easycode/internal/context/estimate"
	"easycode/internal/domain"
	"easycode/internal/tool"
)

type planningReadExecutor struct{}

func (planningReadExecutor) Execute(stdcontext.Context, tool.ReadInvocation) tool.InvocationResult {
	return tool.InvocationResult{}
}

func TestPlannerProducesDeterministicOrderedImmutablePlan(t *testing.T) {
	t.Parallel()
	history := domain.SemanticHistoryView{Provider: domain.ProviderOpenAI, Turns: []domain.SemanticTurn{{UserText: "hello", AssistantText: "world"}}}
	input := mustPlanningInput(t, history, estimatedFootprint(t, domain.ProviderOpenAI, 3, 10), "next", DisabledBudget())
	planner := NewPlanner()
	first, err := planner.Plan(input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := planner.Plan(input)
	if err != nil {
		t.Fatal(err)
	}
	wantKinds := []SourceKind{SourceProviderProfile, SourceToolCatalog, SourceCommittedHistory, SourceCurrentInput}
	for index, source := range first.Sources() {
		if source.Kind() != wantKinds[index] {
			t.Fatalf("source %d = %q", index, source.Kind())
		}
	}
	firstFingerprint, _ := first.CachePlan().StablePrefixFingerprint()
	secondFingerprint, _ := second.CachePlan().StablePrefixFingerprint()
	if firstFingerprint != secondFingerprint {
		t.Fatalf("fingerprints differ: %s != %s", firstFingerprint, secondFingerprint)
	}
	history.Turns[0].UserText = "mutated"
	sources := first.Sources()
	sources[0] = PlannedSource{}
	if first.Sources()[0].Kind() != SourceProviderProfile {
		t.Fatal("plan source slice was mutated")
	}
	if err := first.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestPlannerUsesMaxForVisibleAndNativeHistory(t *testing.T) {
	t.Parallel()
	history := domain.SemanticHistoryView{Provider: domain.ProviderOpenAI, Turns: []domain.SemanticTurn{{UserText: strings.Repeat("a", 400)}}}
	plan, err := NewPlanner().Plan(mustPlanningInput(
		t, history, estimatedFootprint(t, domain.ProviderOpenAI, 1, 140), "", DisabledBudget(),
	))
	if err != nil {
		t.Fatal(err)
	}
	historyTokens, ok := plan.Sources()[2].Estimate().Tokens()
	if !ok || historyTokens != 140 {
		t.Fatalf("history estimate = %d ok %t", historyTokens, ok)
	}
}

func TestPlannerPropagatesUnknownAndBudgetStates(t *testing.T) {
	t.Parallel()
	budget, err := NewBudget(10, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	unknown, _ := domain.NewUnknownTokenEstimate(estimate.MethodByteHeuristic)
	footprint, _ := domain.NewNativeHistoryFootprint(domain.ProviderOpenAI, 0, unknown)
	unknownPlan, err := NewPlanner().Plan(mustPlanningInput(t, emptyHistory(domain.ProviderOpenAI), footprint, "large enough", budget))
	if err != nil {
		t.Fatal(err)
	}
	if unknownPlan.Decision().State() != BudgetIndeterminate {
		t.Fatalf("unknown decision = %q", unknownPlan.Decision().State())
	}

	unenforced, err := NewPlanner().Plan(mustPlanningInput(t, emptyHistory(domain.ProviderOpenAI), estimatedFootprint(t, domain.ProviderOpenAI, 0, 0), strings.Repeat("x", 100), DisabledBudget()))
	if err != nil {
		t.Fatal(err)
	}
	if unenforced.Decision().State() != BudgetNotEnforced {
		t.Fatalf("unenforced decision = %q", unenforced.Decision().State())
	}
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
	firstInput, _ := NewPlanningInput(profile, catalog, empty, history, footprint, NewUserTextInput("first secret-input"), DisabledBudget())
	secondInput, _ := NewPlanningInput(profile, catalog, empty, history, footprint, NewUserTextInput("second secret-input"), DisabledBudget())
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

func TestPlannerToolContinuationHasZeroCurrentInputEstimate(t *testing.T) {
	t.Parallel()
	profile, _ := NewProviderProfile(domain.ProviderOpenAI, "gpt-test")
	input, err := NewPlanningInput(
		profile, testToolCatalog(t), emptyProjectInstructions(t), emptyHistory(domain.ProviderOpenAI),
		estimatedFootprint(t, domain.ProviderOpenAI, 1, 12), NewToolContinuationInput(), DisabledBudget(),
	)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := NewPlanner().Plan(input)
	if err != nil {
		t.Fatal(err)
	}
	sources := plan.Sources()
	current := sources[len(sources)-1]
	if current.Kind() != SourceCurrentInput {
		t.Fatalf("末尾来源 = %q", current.Kind())
	}
	if tokens, known := current.Estimate().Tokens(); !known || tokens != 0 {
		t.Fatalf("continuation current input estimate = %d known=%t", tokens, known)
	}
	invalid := CurrentInput{kind: CurrentInputToolContinuation, text: "forged"}
	if _, err := NewPlanningInput(
		profile, testToolCatalog(t), emptyProjectInstructions(t), emptyHistory(domain.ProviderOpenAI),
		estimatedFootprint(t, domain.ProviderOpenAI, 1, 12), invalid, DisabledBudget(),
	); err == nil {
		t.Fatal("带用户文本的 tool continuation 被接受")
	}
}

func TestPlannerToolCatalogSegmentIsStableAndPathSafe(t *testing.T) {
	t.Parallel()
	first := mustPlanningInput(t, emptyHistory(domain.ProviderOpenAI), estimatedFootprint(t, domain.ProviderOpenAI, 0, 0), "first", DisabledBudget())
	second := mustPlanningInput(t, emptyHistory(domain.ProviderOpenAI), estimatedFootprint(t, domain.ProviderOpenAI, 0, 0), "second", DisabledBudget())
	firstPlan, _ := NewPlanner().Plan(first)
	secondPlan, _ := NewPlanner().Plan(second)
	firstSegment := firstPlan.CachePlan().Segments()[1]
	secondSegment := secondPlan.CachePlan().Segments()[1]
	if firstSegment.ID() != string(SourceToolCatalog) || firstSegment.Stability() != StabilityStable ||
		firstSegment.Fingerprint() != secondSegment.Fingerprint() {
		t.Fatal("tool catalog 未映射为稳定 cache segment")
	}
	encoded := string(firstSegment.CanonicalJSON())
	for _, forbidden := range []string{"api-key", "Authorization", "/absolute/workspace", "invocation", "session"} {
		if strings.Contains(encoded, forbidden) {
			t.Fatalf("tool catalog segment 泄漏动态或敏感值 %q", forbidden)
		}
	}
}

func TestPlanningInputRejectsProviderAndMethodMismatch(t *testing.T) {
	t.Parallel()
	profile, _ := NewProviderProfile(domain.ProviderOpenAI, "gpt-test")
	catalog := testToolCatalog(t)
	wrongFamily := estimatedFootprint(t, domain.ProviderAnthropic, 0, 0)
	empty := emptyProjectInstructions(t)
	if _, err := NewPlanningInput(profile, catalog, empty, emptyHistory(domain.ProviderOpenAI), wrongFamily, NewUserTextInput("x"), DisabledBudget()); err == nil {
		t.Fatal("family mismatch unexpectedly accepted")
	}
	other, _ := domain.NewEstimatedTokenEstimate("future", 1)
	footprint, _ := domain.NewNativeHistoryFootprint(domain.ProviderOpenAI, 0, other)
	if _, err := NewPlanningInput(profile, catalog, empty, emptyHistory(domain.ProviderOpenAI), footprint, NewUserTextInput("x"), DisabledBudget()); err == nil {
		t.Fatal("method mismatch unexpectedly accepted")
	}
}

func TestContextPlanRejectsMalformedSourceSequence(t *testing.T) {
	t.Parallel()
	valid, err := NewPlanner().Plan(mustPlanningInput(
		t, emptyHistory(domain.ProviderOpenAI), estimatedFootprint(t, domain.ProviderOpenAI, 0, 0), "x", DisabledBudget(),
	))
	if err != nil {
		t.Fatal(err)
	}
	malformed := valid
	malformed.sources = append([]PlannedSource(nil), valid.sources...)
	malformed.sources[0], malformed.sources[1] = malformed.sources[1], malformed.sources[0]
	if err := malformed.Validate(); err == nil {
		t.Fatal("malformed source order unexpectedly accepted")
	}
	duplicate := valid
	duplicate.sources = append([]PlannedSource(nil), valid.sources...)
	duplicate.sources[1] = duplicate.sources[0]
	if err := duplicate.Validate(); err == nil {
		t.Fatal("duplicate source unexpectedly accepted")
	}
}

func TestPlannerIncludesProjectInstructionsAsProjectStableSource(t *testing.T) {
	t.Parallel()
	snapshot := projectInstructions(t, "nested/AGENTS.md", "use make verify")
	profile, _ := NewProviderProfile(domain.ProviderOpenAI, "gpt-test")
	input, err := NewPlanningInput(
		profile, testToolCatalog(t), snapshot, emptyHistory(domain.ProviderOpenAI),
		estimatedFootprint(t, domain.ProviderOpenAI, 0, 0), NewUserTextInput("next"), DisabledBudget(),
	)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := NewPlanner().Plan(input)
	if err != nil {
		t.Fatal(err)
	}
	wantKinds := []SourceKind{
		SourceProviderProfile, SourceToolCatalog, SourceProjectInstructions, SourceCommittedHistory, SourceCurrentInput,
	}
	for index, source := range plan.Sources() {
		if source.Kind() != wantKinds[index] {
			t.Fatalf("source %d = %q, want %q", index, source.Kind(), wantKinds[index])
		}
	}
	projectSource := plan.Sources()[2]
	if projectSource.Lifecycle() != SourceLifecycleReplace || projectSource.Revision() != snapshot.Revision() {
		t.Fatalf("project source = %#v", projectSource)
	}
	wantTokens, _ := estimate.String(snapshot.RenderedText()).Tokens()
	gotTokens, known := projectSource.Estimate().Tokens()
	if !known || gotTokens != wantTokens {
		t.Fatalf("project tokens = %d known=%t, want %d", gotTokens, known, wantTokens)
	}
	segments := plan.CachePlan().Segments()
	if len(segments) != 5 || segments[2].ID() != string(SourceProjectInstructions) ||
		segments[2].Stability() != StabilityProjectStable || segments[2].Revision() != snapshot.Revision() {
		t.Fatalf("project cache segment = %#v", segments)
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

func TestPlannerProjectInstructionsParticipateInBudgetDecisions(t *testing.T) {
	t.Parallel()
	snapshot := projectInstructions(t, "AGENTS.md", strings.Repeat("rule ", 20))
	baseline := planWithProjectInstructions(t, snapshot, "", DisabledBudget())
	totalTokens, _ := baseline.TotalEstimate().Tokens()
	if totalTokens < 2 {
		t.Fatalf("planning token setup is too small: %d", totalTokens)
	}
	exactBudget, err := NewBudget(totalTokens+2, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	exact := planWithProjectInstructions(t, snapshot, "", exactBudget)
	if exact.Decision().State() != BudgetWithinLimit {
		t.Fatalf("exact budget decision = %q", exact.Decision().State())
	}
	overBudget, err := NewBudget(totalTokens+1, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	over := planWithProjectInstructions(t, snapshot, "", overBudget)
	if over.Decision().State() != BudgetOverLimit {
		t.Fatalf("over budget decision = %q", over.Decision().State())
	}

	unknown, _ := domain.NewUnknownTokenEstimate(estimate.MethodByteHeuristic)
	footprint, _ := domain.NewNativeHistoryFootprint(domain.ProviderOpenAI, 0, unknown)
	profile, _ := NewProviderProfile(domain.ProviderOpenAI, "gpt-test")
	input, err := NewPlanningInput(profile, testToolCatalog(t), snapshot, emptyHistory(domain.ProviderOpenAI), footprint, NewUserTextInput(""), exactBudget)
	if err != nil {
		t.Fatal(err)
	}
	indeterminate, err := NewPlanner().Plan(input)
	if err != nil {
		t.Fatal(err)
	}
	if indeterminate.Decision().State() != BudgetIndeterminate {
		t.Fatalf("unknown budget decision = %q", indeterminate.Decision().State())
	}
}

func TestContextPlanRejectsMisplacedProjectInstructions(t *testing.T) {
	t.Parallel()
	valid := planWithProjectInstructions(t, projectInstructions(t, "AGENTS.md", "rule"), "input", DisabledBudget())
	malformed := valid
	malformed.sources = append([]PlannedSource(nil), valid.sources...)
	malformed.sources[1], malformed.sources[2] = malformed.sources[2], malformed.sources[1]
	if err := malformed.Validate(); err == nil {
		t.Fatal("misplaced project instructions unexpectedly accepted")
	}
}

func TestBudgetRejectsInvalidAndOverflowingValues(t *testing.T) {
	t.Parallel()
	for _, values := range [][3]uint64{{0, 0, 0}, {10, 9, 1}, {10, math.MaxUint64, 1}} {
		if _, err := NewBudget(values[0], values[1], values[2]); err == nil {
			t.Fatalf("budget %#v unexpectedly accepted", values)
		}
	}
}

func mustPlanningInput(t *testing.T, history domain.SemanticHistoryView, footprint domain.NativeHistoryFootprint, current string, budget Budget) PlanningInput {
	t.Helper()
	profile, err := NewProviderProfile(history.Provider, "fixture-model")
	if err != nil {
		t.Fatal(err)
	}
	input, err := NewPlanningInput(profile, testToolCatalog(t), emptyProjectInstructions(t), history, footprint, NewUserTextInput(current), budget)
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
	input, err := NewPlanningInput(
		profile, testToolCatalog(t), snapshot, emptyHistory(domain.ProviderOpenAI),
		estimatedFootprint(t, domain.ProviderOpenAI, 0, 0), NewUserTextInput(current), budget,
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
	catalog, err := tool.NewReadCatalogSnapshot(planningReadExecutor{})
	if err != nil {
		t.Fatal(err)
	}
	return catalog
}
