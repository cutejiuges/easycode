package context

import (
	"bytes"
	"strings"
	"testing"

	"easycode/internal/domain"
)

func TestTaskContentStampChangesOnlyCommittedHistoryIdentity(t *testing.T) {
	for _, family := range []domain.ProviderFamily{domain.ProviderAnthropic, domain.ProviderOpenAI} {
		t.Run(string(family), func(t *testing.T) {
			profile, _ := NewProviderProfile(family, "fixture-model")
			history := domain.SemanticHistoryView{Provider: family, Turns: []domain.SemanticTurn{planningTurn("same", "summary")}}
			input, err := testPlanningInput(profile, testToolCatalog(t), emptyProjectInstructions(t), history, estimatedFootprint(t, family, 7, 100), NewSamplingInput(), DisabledBudget())
			if err != nil {
				t.Fatal(err)
			}
			states := []domain.TaskProgressState{domain.NewNoTaskProgress()}
			for i, label := range []string{"任务甲", "任务乙", "任务甲", ""} {
				var items []domain.TaskStep
				if label != "" {
					step, _ := domain.NewTaskStep(label, "", domain.TaskPending)
					items = []domain.TaskStep{step}
				}
				update, _ := domain.NewTaskUpdate(items, "")
				source, _ := domain.NewTaskProgressSource(uint64(i+10), "01900000-0000-7000-8000-000000000001", "01900000-0000-7000-8000-000000000002")
				state, err := domain.NewTaskProgressState(source, update)
				if err != nil {
					t.Fatal(err)
				}
				states = append(states, state)
			}
			plans := make([]ContextPlan, len(states))
			for i, state := range states {
				candidate, err := input.WithTaskProgress(state)
				if err != nil {
					t.Fatal(err)
				}
				plans[i], err = NewPlanner().Plan(candidate)
				if err != nil {
					t.Fatal(err)
				}
				if len(plans[i].Sources()) != 4 || plans[i].TotalEstimate() != plans[0].TotalEstimate() && i > 0 {
					t.Fatal("task stamp changed estimation/source count")
				}
				raw := plans[i].CachePlan().Segments()[2].CanonicalJSON()
				if bytes.Contains(raw, []byte("任务")) || bytes.Contains(raw, []byte("invocation_id")) || bytes.Contains(raw, []byte("profile")) {
					t.Fatal("task content/source leaked into metadata")
				}
				if (i == 0) == bytes.Contains(raw, []byte("task_content_digest")) {
					t.Fatal("none stamp presence mismatch")
				}
			}
			for _, pair := range [][2]int{{0, 1}, {1, 2}, {0, 4}} {
				a, b := plans[pair[0]].CachePlan().Segments(), plans[pair[1]].CachePlan().Segments()
				if a[2].Fingerprint() == b[2].Fingerprint() {
					t.Fatal("history ignored equal-length content replacement or clear")
				}
				for _, index := range []int{0, 1, 3} {
					if a[index].Fingerprint() != b[index].Fingerprint() {
						t.Fatal("stamp polluted unrelated source")
					}
				}
				ap, _ := plans[pair[0]].CachePlan().StablePrefixFingerprint()
				bp, _ := plans[pair[1]].CachePlan().StablePrefixFingerprint()
				if ap != bp {
					t.Fatal("dynamic task polluted stable prefix")
				}
			}
			if !bytes.Equal(plans[1].CachePlan().Segments()[2].CanonicalJSON(), plans[3].CachePlan().Segments()[2].CanonicalJSON()) {
				t.Fatal("same content with different source changed stamp")
			}
			if _, err := input.WithTaskProgress(domain.TaskProgressState{}); err == nil {
				t.Fatal("zero state accepted")
			}
		})
	}
}

func TestTaskStampDoesNotDoubleCountNativeFootprint(t *testing.T) {
	history := domain.SemanticHistoryView{Provider: domain.ProviderOpenAI, Turns: []domain.SemanticTurn{planningTurn(strings.Repeat("a", 400), "")}}
	step, _ := domain.NewTaskStep("保留进度", "", domain.TaskPending)
	update, _ := domain.NewTaskUpdate([]domain.TaskStep{step}, "")
	source, _ := domain.NewTaskProgressSource(8, "01900000-0000-7000-8000-000000000001", "01900000-0000-7000-8000-000000000002")
	state, _ := domain.NewTaskProgressState(source, update)
	for _, native := range []uint64{100, 150} {
		input := mustPlanningInput(t, history, estimatedFootprint(t, domain.ProviderOpenAI, 9, native), "", DisabledBudget())
		input, err := input.WithTaskProgress(state)
		if err != nil {
			t.Fatal(err)
		}
		plan, err := NewPlanner().Plan(input)
		if err != nil {
			t.Fatal(err)
		}
		got, known := plan.Sources()[2].Estimate().Tokens()
		if !known || got != native {
			t.Fatalf("task changed max visible/native coverage: native=%d got=%d", native, got)
		}
	}
}
