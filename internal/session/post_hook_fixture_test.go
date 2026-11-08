package session

import (
	"bytes"
	"context"
	"easycode/internal/domain"
	"easycode/internal/tool"
	"os"
	"testing"
)

func TestCurrentPostHookFixturesAreFixedAndStrict(t *testing.T) {
	for _, test := range []struct {
		name      string
		processed int
		pending   bool
		terminal  domain.PostHookDisposition
		code      domain.HookCode
		suffix    int
	}{
		{"planned", 0, false, "", "", 0}, {"result", 0, false, "", "", 0}, {"started", 0, true, "", "", 0}, {"prefix", 1, false, "", "", 0}, {"prefix_started", 1, true, "", "", 0},
		{"completed", 2, false, domain.PostHookCompleted, domain.HookExitFailed, 0},
		{"none", 1, false, domain.PostHookCompleted, domain.HookOK, 0},
		{"denied", 0, false, domain.PostHookSkipped, domain.HookTrustRequired, 0},
		{"budget_suffix", 1, false, domain.PostHookSkipped, domain.HookExecutionBudgetExhausted, 1},
		{"input_limit", 0, false, domain.PostHookSkipped, domain.HookInputLimit, 2},
		{"recovered_unstarted", 1, false, domain.PostHookCancelled, domain.HookPostInterrupted, 0},
		{"recovered_started", 2, false, domain.PostHookUncertain, domain.HookOutcomeUncertain, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixed := readCurrentFixture(t, "post_hook_"+test.name+".jsonl")
			for _, line := range bytes.Split(bytes.TrimSuffix(fixed, []byte("\n")), []byte("\n")) {
				r, err := DecodeRecord(line)
				if err != nil {
					t.Fatal(err)
				}
				_, encoded, err := EncodeRecord(r)
				if err != nil || !bytes.Equal(encoded, line) {
					t.Fatal("current post bytes changed", err)
				}
			}
			repo, lease := installCurrentFixture(t, fixed)
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
				t.Fatal("lost post recovery/history")
			}
			call := plan.ToolRecovery.Calls()[0]
			g := call.PostHooks
			if g == nil || len(g.Finished) != test.processed || (g.Started != nil) != test.pending {
				t.Fatal("collapsed post prefix or admission")
			}
			if test.terminal == "" {
				if g.Terminal != nil {
					t.Fatal("invented terminal")
				}
			} else if g.Terminal == nil || g.Terminal.Disposition != test.terminal || g.Terminal.Code != test.code || len(g.Terminal.SuffixFeedback) != test.suffix {
				t.Fatal("wrong post terminal")
			}
			if test.name == "budget_suffix" && (g.Finished[0].Result.TimedOut || g.Terminal.Processed != 1 || g.Terminal.UnstartedFrom != 1) {
				t.Fatal("invented suffix execution statistics")
			}
			if test.name == "input_limit" {
				if len(g.Finished) != 0 || g.Started != nil || call.Result.Status() != tool.ResultSuccess {
					t.Fatal("oversized stdin rewrote tool or started")
				}
				patch, _ := call.Invocation.Patch()
				rs := patch.Input().Replacements()
				_, err := domain.NewHookPatchInput(patch.Input().FilePath(), []domain.HookReplacement{{OldText: rs[0].OldText(), NewText: rs[0].NewText()}})
				if err != domain.HookInputLimit {
					t.Fatal("input limit fixture is not oversized", err)
				}
			}
			if g.Terminal != nil && g.Terminal.Origin == "recovery" && len(g.Terminal.SuffixFeedback) != 0 {
				t.Fatal("recovery invented model notices")
			}
		})
	}
}
func TestPostSemanticCorruptionPrecedesTornTailRepair(t *testing.T) {
	content := append(readCurrentFixture(t, "post_hook_invalid.jsonl"), []byte(`{"seq":`)...)
	repo, lease := installCurrentFixture(t, content)
	defer repo.Close()
	defer lease.Close()
	path, _ := repo.JournalPath(testThreadID)
	loaded, err := NewLoader().Load(context.Background(), lease)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewReplayPlanner().Plan(loaded); err == nil {
		t.Fatal("forged terminal budget accepted")
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(content, after) {
		t.Fatal("semantic corruption repaired before validation")
	}
}
func TestPostLeaseCrashReleaseRepairAndMonotonicAppend(t *testing.T) {
	for _, name := range []string{"result", "prefix_started"} {
		t.Run(name, func(t *testing.T) {
			fixed := readCurrentFixture(t, "post_hook_"+name+".jsonl")
			content := append(bytes.Clone(fixed), []byte(`{"seq":`)...)
			root := createLeaseProcessJournal(t, content)
			repo, err := OpenOrCreateRepository(root)
			if err != nil {
				t.Fatal(err)
			}
			defer repo.Close()
			helper := startJournalLeaseHelper(t, root)
			if _, err := repo.Open(context.Background(), testThreadID); !IsJournalBusy(err) {
				t.Fatal("post activity admitted repair", err)
			}
			path, _ := repo.JournalPath(testThreadID)
			during, _ := os.ReadFile(path)
			if !bytes.Equal(content, during) {
				t.Fatal("busy post repair changed bytes")
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
			plan, err := NewReplayPlanner().Plan(loaded)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := NewLoader().Repair(context.Background(), lease, loaded); err != nil {
				t.Fatal(err)
			}
			if startHookLeaseCompetitor(t, root) {
				t.Fatal("repair lost exclusive post lease")
			}
			call := plan.ToolRecovery.Calls()[0]
			g := call.PostHooks
			var drafts []RecordDraft
			if g.Started == nil {
				id, _ := domain.NewPostHookRunIdentity(g.Plan, len(g.Finished))
				d, _ := NewHookExecutionStartedDraft(testTurnID, id)
				drafts = []RecordDraft{d}
			} else {
				stats := domain.HookRunResult{Identity: *g.Started, Status: domain.HookUncertain, Code: domain.HookOutcomeUncertain, Origin: "recovery"}
				frozen, _ := domain.NewEmptyHookFinished(stats, g.Plan.Plan.Hooks[g.Started.Ordinal])
				finished, _ := NewHookExecutionFinishedDraft(testTurnID, frozen)
				prefix := append(append([]domain.HookFinished(nil), g.Finished...), frozen)
				terminal := postTerminalDraft(t, g.Plan, call.Result, prefix, domain.PostHookUncertain, domain.HookOutcomeUncertain, "recovery")
				drafts = []RecordDraft{finished, terminal}
			}
			writer, err := StartJournalWriter(lease, loaded.Identity, loaded.NextSequence)
			if err != nil {
				t.Fatal(err)
			}
			records, err := writer.AppendBatch(context.Background(), drafts)
			if err != nil || records[0].Sequence != loaded.NextSequence {
				t.Fatal("post append sequence changed", err)
			}
			if err := writer.Close(context.Background()); err != nil {
				t.Fatal(err)
			}
			after, _ := os.ReadFile(path)
			if !bytes.HasPrefix(after, fixed) {
				t.Fatal("post append rewrote frozen prefix")
			}
			next, err := repo.Open(context.Background(), testThreadID)
			if err != nil {
				t.Fatal(err)
			}
			defer next.Close()
			again, err := NewLoader().Load(context.Background(), next)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := NewReplayPlanner().Plan(again); err != nil {
				t.Fatal(err)
			}
		})
	}
}
