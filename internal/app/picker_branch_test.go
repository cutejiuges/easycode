package app

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"easycode/internal/domain"
	"easycode/internal/fault"
	"easycode/internal/provider/openai"
	"easycode/internal/secret"
	"easycode/internal/session"
	"easycode/internal/tui"
)

func startBranchGateSource(t *testing.T, ctx context.Context) (*chatResources, tui.Model, string) {
	t.Helper()
	paths, cwd, cfg := pickerFixture(t)
	environment := &chatEnvironment{}
	t.Cleanup(func() { _ = environment.close() })
	source, err := openChatResourcesWithHooks(ctx, cfg, paths, "", false, cwd, chatResourceHooks{
		interactiveApprovals: true, environment: environment, lifetime: ctx,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = source.close(context.Background()) })
	model, err := assembleChatModel(ctx, source)
	if err != nil {
		t.Fatal(err)
	}
	return source, model, cwd
}

func TestBranchPublicationRejectsSourceLossAtAdmission(t *testing.T) {
	ctx, stop := context.WithTimeout(context.Background(), 8*time.Second)
	defer stop()
	sourceCtx, stopSource := context.WithCancel(ctx)
	defer stopSource()
	source, _, _ := startBranchGateSource(t, sourceCtx)
	owner := newPickerOwner(nil)
	owner.initial = source
	entered := make(chan struct{})
	admitted := make(chan bool, 1)
	const target domain.ThreadID = "01900000-0000-7000-8000-000000000101"
	owner.branch = func(ctx, _ context.Context, request tui.PickerRequest, _ *chatResources, admit func(domain.ThreadID) bool) pickerExecution {
		close(entered)
		select {
		case <-sourceCtx.Done():
		case <-ctx.Done():
		}
		admitted <- admit(target)
		return pickerExecution{result: tui.PickerResult{Request: request, Error: "branch source lost"}}
	}
	owner.Start(ctx)
	defer func() {
		owner.Stop()
		_ = owner.Wait()
	}()
	submitPickerRequest(t, owner, tui.PickerRequest{ID: 1, Operation: tui.PickerBranch, Source: source.identity.ThreadID})
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("branch did not reach the publication barrier")
	}
	stopSource()
	select {
	case allowed := <-admitted:
		if allowed {
			t.Fatal("branch publication admitted after source cancellation")
		}
	case <-ctx.Done():
		t.Fatal("publication gate did not complete")
	}
	if err := owner.Wait(); err == nil || owner.lastBranch.notice.Message() != "" {
		t.Fatalf("source loss did not prevent publication: %v, %#v", err, owner.lastBranch)
	}
}

func TestBranchTerminalCancellationPreservesAdmittedOutcome(t *testing.T) {
	for _, scenario := range []struct {
		name        string
		publication tui.BranchPublication
		message     string
	}{
		{"published", tui.BranchPublished, "Resume with easycode --resume "},
		{"uncertain", tui.BranchPublicationUncertain, "publication durability is uncertain for "},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			ctx, stop := context.WithTimeout(context.Background(), 8*time.Second)
			defer stop()
			source, chat, cwd := startBranchGateSource(t, ctx)
			owner := newPickerOwner(nil)
			owner.initial = source
			admitted := make(chan bool, 1)
			const target domain.ThreadID = "01900000-0000-7000-8000-000000000102"
			owner.branch = func(ctx, _ context.Context, request tui.PickerRequest, _ *chatResources, admit func(domain.ThreadID) bool) pickerExecution {
				admitted <- admit(target)
				<-ctx.Done()
				return pickerExecution{result: tui.PickerResult{Request: request, BranchThreadID: target, BranchPublication: scenario.publication}}
			}
			input, user := io.Pipe()
			defer input.Close()
			defer user.Close()
			runCtx, stopTerminal := context.WithCancel(ctx)
			defer stopTerminal()
			model := tui.NewChatShell(owner, tui.PickerConfig{Family: domain.ProviderOpenAI, Wire: "responses", Model: "gpt-test", CWD: cwd}, chat)
			done := make(chan Outcome, 1)
			go func() {
				done <- runPickerTerminal(runCtx, stopTerminal, Options{Input: input, Output: io.Discard}, owner, model)
			}()
			defer func() {
				stopTerminal()
				_ = owner.Wait()
			}()
			go func() { _, _ = io.WriteString(user, "/branch Trial\r") }()
			select {
			case allowed := <-admitted:
				if !allowed {
					t.Fatal("healthy branch failed to enter publication")
				}
			case <-ctx.Done():
				t.Fatal("terminal did not submit branch")
			}
			stopTerminal()
			select {
			case outcome := <-done:
				notice := outcome.BranchNotice.Message()
				if outcome.ExitCode() != 130 || outcome.Report != ReportComplete || !strings.Contains(notice, scenario.message+string(target)) {
					t.Fatalf("terminal cancellation omitted its branch result: %#v, %q", outcome, notice)
				}
				withFailure := interactiveCleanupOutcome(outcome, errors.New("injected cleanup failure"))
				if withFailure.ExitCode() != 1 || withFailure.BranchNotice.Message() != notice {
					t.Fatalf("environment cleanup lost the branch result: %#v", withFailure)
				}
			case <-ctx.Done():
				t.Fatal("terminal failed to release the branch owner")
			}
		})
	}
}

