package context

import (
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"

	"easycode/internal/context/estimate"
	"easycode/internal/domain"
)

func TestCompactionRequestBudget(t *testing.T) {
	budget, _ := NewBudget(100, 10, 5)
	unknown, _ := domain.NewUnknownTokenEstimate(estimate.MethodByteHeuristic)
	for _, test := range []struct {
		name   string
		tokens domain.TokenEstimate
		output uint64
		budget Budget
		want   CompactionRejection
	}{
		{"exact", mustEstimated(75), 20, budget, ""},
		{"above", mustEstimated(76), 20, budget, CompactionBudgetExceeded},
		{"unknown", unknown, 20, budget, CompactionBudgetUnknown},
		{"unknown unconfigured", unknown, 20, DisabledBudget(), ""},
		{"default too large", mustEstimated(1), 4096, budget, CompactionBudgetExceeded},
		{"overflow", mustEstimated(math.MaxUint64), 20, budget, CompactionBudgetExceeded},
		{"unconfigured", mustEstimated(math.MaxUint64), 20, DisabledBudget(), ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			assertCompactionReason(t, CheckCompactionRequest(test.tokens, test.output, test.budget), test.want)
		})
	}
	if CheckCompactionRequest(mustEstimated(1), 0, budget) == nil {
		t.Fatal("zero output reserve accepted")
	}
	if CheckCompactionRequest(domain.TokenEstimate{}, 1, budget) == nil {
		t.Fatal("zero estimate accepted")
	}
}

func TestCompactionCandidateAndStableCache(t *testing.T) {
	profile, _ := NewProviderProfile(domain.ProviderOpenAI, "model")
	catalog := testToolCatalog(t)
	project := projectInstructions(t, "AGENTS.md", "Keep project constraints stable")
	history := domain.SemanticHistoryView{Provider: domain.ProviderOpenAI, Turns: []domain.SemanticTurn{planningTurn(strings.Repeat("old archived text ", 1000), "answer")}}
	original, err := domain.NewActiveContextView(history, domain.ContextSummary{}, 2)
	if err != nil {
		t.Fatal(err)
	}
	summary, _ := domain.NewContextSummary("keep the goal and next task")
	compacted, err := domain.NewActiveContextView(emptyHistory(domain.ProviderOpenAI), summary, 3)
	if err != nil {
		t.Fatal(err)
	}
	makePlan := func(view domain.ActiveContextView, nativeTokens uint64, budget Budget) ContextPlan {
		t.Helper()
		input, err := NewPlanningInput(profile, catalog, project, view, estimatedFootprint(t, domain.ProviderOpenAI, view.Revision(), nativeTokens), NewSamplingInput(), budget)
		if err != nil {
			t.Fatal(err)
		}
		plan, err := NewPlanner().Plan(input)
		if err != nil {
			t.Fatal(err)
		}
		return plan
	}
	before := makePlan(original, 5000, DisabledBudget())
	after := makePlan(compacted, 100, DisabledBudget())
	assertCompactionReason(t, CheckCompactionCandidate(before, after), "")
	assertCompactionReason(t, CheckCompactionCandidate(after, after), CompactionNoReduction)
	assertCompactionReason(t, CheckCompactionCandidate(after, before), CompactionNoReduction)
	if !reflect.DeepEqual(before.CachePlan().Segments()[:3], after.CachePlan().Segments()[:3]) {
		t.Fatal("stable prefix changed during compaction")
	}
	if reflect.DeepEqual(before.CachePlan().Segments()[3], after.CachePlan().Segments()[3]) {
		t.Fatal("history fingerprint did not change")
	}
	if !reflect.DeepEqual(after, makePlan(compacted, 100, DisabledBudget())) {
		t.Fatal("query changed plan")
	}
	oldTotal, _ := before.TotalEstimate().Tokens()
	newTotal, _ := after.TotalEstimate().Tokens()
	if newTotal >= oldTotal {
		t.Fatal("archived text still counted")
	}
	budget, _ := NewBudget(newTotal, 1, 0)
	over := makePlan(compacted, 100, budget)
	assertCompactionReason(t, CheckCompactionCandidate(before, over), CompactionBudgetExceeded)
	unknown, _ := domain.NewUnknownTokenEstimate(estimate.MethodByteHeuristic)
	footprint, _ := domain.NewNativeHistoryFootprint(domain.ProviderOpenAI, compacted.Revision(), unknown)
	input, err := NewPlanningInput(profile, catalog, project, compacted, footprint, NewSamplingInput(), DisabledBudget())
	if err != nil {
		t.Fatal(err)
	}
	unknownPlan, err := NewPlanner().Plan(input)
	if err != nil {
		t.Fatal(err)
	}
	assertCompactionReason(t, CheckCompactionCandidate(before, unknownPlan), CompactionBudgetUnknown)
}

func assertCompactionReason(t *testing.T, err error, want CompactionRejection) {
	t.Helper()
	if want == "" {
		if err != nil {
			t.Fatal(err)
		}
		return
	}
	var rejection *CompactionBudgetError
	if !errors.As(err, &rejection) || rejection.Reason() != want {
		t.Fatalf("got %v, want %s", err, want)
	}
}
