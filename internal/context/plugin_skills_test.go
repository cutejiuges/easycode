package context

import (
	"easycode/internal/domain"
	"reflect"
	"testing"
)

func TestPluginPrivateRevisionBodyAndCatalogCacheBoundaries(t *testing.T) {
	source, _ := domain.NewSkillSource("plugin", "qa", "codex")
	m, _ := domain.NewSkillMetadata(source, "qa:review", "Review changes", "", true, false)
	catalog, _ := domain.NewSkillCatalog([]domain.SkillMetadata{m})
	attachment, _ := domain.NewSkillAttachment(source, m.Name, domain.SkillDigest("binding"), "", "Frozen instructions")
	history := domain.SemanticHistoryView{Provider: domain.ProviderOpenAI, Turns: []domain.SemanticTurn{planningTurn("hello", "world")}}
	history.Turns[0].Messages[0].HasSkill = true
	history.Turns[0].Messages[0].Skill = attachment
	plan := func(view domain.SemanticHistoryView, catalog domain.SkillCatalog) CacheSources {
		input := mustPlanningInput(t, view, estimatedFootprint(t, domain.ProviderOpenAI, 3, 10), "next", DisabledBudget())
		input, err := input.WithSkillCatalog(catalog)
		if err != nil {
			t.Fatal(err)
		}
		planned, err := NewPlanner().Plan(input)
		if err != nil {
			t.Fatal(err)
		}
		sources, err := NewCacheSources(planned)
		if err != nil {
			t.Fatal(err)
		}
		return sources
	}
	before := plan(history, catalog)
	history.Turns[0].Messages[0].Skill.Revision = domain.SkillDigest("other private binding")
	if after := plan(history, catalog); !reflect.DeepEqual(before, after) {
		t.Fatal("private revision changed content fingerprints")
	}
	history.Turns[0].Messages[0].Skill.Content = "Changed instructions"
	after := plan(history, catalog)
	if before.StableFingerprint != after.StableFingerprint || before.Sources[4].Fingerprint == after.Sources[4].Fingerprint {
		t.Fatal("body invalidation crossed stable prefix")
	}
	history.Turns[0].Messages[0].Skill.Content = attachment.Content
	m.Description = "Changed description"
	changed, _ := domain.NewSkillCatalog([]domain.SkillMetadata{m})
	after = plan(history, changed)
	for i, source := range before.Sources {
		if source.Kind == SourceSkillCatalog {
			if source.Fingerprint == after.Sources[i].Fingerprint {
				t.Fatal("metadata not invalidated")
			}
		} else if source.Fingerprint != after.Sources[i].Fingerprint {
			t.Fatal("metadata changed another source", source.Kind)
		}
	}
}
