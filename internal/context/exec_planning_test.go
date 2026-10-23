package context

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"easycode/internal/codec"
	"easycode/internal/context/estimate"
	"easycode/internal/domain"
	"easycode/internal/tool"
)

func TestExecCatalogEstimateAndStablePrefixRegression(t *testing.T) {
	for _, family := range []domain.ProviderFamily{domain.ProviderAnthropic, domain.ProviderOpenAI} {
		t.Run(string(family), func(t *testing.T) {
			catalog := testToolCatalog(t)
			view, err := catalog.View(family)
			if err != nil {
				t.Fatal(err)
			}
			profile, _ := NewProviderProfile(family, "fixture-model")
			input, err := NewPlanningInput(profile, catalog, emptyProjectInstructions(t), emptyHistory(family), estimatedFootprint(t, family, 0, 0), NewUserTextInput("next"), DisabledBudget())
			if err != nil {
				t.Fatal(err)
			}
			plan, err := NewPlanner().Plan(input)
			if err != nil {
				t.Fatal(err)
			}
			prefix, _ := plan.CachePlan().StablePrefixFingerprint()
			catalogTokens, known := plan.Sources()[1].Estimate().Tokens()
			wantTokens, _ := estimate.Bytes(view.CanonicalJSON()).Tokens()
			total, _ := plan.TotalEstimate().Tokens()
			if !known || catalogTokens != wantTokens || total != catalogTokens+1 || plan.Sources()[1].Revision() != view.Fingerprint() {
				t.Fatal("context omitted current facade schema or source revision")
			}
			// 只在测试中投影不含Exec的字节用于增量比较，不构造旧目录或兼容reader。
			var projected struct {
				Facades []json.RawMessage `json:"facades"`
			}
			if err := json.Unmarshal(view.CanonicalJSON(), &projected); err != nil {
				t.Fatal(err)
			}
			var withoutExec []json.RawMessage
			for index, facade := range view.Facades() {
				if facade.Capability() != tool.CapabilityExec {
					withoutExec = append(withoutExec, projected.Facades[index])
				}
			}
			projected.Facades = withoutExec
			baseline, err := codec.MarshalCanonical(projected, MaxSegmentBytes)
			if err != nil {
				t.Fatal(err)
			}
			beforeTokens, _ := estimate.Bytes(baseline.Bytes()).Tokens()
			sum := sha256.Sum256(baseline.Bytes())
			beforeFingerprint := hex.EncodeToString(sum[:])
			beforeJSON, err := codec.MarshalCanonical(struct {
				Facade      json.RawMessage `json:"facade"`
				Fingerprint string          `json:"fingerprint"`
			}{baseline.Bytes(), beforeFingerprint}, MaxSegmentBytes)
			if err != nil {
				t.Fatal(err)
			}
			beforeSegment, err := NewSegment(string(SourceToolCatalog), StabilityStable, beforeFingerprint, beforeJSON)
			if err != nil {
				t.Fatal(err)
			}
			segments := plan.CachePlan().Segments()
			segments[1] = beforeSegment
			beforePlan, err := NewPlan(segments...)
			if err != nil {
				t.Fatal(err)
			}
			beforePrefix, _ := beforePlan.StablePrefixFingerprint()
			if beforeTokens >= catalogTokens || beforePrefix == prefix || beforeSegment.Fingerprint() == plan.CachePlan().Segments()[1].Fingerprint() {
				t.Fatal("Exec facade did not invalidate catalog estimate and stable prefix")
			}
			actual := fmt.Sprintf("catalog_tokens=%d\ntotal_tokens=%d\nwithout_exec_tokens=%d\ncatalog_revision=%s\nstable_prefix=%s\n", catalogTokens, total, beforeTokens, view.Fingerprint(), prefix)
			golden, err := os.ReadFile("testdata/exec_catalog_" + string(family) + ".golden")
			if err != nil || string(golden) != actual {
				t.Fatalf("planning golden mismatch: %v; actual=%q", err, actual)
			}
			for _, dynamic := range []string{"command=go test ./... workdir=src timeout=1000 approval=one env=secret-one", "command=make verify workdir=test timeout=2000 approval=two env=secret-two"} {
				changedHistory := domain.SemanticHistoryView{Provider: family, Turns: []domain.SemanticTurn{{UserText: dynamic, AssistantText: "command completed"}}}
				changedInput, err := NewPlanningInput(profile, catalog, emptyProjectInstructions(t), changedHistory, estimatedFootprint(t, family, 1, 100), NewUserTextInput(dynamic), DisabledBudget())
				if err != nil {
					t.Fatal(err)
				}
				changed, err := NewPlanner().Plan(changedInput)
				if err != nil {
					t.Fatal(err)
				}
				changedPrefix, _ := changed.CachePlan().StablePrefixFingerprint()
				if changedPrefix != prefix || string(changed.CachePlan().Segments()[1].CanonicalJSON()) != string(plan.CachePlan().Segments()[1].CanonicalJSON()) || changed.Sources()[1].Revision() != view.Fingerprint() {
					t.Fatal("command world state polluted stable prefix")
				}
				if strings.Contains(string(changed.CachePlan().Segments()[1].CanonicalJSON()), "secret-") {
					t.Fatal("secret entered catalog")
				}
			}
		})
	}
}
