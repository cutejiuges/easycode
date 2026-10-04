package context

import (
	"math"
	"strings"
	"testing"

	"easycode/internal/context/estimate"
	"easycode/internal/domain"
)

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
	wantKinds := []SourceKind{SourceProviderProfile, SourceCommittedHistory, SourceCurrentInput}
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
	historyTokens, ok := plan.Sources()[1].Estimate().Tokens()
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
	unknown, _ := domain.NewUnknownTokenEstimate(estimate.MethodByteHeuristicV1)
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
	budget, err := NewBudget(10, 1, 1)
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
	firstInput, _ := NewPlanningInput(profile, history, footprint, "first secret-input", DisabledBudget())
	secondInput, _ := NewPlanningInput(profile, history, footprint, "second secret-input", DisabledBudget())
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

func TestPlanningInputRejectsProviderAndMethodMismatch(t *testing.T) {
	t.Parallel()
	profile, _ := NewProviderProfile(domain.ProviderOpenAI, "gpt-test")
	wrongFamily := estimatedFootprint(t, domain.ProviderAnthropic, 0, 0)
	if _, err := NewPlanningInput(profile, emptyHistory(domain.ProviderOpenAI), wrongFamily, "x", DisabledBudget()); err == nil {
		t.Fatal("family mismatch unexpectedly accepted")
	}
	other, _ := domain.NewEstimatedTokenEstimate("future", 1)
	footprint, _ := domain.NewNativeHistoryFootprint(domain.ProviderOpenAI, 0, other)
	if _, err := NewPlanningInput(profile, emptyHistory(domain.ProviderOpenAI), footprint, "x", DisabledBudget()); err == nil {
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
	input, err := NewPlanningInput(profile, history, footprint, current, budget)
	if err != nil {
		t.Fatal(err)
	}
	return input
}

func estimatedFootprint(t *testing.T, family domain.ProviderFamily, revision, tokens uint64) domain.NativeHistoryFootprint {
	t.Helper()
	value, err := domain.NewEstimatedTokenEstimate(estimate.MethodByteHeuristicV1, tokens)
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
