package context

import (
	"bytes"
	"testing"

	"easycode/internal/context/estimate"
	"easycode/internal/domain"
	"easycode/internal/tool"
)

func TestPlanModeStableSourcesAndCatalog(t *testing.T) {
	full := testToolCatalog(t)
	plan, err := full.ForPlanMode()
	if err != nil {
		t.Fatal(err)
	}
	// 目录定义和顺序由两家 Provider 的请求 golden 验证。
	profile, err := NewProviderProfile(domain.ProviderOpenAI, "fixture-model")
	if err != nil {
		t.Fatal(err)
	}
	selected, err := profile.WithWorkMode(domain.WorkModePlan)
	if err != nil {
		t.Fatal(err)
	}
	planView, err := plan.View(profile.Family())
	if err != nil {
		t.Fatal(err)
	}
	makePlan := func(profile ProviderProfile, catalog tool.CatalogSnapshot) (ContextPlan, CacheSources) {
		family := profile.Family()
		input, err := testPlanningInput(profile, catalog, emptyProjectInstructions(t), emptyHistory(family), estimatedFootprint(t, family, 0, 0), NewSamplingInput(), DisabledBudget())
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
		return planned, sources
	}
	_, baseSources := makePlan(profile, full)
	filtered, planSources := makePlan(selected, plan)
	reverted, err := selected.WithWorkMode(domain.WorkModeDefault)
	if err != nil {
		t.Fatal(err)
	}
	_, revertedSources := makePlan(reverted, full)
	if baseSources != revertedSources {
		t.Fatal("returning to default changed cache sources")
	}
	change := CompareCacheSources(baseSources, planSources, "", domain.RequestReceipt{}, false)
	if change.Source != SourceProviderProfile || planSources.StableFingerprint == baseSources.StableFingerprint {
		t.Fatalf("unexpected planning cache attribution: %+v", change)
	}
	for i, source := range planSources.Sources {
		if source.Kind != SourceProviderProfile && source.Kind != SourceToolCatalog && source != baseSources.Sources[i] {
			t.Fatalf("unrelated source changed on Plan mode: %+v", source)
		}
	}
	instructions := domain.WorkModePlan.Instructions()
	sources := filtered.Sources()
	if sources[0].Estimate() != estimate.String(instructions) || sources[1].Estimate() != estimate.Bytes(planView.CanonicalJSON()) || sources[1].Revision() != planView.Fingerprint() {
		t.Fatal("planning estimate differs from actual instructions or advertised catalog")
	}
	profileIdentity := filtered.CachePlan().Segments()[0].CanonicalJSON()
	if !bytes.Contains(profileIdentity, []byte(domain.ContentFingerprint([]byte(instructions)))) || bytes.Contains(profileIdentity, []byte(instructions)) {
		t.Fatal("profile evidence differs from the actual planning instruction digest")
	}
	if planSources.Sources[1].Fingerprint == baseSources.Sources[1].Fingerprint {
		t.Fatal("advertised catalog change was omitted from the stable evidence")
	}
}
