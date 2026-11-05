package session

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"testing"

	"easycode/internal/domain"
)

func TestCurrentHookFixturesAndCanonicalBytes(t *testing.T) {
	for _, phase := range []string{"planned", "started", "prefix", "continued", "denied", "error", "headless", "recovered", "tool_allowed", "tool_started", "tool_result", "outputs"} {
		t.Run(phase, func(t *testing.T) {
			fixture := readCurrentFixture(t, "hook_"+phase+".jsonl")
			for _, line := range bytes.Split(bytes.TrimSuffix(fixture, []byte("\n")), []byte("\n")) {
				r, err := DecodeRecord(line)
				if err != nil {
					t.Fatal(err)
				}
				_, canonical, err := EncodeRecord(r)
				if err != nil || !bytes.Equal(line, canonical) {
					t.Fatal("hook canonical bytes changed", err)
				}
			}
			repo, lease := installCurrentFixture(t, fixture)
			defer repo.Close()
			defer lease.Close()
			loaded, err := NewLoader().Load(context.Background(), lease)
			if err != nil {
				t.Fatal(err)
			}
			plan, err := NewReplayPlanner().Plan(loaded)
			if err != nil {
				t.Fatal(err)
			}
			if plan.ToolRecovery == nil || len(plan.Tools) != 1 {
				t.Fatal("missing hook recovery/history")
			}
			if phase == "outputs" {
				if !plan.ToolRecovery.ToolOutputsCommitted() || len(plan.Tools[0].Hooks) != 2 {
					t.Fatal("lost committed hook history")
				}
				return
			}
			g := plan.ToolRecovery.Calls()[0].Hooks
			if g == nil || len(g.Plan.Hooks) != 2 {
				t.Fatal("lost frozen plan")
			}
			counts := map[string]int{"planned": 0, "started": 0, "prefix": 1, "continued": 2, "denied": 1, "error": 1, "headless": 0, "recovered": 1, "tool_allowed": 2, "tool_started": 2, "tool_result": 2}
			if len(g.Results) != counts[phase] || len(plan.Tools[0].Hooks) != counts[phase] {
				t.Fatal("lost hook statistics")
			}
			if (phase == "started") != (g.Started != nil) {
				t.Fatal("wrong active hook")
			}
			if phase == "recovered" && (g.Results[0].Origin != "recovery" || g.Decision.Status != domain.HookUncertain) {
				t.Fatal("uncertain recovery changed")
			}
		})
	}
}

func TestHookSemanticCorruptionPrecedesTornTailRepair(t *testing.T) {
	content := append(readCurrentFixture(t, "hook_invalid.jsonl"), []byte(`{"seq":`)...)
	repo, lease := installCurrentFixture(t, content)
	defer repo.Close()
	defer lease.Close()
	loaded, err := NewLoader().Load(context.Background(), lease)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.NeedsRepair() {
		t.Fatal("missing torn tail")
	}
	if _, err := NewReplayPlanner().Plan(loaded); err == nil {
		t.Fatal("skipped ordinal accepted")
	}
	path, _ := repo.JournalPath(testThreadID)
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, content) {
		t.Fatal("semantic failure repaired bytes")
	}
}

