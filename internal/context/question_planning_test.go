package context

import (
	"bytes"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/tool"
)

func TestQuestionCatalogAndAnswersHaveSeparateCacheSources(t *testing.T) {
	for _, family := range []domain.ProviderFamily{domain.ProviderOpenAI, domain.ProviderAnthropic} {
		t.Run(string(family), func(t *testing.T) {
			catalog := testToolCatalog(t)
			view, err := catalog.View(family)
			if err != nil {
				t.Fatal(err)
			}
			if len(view.Facades()) != 9 {
				t.Fatal("missing ninth capability")
			}
			questions := 0
			for _, facade := range view.Facades() {
				if facade.Capability() == tool.CapabilityQuestion {
					questions++
				}
			}
			if questions != 1 {
				t.Fatal("question facade count")
			}
			profile, _ := NewProviderProfile(family, "fixture-model")
			var prefix string
			var stable []byte
			for i, text := range []string{"question storage=SQLite request=one width=80 allowance=1000", "question storage=Other 🧑‍💻 request=two width=12 allowance=1024", "question storage=Skipped request=three"} {
				history := domain.SemanticHistoryView{Provider: family, Turns: []domain.SemanticTurn{planningTurn(text, "answer accepted")}}
				input, err := testPlanningInput(profile, catalog, emptyProjectInstructions(t), history, estimatedFootprint(t, family, 1, uint64(len(text))), NewSamplingInput(), DisabledBudget())
				if err != nil {
					t.Fatal(err)
				}
				plan, err := NewPlanner().Plan(input)
				if err != nil {
					t.Fatal(err)
				}
				got, _ := plan.CachePlan().StablePrefixFingerprint()
				segment := plan.CachePlan().Segments()[1]
				if i == 0 {
					prefix = got
					stable = segment.CanonicalJSON()
				} else if got != prefix || !bytes.Equal(stable, segment.CanonicalJSON()) {
					t.Fatal("answer state polluted stable catalog")
				}
				if plan.Sources()[1].Revision() != view.Fingerprint() {
					t.Fatal("unexplained tool catalog source revision")
				}
			}
		})
	}
}
