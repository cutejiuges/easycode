package runtime

import (
	"context"
	"encoding/json"
	"testing"

	contextplan "easycode/internal/context"
	"easycode/internal/domain"
	"easycode/internal/protocol"
	"easycode/internal/tool"
)

// taskRecordingPlanner 只记录最终历史 metadata，用于核对真实规划入口。
type taskRecordingPlanner struct{ stamps []string }

func (p *taskRecordingPlanner) Plan(input contextplan.PlanningInput) (contextplan.ContextPlan, error) {
	plan, err := contextplan.NewPlanner().Plan(input)
	if err != nil {
		return contextplan.ContextPlan{}, err
	}
	for _, segment := range plan.CachePlan().Segments() {
		if segment.ID() == string(contextplan.SourceCommittedHistory) {
			var metadata struct {
				Stamp string `json:"task_content_digest"`
			}
			if err := json.Unmarshal(segment.CanonicalJSON(), &metadata); err != nil {
				return contextplan.ContextPlan{}, err
			}
			p.stamps = append(p.stamps, metadata.Stamp)
		}
	}
	return plan, nil
}
func (p *taskRecordingPlanner) requireAll(t *testing.T, want string, minPlans int) {
	t.Helper()
	if len(p.stamps) < minPlans {
		t.Fatalf("not all planning stages exercised: %v", p.stamps)
	}
	for _, stamp := range p.stamps {
		if stamp != want {
			t.Fatalf("planning lost task identity: got %q want %q", stamp, want)
		}
	}
}

func TestTaskStampBindsOrdinaryInputCandidateQueryFailureAndRestoredPlanning(t *testing.T) {
	r := taskRuntimeFixture(t, &fakeJournal{})
	recorder := &taskRecordingPlanner{}
	r.planner = recorder
	if _, err := r.contextPlanFor(r.operationProfile); err != nil {
		t.Fatal(err)
	}
	recorder.requireAll(t, "", 1)
	a := taskRuntimeInvocation(t, "稳定内容 A", domain.TaskInProgress, "task-a")
	if _, err := executeReadyFixtureCalls(t, r, context.Background(), runtimeTurnID, []tool.Invocation{a}); err != nil {
		t.Fatal(err)
	}
	saved := r.TaskProgress()
	stamp := saved.ContentStamp()
	recorder.stamps = nil
	if _, err := r.contextPlanFor(r.operationProfile); err != nil {
		t.Fatal(err)
	}
	committed := r.commitInputs(context.Background(), runtimeTurnID, 0, runtimeInputs("新的用户输入"), nil, func(protocol.Event) {})
	if committed.err != nil || !committed.committed {
		t.Fatal("input candidate path not exercised", committed.err)
	}
	if _, err := r.contextPlanFor(r.operationProfile); err != nil {
		t.Fatal(err)
	}
	r.tools.Policy = taskDenyPolicy{}
	b := taskRuntimeInvocation(t, "失败内容 B", domain.TaskPending, "task-b")
	if results, err := executeReadyFixtureCalls(t, r, context.Background(), runtimeTurnID, []tool.Invocation{b}); err != nil || results[0].Status() != tool.ResultDenied {
		t.Fatal("failure path not exercised", err)
	}
	if _, err := r.contextSnapshot(); err != nil {
		t.Fatal(err)
	}
	// 读取不会推进内容身份，失败更新仍保留 A。
	if !r.TaskProgress().Equal(saved) {
		t.Fatal("failed update changed task state")
	}
	if _, err := r.contextPlanFor(r.operationProfile); err != nil {
		t.Fatal(err)
	}
	recorder.requireAll(t, stamp, 5)
	config := testRuntimeConfig(t, &fakeJournal{})
	config.InitialTaskProgress = saved
	restoredRecorder := &taskRecordingPlanner{}
	config.ContextPlanner = restoredRecorder
	restored, err := New(&fakeConversation{}, config)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := restored.contextPlanFor(restored.operationProfile); err != nil {
		t.Fatal(err)
	}
	restoredRecorder.requireAll(t, stamp, 1)
}

func TestTaskStampIsPreservedByManualAndAutomaticCompactionPlans(t *testing.T) {
	inv := taskRuntimeInvocation(t, "压缩仍保留", domain.TaskCompleted, "task")
	task, _ := inv.Task()
	update := task.Input().Update()
	source, _ := domain.NewTaskProgressSource(7, runtimeTurnID, string(inv.InvocationID()))
	saved, _ := domain.NewTaskProgressState(source, update)
	for _, automatic := range []bool{false, true} {
		name := "manual"
		if automatic {
			name = "automatic"
		}
		t.Run(name, func(t *testing.T) {
			c := newCompactionConversation()
			recorder := &taskRecordingPlanner{}
			config := testRuntimeConfig(t, &fakeJournal{})
			config.InitialTaskProgress = saved
			config.ContextPlanner = recorder
			r, err := New(c, config)
			if err != nil {
				t.Fatal(err)
			}
			if automatic {
				budget, _ := contextplan.NewBudget(60000, 0, 0)
				r.budget = budget
				r.autoCompactionPolicy, _ = contextplan.NewAutoCompactionPolicy(true, budget, 4096)
				if err := r.prepareAutomaticSample(context.Background(), runtimeTurnID, 1, nil, func(protocol.Event) {}); err != nil {
					t.Fatal(err)
				}
			} else {
				admission, code := r.preflightCompaction("")
				if code != "" {
					t.Fatal(code)
				}
				if !admission.preview.Input().TaskProgress().Equal(saved) {
					t.Fatal("compaction did not freeze exact task state")
				}
				if err := r.runCompaction(context.Background(), "01900000-0000-7000-8000-000000000099", admission, func(protocol.Event) {}); err != nil {
					t.Fatal(err)
				}
			}
			if !c.installed.Load() || !r.TaskProgress().Equal(saved) {
				t.Fatal("summary reset task state")
			}
			if _, err := r.contextPlanFor(r.operationProfile); err != nil {
				t.Fatal(err)
			}
			recorder.requireAll(t, saved.ContentStamp(), 3)
		})
	}
}
