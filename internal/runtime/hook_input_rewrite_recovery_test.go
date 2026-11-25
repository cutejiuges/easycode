package runtime

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/session"
	"easycode/internal/tool"
)

func TestPreRewriteRecoveryFromFixedCrashCutsNeverRerunsAndPreservesFrozenBytes(t *testing.T) {
	for _, phase := range []string{"prefix", "next_started", "continued", "allowed", "started", "result", "recovery", "reverted", "rejected"} {
		t.Run(phase, func(t *testing.T) {
			fixture, err := os.ReadFile(filepath.Join("..", "session", "testdata", "current", "rewrite_"+phase+".jsonl"))
			requireNoError(t, err)
			const thread = domain.ThreadID("00000000-0001-7000-8000-000000000002")
			repo, err := session.OpenOrCreateRepository(filepath.Join(t.TempDir(), "sessions"))
			requireNoError(t, err)
			defer repo.Close()
			lease, err := repo.Create(context.Background(), thread)
			requireNoError(t, err)
			path, err := repo.JournalPath(thread)
			requireNoError(t, err)
			if err := os.WriteFile(path, fixture, 0600); err != nil {
				t.Fatal(err)
			}
			loaded, err := session.NewLoader().Load(context.Background(), lease)
			requireNoError(t, err)
			plan, err := session.NewReplayPlanner().Plan(loaded)
			if err != nil || plan.ToolRecovery == nil {
				t.Fatal("missing crash recovery", err)
			}
			writer, err := session.StartJournalWriter(lease, loaded.Identity, loaded.NextSequence)
			requireNoError(t, err)
			defer writer.Close(context.Background())
			r, _, runner, _ := hookRuntimeFixture(t, writer, 0)
			bindRuntimeRecoveryFixture(t, r, *plan.ToolRecovery)
			prepares, executes, _ := rewriteCandidateFixture(t, r, plan.Tools[0].Invocation)
			if err := r.ReconcileToolTurn(context.Background(), *plan.ToolRecovery); err != nil {
				t.Fatal(err)
			}
			if runner.runs != 0 || runner.preflights != 0 || prepares() != 0 || executes() != 0 {
				t.Fatal("recovery reopened old external sources")
			}
			conversation := r.conversation.(*fakeToolConversation)
			if conversation.calls.Load() != 0 || len(conversation.results) != 1 {
				t.Fatal("recovery contacted network or lost result pairing")
			}
			result := conversation.results[0]
			if plan.Tools[0].HasResult {
				if result.Preview() != plan.Tools[0].Result.Preview() || result.Budget() != plan.Tools[0].Result.Budget() {
					t.Fatal("existing frozen input disclosure changed")
				}
			} else if phase == "continued" || phase == "allowed" || phase == "started" {
				if result.Budget().Reason() != tool.BudgetRecovery || result.Budget().ActualBytes() > tool.SafetyResultBytes || !strings.Contains(result.Preview().Text(), "recovery did not rerun") || strings.Contains(result.Preview().Text(), `"file_path"`) {
					t.Fatal("missing result was redisclosed or treated as executed")
				}
			} else if strings.Contains(result.Preview().Text(), "PreToolUse changed input") {
				t.Fatal("partial candidate became an effective input")
			}
			if err := writer.Close(context.Background()); err != nil {
				t.Fatal(err)
			}
			lease, err = repo.Open(context.Background(), thread)
			requireNoError(t, err)
			defer lease.Close()
			after, err := os.ReadFile(path)
			if err != nil || !bytes.HasPrefix(after, fixture) {
				t.Fatal("recovery rewrote old facts", err)
			}
			reloaded, err := session.NewLoader().Load(context.Background(), lease)
			requireNoError(t, err)
			sealed, err := session.NewReplayPlanner().Plan(reloaded)
			if err != nil || sealed.ToolRecovery != nil || len(sealed.Tools) != 1 || sealed.Tools[0].Result.Preview() != result.Preview() {
				t.Fatal("second crash/reload changed compensation", err)
			}
			results, gates := 0, 0
			for _, record := range reloaded.Records {
				if record.EventKind == session.EventToolCallResult {
					results++
				}
				if record.EventKind == session.EventToolHookGateDecided {
					gates++
				}
			}
			if results != 1 || gates != 1 || reloaded.NextSequence <= loaded.NextSequence {
				t.Fatal("recovery duplicated selected input/result or sequence")
			}
		})
	}
}
