package context

import (
	"easycode/internal/domain"
	"testing"
)

func TestSkillCatalogPlanAndCacheIsolation(t *testing.T) {
	history := domain.SemanticHistoryView{Provider: domain.ProviderOpenAI, Turns: []domain.SemanticTurn{planningTurn("hello", "world")}}
	input := mustPlanningInput(t, history, estimatedFootprint(t, domain.ProviderOpenAI, 3, 10), "next", DisabledBudget())
	source, _ := domain.NewSkillSource("project", ".", "easycode")
	m, _ := domain.NewSkillMetadata(source, "demo", "Review changes", "", true, false)
	catalog, _ := domain.NewSkillCatalog([]domain.SkillMetadata{m})
	input, err := input.WithSkillCatalog(catalog)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := NewPlanner().Plan(input)
	if err != nil {
		t.Fatal(err)
	}
	sources, err := NewCacheSources(plan)
	if err != nil {
		t.Fatal(err)
	}
	if !sources.Sources[3].Present || sources.Sources[3].Kind != SourceSkillCatalog {
		t.Fatal("missing skill source")
	}
	m.Description = "Changed metadata"
	changed, _ := domain.NewSkillCatalog([]domain.SkillMetadata{m})
	input, _ = input.WithSkillCatalog(changed)
	next, err := NewPlanner().Plan(input)
	if err != nil {
		t.Fatal(err)
	}
	after, _ := NewCacheSources(next)
	if sources.StableFingerprint == after.StableFingerprint {
		t.Fatal("catalog change did not invalidate stable prefix")
	}
}
