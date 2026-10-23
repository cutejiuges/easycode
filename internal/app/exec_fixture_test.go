package app

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"easycode/internal/config"
	"easycode/internal/domain"
	"easycode/internal/provider"
	"easycode/internal/secret"
	"easycode/internal/session"
	"easycode/internal/tool"
)

func TestImmutableExecFixtureRestoresRealProviderNativePairs(t *testing.T) {
	for _, family := range []domain.ProviderFamily{domain.ProviderOpenAI, domain.ProviderAnthropic} {
		t.Run(string(family), func(t *testing.T) {
			fixture, err := os.ReadFile(filepath.Join("..", "session", "testdata", "current", "heterogeneous_exec_"+string(family)+".jsonl"))
			if err != nil {
				t.Fatal(err)
			}
			thread := domain.ThreadID("00000000-0001-7000-8000-000000000002")
			repository, err := session.OpenOrCreateRepository(filepath.Join(t.TempDir(), "sessions"))
			if err != nil {
				t.Fatal(err)
			}
			defer repository.Close()
			lease, err := repository.Create(context.Background(), thread)
			if err != nil {
				t.Fatal(err)
			}
			if err := lease.Close(); err != nil {
				t.Fatal(err)
			}
			path, err := repository.JournalPath(thread)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, fixture, 0600); err != nil {
				t.Fatal(err)
			}
			lease, err = repository.Open(context.Background(), thread)
			if err != nil {
				t.Fatal(err)
			}
			defer lease.Close()
			loaded, err := session.NewLoader().Load(context.Background(), lease)
			if err != nil {
				t.Fatal(err)
			}
			plan, err := session.NewReplayPlanner().Plan(loaded)
			if err != nil {
				t.Fatal(err)
			}
			factory, err := newProviderResource(config.Config{Provider: config.Provider{Family: family, Model: "fixture-model", BaseURL: "https://example.invalid", APIKey: secret.New("fixture-private-key")}})
			if err != nil {
				t.Fatal(err)
			}
			defer factory.Close()
			var commits []provider.NativeCommitEnvelope
			for _, native := range plan.NativeCommits {
				payload := native.Commit
				commit, err := provider.NewNativeCommitEnvelope(payload.Provider, payload.Wire, payload.PayloadVersion, payload.Payload)
				if err != nil {
					t.Fatal(err)
				}
				commits = append(commits, commit)
			}
			conversation, err := factory.RestoreConversation(commits)
			if err != nil {
				t.Fatal(err)
			}
			view := conversation.ProjectHistory()
			if len(view.Turns) != 1 || view.Turns[0].UserText != "Inspect, edit and verify" {
				t.Fatal("native fixture changed history boundaries")
			}
			call := plan.ToolRecovery.Calls()[0]
			result := tool.NewInvocationErrorResult(call.Invocation, tool.ResultOutcomeUncertain, "outcome_uncertain", "Tool failed: execution outcome is uncertain")
			preparer, ok := conversation.(provider.ToolResultPreparer)
			if !ok {
				t.Fatal("restored Provider has no tool result codec")
			}
			prepared, err := preparer.PrepareToolOutputs([]tool.InvocationResult{result})
			if err != nil {
				t.Fatal(err)
			}
			envelope, err := prepared.Envelope()
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(envelope.Payload(), []byte("call-interrupted")) || !bytes.Contains(envelope.Payload(), []byte(result.Preview().Text())) {
				t.Fatal("restored Exec output lost pairing or frozen preview")
			}
			if err := prepared.Finalize(); err != nil {
				t.Fatal(err)
			}
			commits = append(commits, envelope)
			if _, err := factory.RestoreConversation(commits); err != nil {
				t.Fatalf("compensated native pairs invalid: %v", err)
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(after, fixture) {
				t.Fatal("native restore modified fixture bytes")
			}
		})
	}
}
