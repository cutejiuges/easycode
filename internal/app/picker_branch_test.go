//go:build darwin || linux

package app

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
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
	owner.branch = func(ctx, _ context.Context, request tui.PickerRequest, _ *chatResources, _ *rewindCandidate, admit func(domain.ThreadID) bool) pickerExecution {
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

func TestRewindPickerTransfersOrReleasesItsSourcePermit(t *testing.T) {
	ctx, stop := context.WithTimeout(context.Background(), 8*time.Second)
	defer stop()
	source, _, _ := startBranchGateSource(t, ctx)
	owner := newPickerOwner(nil)
	owner.initial = source
	selected := tui.RewindSelection{
		TurnID: "01900000-0000-7000-8000-000000000102", InputID: "01900000-0000-7000-8000-000000000103",
	}
	browseEntered := make(chan struct{})
	confirmEntered := make(chan bool, 1)
	confirmRelease := make(chan struct{})
	owner.rewind = func(ctx context.Context, request tui.PickerRequest, _ *chatResources) (tui.PickerResult, map[tui.RewindSelection]rewindCandidate) {
		if request.ID == 1 {
			close(browseEntered)
			<-ctx.Done()
		}
		return tui.PickerResult{
			Request: request, RewindEntries: []tui.RewindEntry{{RewindSelection: selected, Number: 2, Summary: "prompt"}},
		}, map[tui.RewindSelection]rewindCandidate{selected: {}}
	}
	owner.branch = func(ctx, _ context.Context, request tui.PickerRequest, _ *chatResources, candidate *rewindCandidate, _ func(domain.ThreadID) bool) pickerExecution {
		if request.BrowseID != 0 {
			confirmEntered <- candidate != nil
			select {
			case <-ctx.Done():
			case <-confirmRelease:
			}
		}
		return pickerExecution{result: tui.PickerResult{Request: request, Error: "injected branch preparation failure"}}
	}
	owner.Start(ctx)
	defer func() {
		owner.Stop()
		if err := owner.Wait(); err != nil {
			t.Error(err)
		}
	}()
	browse := tui.PickerRequest{ID: 1, Operation: tui.PickerRewind, Source: source.identity.ThreadID}
	submitPickerRequest(t, owner, browse)
	select {
	case <-browseEntered:
	case <-ctx.Done():
		t.Fatal("rewind did not enter source read")
	}
	owner.CancelResume(browse)
	if result := awaitPickerStage(t, ctx, owner, tui.PickerCancelled); result.Request != browse || len(result.RewindEntries) != 0 {
		t.Fatalf("cancelled browse leaked a late list: %+v", result)
	}

	browse.ID = 2
	submitPickerRequest(t, owner, browse)
	if result := awaitPickerStage(t, ctx, owner, tui.PickerCompleted); result.Request != browse || len(result.RewindEntries) != 1 {
		t.Fatalf("browse did not offer the historical choice: %+v", result)
	}
	latest := tui.PickerRequest{ID: 3, Operation: tui.PickerBranch, Source: source.identity.ThreadID}
	confirm := latest
	confirm.BrowseID = 1
	confirm.RewindSelection = selected
	if owner.Submit(confirm) || owner.Submit(latest) {
		t.Fatal("stale confirmation or unrelated branch changed the active browse")
	}
	confirm.BrowseID = browse.ID
	submitPickerRequest(t, owner, confirm)
	select {
	case received := <-confirmEntered:
		if !received {
			t.Fatal("historical branch lost its selection")
		}
	case <-ctx.Done():
		t.Fatal("confirmation did not reuse the held source permit")
	}
	duplicate := confirm
	duplicate.ID++
	if owner.Submit(duplicate) {
		t.Fatal("historical selection admitted a second confirmation")
	}
	close(confirmRelease)
	if result := awaitPickerStage(t, ctx, owner, tui.PickerCancelled); result.Request != confirm {
		t.Fatalf("historical failure did not settle its confirmation ID: %+v", result)
	}

	browse.ID = 4
	submitPickerRequest(t, owner, browse)
	_ = awaitPickerStage(t, ctx, owner, tui.PickerCompleted)
	confirm.ID, confirm.BrowseID = 5, 1
	if owner.Submit(confirm) {
		t.Fatal("stale confirmation unexpectedly admitted")
	}
	owner.CancelResume(browse)
	if result := awaitPickerStage(t, ctx, owner, tui.PickerCancelled); result.Request != browse {
		t.Fatalf("accepting a rejected confirmation stranded the browse permit: %+v", result)
	}
	latest.ID = 6
	submitPickerRequest(t, owner, latest)
	if result := awaitPickerStage(t, ctx, owner, tui.PickerCancelled); result.Request != latest || !strings.Contains(result.Error, "injected branch preparation failure") {
		t.Fatalf("ordinary branch did not independently obtain its source permit: %+v", result)
	}
}

func TestRewindProjectionMismatchKeepsSourceWritable(t *testing.T) {
	ctx, stop := context.WithTimeout(context.Background(), 8*time.Second)
	defer stop()
	paths, cwd, cfg := pickerFixture(t)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write(activeReasoningFixture(cfg.Provider.Family, int(calls.Add(1)), false))
	}))
	defer server.Close()
	cfg.Provider.BaseURL = server.URL
	owner, source, _ := startBranchFixture(t, ctx, cfg, paths, cwd)
	appEffortTurn(t, source, "retained prompt", false)
	waitCompactionIdle(t, source)
	appEffortTurn(t, source, "selected prompt", false)
	waitCompactionIdle(t, source)
	path, err := source.service.repository.JournalPath(source.identity.ThreadID)
	if err != nil {
		t.Fatal(err)
	}
	journal := readPickerJournal(t, path)

	browse := tui.PickerRequest{ID: 1, Operation: tui.PickerRewind, Source: source.identity.ThreadID}
	submitPickerRequest(t, owner, browse)
	list := awaitPickerStage(t, ctx, owner, tui.PickerCompleted)
	if len(list.RewindEntries) != 1 || list.RewindEntries[0].Summary != "selected prompt" {
		t.Fatalf("unexpected historical evidence: %+v", list)
	}
	entry := list.RewindEntries[0]
	queries := source.transcriptQueries
	queries.mu.Lock()
	var changed bool
	for i := 0; i < queries.nodes.len(); i++ {
		if node := queries.nodes.at(i); node.item.InputID == entry.InputID {
			node.item.Text = "changed projection"
			changed = true
			break
		}
	}
	queries.mu.Unlock()
	if !changed {
		t.Fatal("source transcript did not contain the selected prompt")
	}
	confirm := tui.PickerRequest{ID: 2, Operation: tui.PickerBranch, Source: browse.Source, BrowseID: browse.ID, RewindSelection: entry.RewindSelection}
	submitPickerRequest(t, owner, confirm)
	failed := awaitPickerStage(t, ctx, owner, tui.PickerCancelled)
	if failed.BranchPublication != tui.BranchNotPublished || !strings.Contains(failed.Error, "reopen rewind") ||
		calls.Load() != 2 || !bytes.Equal(journal, readPickerJournal(t, path)) {
		t.Fatalf("stale source projection changed the durable outcome: %+v", failed)
	}
	confirmTitle(t, ctx, source.session, "rename-after-projection-mismatch", "Source remains writable")
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
			owner.branch = func(ctx, _ context.Context, request tui.PickerRequest, _ *chatResources, _ *rewindCandidate, admit func(domain.ThreadID) bool) pickerExecution {
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