func TestContinueRevalidatesSelectedWorkspaceBeforeRepair(t *testing.T) {
	ctx := context.Background()
	paths, cwd, cfg := pickerFixture(t)
	target := newPickerFixtureThread(t, ctx, paths, cwd)
	repo, err := session.NewRepository(paths.sessionRoot)
	if err != nil {
		t.Fatal(err)
	}
	path, err := repo.JournalPath(target)
	if err != nil {
		t.Fatal(err)
	}
	first, rest, _ := bytes.Cut(readPickerJournal(t, path), []byte("\n"))
	record, err := session.DecodeRecord(first)
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := session.DecodeSessionMetaPayload(record)
	if err != nil {
		t.Fatal(err)
	}
	metadata.CreationCWD = filepath.Join(cwd, "different")
	draft, err := session.NewSessionMetaDraft(metadata)
	if err != nil {
		t.Fatal(err)
	}
	record.Payload = draft.PayloadBytes()
	_, changed, err := session.EncodeRecord(record)
	if err != nil {
		t.Fatal(err)
	}
	replacement := append(append(append(changed, '\n'), rest...), '{')
	resources, err := openChatResourcesWithHooks(ctx, cfg, paths, "", true, cwd, chatResourceHooks{
		afterContinueSelection: func(selected domain.ThreadID) {
			if selected != target {
				t.Fatalf("continue selected %s instead of %s", selected, target)
			}
			if err := os.WriteFile(path, replacement, 0600); err != nil {
				t.Fatal(err)
			}
		},
	})
	if resources != nil {
		_ = resources.close(ctx)
		t.Fatal("continue resumed a journal whose workspace changed after selection")
	}
	if fault.Project(err).Code != fault.CodeSessionIncompatible || !bytes.Equal(readPickerJournal(t, path), replacement) {
		t.Fatalf("continue repaired changed journal or lost mismatch: %v", err)
	}
}

func TestBranchInvalidProviderEvidenceClosesSourceWritesBeforePublication(t *testing.T) {
	ctx := context.Background()
	fixture, err := os.ReadFile("../session/testdata/current/question_openai_complete.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	invalid := corruptThinkingFact(t, fixture,
		`"reasoning_output":{"state":"unknown"}`, `"reasoning_output":{"state":"known","value":0}`)
	root, path, thread := installPreviewFixture(t, invalid)
	service, err := openSessionService(root)
	if err != nil {
		t.Fatal(err)
	}
	defer service.close()
	lease, err := service.repository.Open(ctx, thread)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := session.NewLoader().Load(ctx, lease)
	if err != nil {
		_ = lease.Close()
		t.Fatal(err)
	}
	source, err := session.StartJournalWriter(lease, loaded.Identity, loaded.NextSequence)
	if err != nil {
		_ = lease.Close()
		t.Fatal(err)
	}
	defer source.Close(ctx)
	instance, err := openai.New(openai.Config{BaseURL: "https://example.invalid", Model: "gpt-test", APIKey: secret.New("fixture-key")})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	target, err := domain.GenerateThreadID()
	if err != nil {
		t.Fatal(err)
	}
	creation := &branchCreation{source: source, target: target, admit: func(domain.ThreadID) bool {
		t.Error("invalid provider evidence reached publication")
		return false
	}}
	_, err = service.branch(ctx, testSessionProvider(instance, "responses"), "gpt-test", creation, func(context.Context) error { return nil })
	if !session.IsBranchSourceUnsafe(err) || !source.Poisoned() || !bytes.Equal(readPickerJournal(t, path), invalid) {
		t.Fatalf("branch did not preserve bytes and stop the corrupt source: %v", err)
	}
}