func TestHookLeaseAcrossProcessesRecoveryAndMonotonicAppend(t *testing.T) {
	for _, phase := range []string{"planned", "started", "prefix"} {
		t.Run(phase, func(t *testing.T) {
			content := append(readCurrentFixture(t, "hook_"+phase+".jsonl"), []byte(`{"seq":`)...)
			root := createLeaseProcessJournal(t, content)
			repo, err := OpenOrCreateRepository(root)
			if err != nil {
				t.Fatal(err)
			}
			defer repo.Close()
			helper := startJournalLeaseHelper(t, root)
			if _, err := repo.Open(context.Background(), testThreadID); !IsJournalBusy(err) {
				t.Fatal("lease contention admitted repair", err)
			}
			path, _ := repo.JournalPath(testThreadID)
			during, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(content, during) {
				t.Fatal("busy acquisition mutated journal")
			}
			helper.kill(t)
			lease, err := repo.Open(context.Background(), testThreadID)
			if err != nil {
				t.Fatal(err)
			}
			loaded, err := NewLoader().Load(context.Background(), lease)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := NewReplayPlanner().Plan(loaded); err != nil {
				t.Fatal(err)
			}
			if _, err := NewLoader().Repair(context.Background(), lease, loaded); err != nil {
				t.Fatal(err)
			}
			competitor := startHookLeaseCompetitor(t, root)
			if competitor {
				t.Fatal("repair lost lease")
			}
			writer, err := StartJournalWriter(lease, loaded.Identity, loaded.NextSequence)
			if err != nil {
				t.Fatal(err)
			}
			call := func() domain.HookPlan {
				plan, err := NewReplayPlanner().Plan(loaded)
				if err != nil {
					t.Fatal(err)
				}
				return plan.ToolRecovery.Calls()[0].Hooks.Plan
			}()
			var drafts []RecordDraft
			if phase == "planned" || phase == "prefix" {
				ordinal := 0
				if phase == "prefix" {
					ordinal = 1
				}
				id, _ := domain.NewHookRunIdentity(call, ordinal)
				d, _ := NewHookExecutionStartedDraft(testTurnID, id)
				drafts = []RecordDraft{d}
			} else {
				id, _ := domain.NewHookRunIdentity(call, 0)
				r := domain.HookRunResult{Identity: id, Status: domain.HookContinue, Code: domain.HookOK, ExitKnown: true, CleanupConfirmed: true, Origin: "execution"}
				d, _ := NewHookExecutionFinishedDraft(testTurnID, r)
				drafts = []RecordDraft{d}
			}
			records, err := writer.AppendBatch(context.Background(), drafts)
			if err != nil || records[0].Sequence != loaded.NextSequence {
				t.Fatal("nonmonotonic sequence", err)
			}
			if err := writer.Close(context.Background()); err != nil {
				t.Fatal(err)
			}
			after, err := os.ReadFile(path)
			prefix := readCurrentFixture(t, "hook_"+phase+".jsonl")
			if err != nil || !bytes.HasPrefix(after, prefix) {
				t.Fatal("append rewrote committed prefix")
			}
		})
	}
}

// startHookLeaseCompetitor 使用独立 Repository 验证恢复阶段继续持有原 lease。
func startHookLeaseCompetitor(t *testing.T, root string) bool {
	t.Helper()
	repo, err := OpenOrCreateRepository(root)
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	lease, err := repo.Open(context.Background(), testThreadID)
	if err == nil {
		lease.Close()
		return true
	}
	if !IsJournalBusy(err) {
		t.Fatal(err)
	}
	return false
}

func TestHookCurrentReplayExpectationsAreFixed(t *testing.T) {
	var want map[string]struct {
		Next      uint64 `json:"next"`
		Completed int    `json:"completed"`
		Started   bool   `json:"started"`
		Gate      string `json:"gate"`
		ToolState string `json:"tool_state"`
		Outputs   bool   `json:"outputs"`
	}
	if err := json.Unmarshal(readCurrentFixture(t, "hook_replay_plan.json"), &want); err != nil {
		t.Fatal(err)
	}
	for phase, expected := range want {
		t.Run(phase, func(t *testing.T) {
			repo, lease := installCurrentFixture(t, readCurrentFixture(t, "hook_"+phase+".jsonl"))
			defer repo.Close()
			defer lease.Close()
			loaded, err := NewLoader().Load(context.Background(), lease)
			if err != nil {
				t.Fatal(err)
			}
			plan, err := NewReplayPlanner().Plan(loaded)
			if err != nil {
				t.Fatal(err)
			}
			got := expected
			got.Next = plan.NextSequence
			got.Outputs = plan.ToolRecovery.ToolOutputsCommitted()
			got.Completed = len(plan.Tools[0].Hooks)
			got.Started = false
			got.Gate = ""
			got.ToolState = ""
			if !got.Outputs {
				call := plan.ToolRecovery.Calls()[0]
				got.ToolState = string(call.State)
				got.Started = call.Hooks.Started != nil
				if call.Hooks.Decision != nil {
					got.Gate = string(call.Hooks.Decision.Status)
				}
			}
			if got != expected {
				t.Fatalf("hook replay expectation changed: %#v want %#v", got, expected)
			}
		})
	}
}
