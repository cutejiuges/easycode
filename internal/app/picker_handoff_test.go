package app

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"easycode/internal/config"
	"easycode/internal/domain"
	"easycode/internal/extension/hook"
	"easycode/internal/fault"
	"easycode/internal/protocol"
	"easycode/internal/secret"
	"easycode/internal/session"
	"easycode/internal/tui"
)

func TestInChatResumeOwnerKeepsSourceUntilCandidateIsAccepted(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	paths, cwd, cfg := pickerFixture(t)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		serveStopAssistant(w, domain.ProviderOpenAI, "picker fixture reply")
	}))
	defer server.Close()
	cfg.Provider.BaseURL = server.URL
	hookDir := filepath.Join(cwd, ".easycode")
	if err := os.Mkdir(hookDir, 0700); err != nil {
		t.Fatal(err)
	}
	const resumeHook = `{"hooks":{"SessionStart":[{"matcher":"resume","hooks":[{"id":"resume-context","type":"command","command":"printf '{}'"}]}]}}`
	if err := os.WriteFile(filepath.Join(hookDir, "hooks.json"), []byte(resumeHook), 0600); err != nil {
		t.Fatal(err)
	}
	target := newPickerFixtureThread(t, ctx, paths, cwd)
	environment := &chatEnvironment{}
	source, err := openChatResourcesWithHooks(ctx, cfg, paths, "", false, cwd, chatResourceHooks{interactiveApprovals: true, environment: environment})
	if err != nil {
		t.Fatal(err)
	}
	defer environment.close()
	defer source.close(context.Background())
	if _, err = assembleChatModel(ctx, source); err != nil {
		t.Fatal(err)
	}
	execute := newPickerExecutor(cfg, paths, cwd, nil, environment)
	prepared := make(chan struct{})
	adopted := make(chan *chatResources, 1)
	owner := newPickerOwner(func(opCtx, life context.Context, request tui.PickerRequest) pickerExecution {
		execution := execute(opCtx, life, request)
		if request.ID == 2 {
			close(prepared)
			<-opCtx.Done()
		}
		if request.ID == 5 {
			adopted <- execution.resources
		}
		return execution
	})
	owner.initial = source
	owner.Start(ctx)
	defer func() {
		owner.Stop()
		if err := owner.Wait(); err != nil {
			t.Error(err)
		}
	}()
	request := tui.PickerRequest{ID: 1, Operation: tui.PickerOpen, Source: source.identity.ThreadID, ThreadID: target}
	repo := source.service.repository
	targetPath, err := repo.JournalPath(target)
	if err != nil {
		t.Fatal(err)
	}
	before := readPickerJournal(t, targetPath)
	other := startAppSessionHelper(t, paths.sessionRoot, target)
	submitPickerRequest(t, owner, request)
	failed := awaitPickerStage(t, ctx, owner, tui.PickerCancelled)
	other.stop(t)
	if failed.Error != "session_busy: session thread is already active" || !bytes.Equal(before, readPickerJournal(t, targetPath)) {
		t.Fatalf("busy recovery changed target or misreported failure: %q", failed.Error)
	}
	request.ID = 2
	submitPickerRequest(t, owner, request)
	select {
	case <-prepared:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	requirePickerLeaseBusy(t, ctx, repo, source.identity.ThreadID)
	requirePickerLeaseBusy(t, ctx, repo, target)
	owner.CancelResume(request)
	awaitPickerStage(t, ctx, owner, tui.PickerCancelled)
	requirePickerLeaseBusy(t, ctx, repo, source.identity.ThreadID)
	if !source.session.Live() || owner.Accept(request.ID) || !bytes.Equal(before, readPickerJournal(t, targetPath)) {
		t.Fatal("cancelled handoff replaced its source or attempted a target Hook")
	}

	previous := request
	submitPickerRequest(t, owner, tui.PickerRequest{ID: 3, Operation: tui.PickerOpen, Source: source.identity.ThreadID})
	awaitPickerStage(t, ctx, owner, tui.PickerCompleted)
	submitPickerRequest(t, owner, tui.PickerRequest{ID: 4, Operation: tui.PickerPreview, Source: source.identity.ThreadID, ThreadID: target})
	preview := awaitPickerStage(t, ctx, owner, tui.PickerCompleted)
	if preview.Error != "" || !bytes.Equal(before, readPickerJournal(t, targetPath)) || requests.Load() != 0 {
		t.Fatal("preview did not remain read-only", preview.Error)
	}
	request.ID, request.Operation = 5, tui.PickerResume
	submitPickerRequest(t, owner, request)
	awaitPickerStage(t, ctx, owner, tui.PickerPrepared)
	owner.CancelResume(previous)
	if !owner.Accept(request.ID) || owner.Accept(request.ID) {
		t.Fatal("candidate must survive a stale cancellation and be accepted once")
	}
	awaitPickerStage(t, ctx, owner, tui.PickerActivated)
	current := <-adopted
	if current == nil || !bytes.Equal(before, readPickerJournal(t, targetPath)) || requests.Load() != 0 {
		t.Fatal("adoption ran the pending hook before accepting a real turn")
	}
	lease, err := repo.Open(ctx, source.identity.ThreadID)
	if err != nil {
		t.Fatalf("source lease remains after activation: %v", err)
	}
	if err = lease.Close(); err != nil {
		t.Fatal(err)
	}
	requirePickerLeaseBusy(t, ctx, repo, target)

	if plans := pickerSessionStartTurn(t, ctx, current); plans != 1 || requests.Load() != 1 {
		t.Fatal("first adopted turn did not match exactly one resume plan", plans)
	}
	waitCompactionIdle(t, current)
	adoptedPrefix := readPickerJournal(t, targetPath)
	submitPickerRequest(t, owner, tui.PickerRequest{ID: 6, Operation: tui.PickerOpen, Source: target, ThreadID: target})
	same := awaitPickerStage(t, ctx, owner, tui.PickerCancelled)
	if same.Error != "This session is already active" || !bytes.Equal(adoptedPrefix, readPickerJournal(t, targetPath)) {
		t.Fatal("same-thread selection changed the adopted session", same.Error)
	}
	if plans := pickerSessionStartTurn(t, ctx, current); plans != 0 || requests.Load() != 2 {
		t.Fatal("same-thread selection rearmed the consumed resume source", plans)
	}
	waitCompactionIdle(t, current)
	submitPickerRequest(t, owner, tui.PickerRequest{ID: 7, Operation: tui.PickerOpen, Source: target})
	listed := awaitPickerStage(t, ctx, owner, tui.PickerCompleted)
	if len(listed.Entries) != 1 || listed.Entries[0].ThreadID != source.identity.ThreadID {
		t.Fatalf("picker did not re-discover the released source: %#v", listed)
	}
}

func pickerSessionStartTurn(t *testing.T, ctx context.Context, resources *chatResources) int {
	t.Helper()
	if result, err := appendInteractiveText(ctx, resources.session, "continue the adopted session"); err != nil || result.Status() != protocol.CommandStatusAccepted {
		t.Fatal("picker left the adopted session unable to accept input", err)
	}
	plans := 0
	for {
		select {
		case item, open := <-resources.session.Outputs():
			if !open {
				t.Fatal("adopted session closed before the turn")
			}
			event, ok := item.TurnEvent()
			if !ok {
				continue
			}
			switch event.Kind {
			case protocol.EventSessionStart:
				payload, err := protocol.DecodeSessionStart(event)
				if err != nil || payload.Target.Source != domain.SessionStartResume || event.ThreadID != resources.identity.ThreadID {
					t.Fatal("session start event lost the adopted resume identity", err)
				}
				if payload.State == protocol.SessionStartPlanned {
					plans++
				}
			case protocol.EventHookTrustRequested:
				request, err := protocol.DecodeHookTrustRequested(event)
				if err != nil {
					t.Fatal(err)
				}
				if err := resources.session.ResolveHookTrust(ctx, event.TurnID, request, protocol.HookTrustDeny); err != nil {
					t.Fatal(err)
				}
			case protocol.EventTurnCompleted:
				return plans
			case protocol.EventTurnFailed:
				t.Fatalf("adopted turn failed: %s", event.Payload)
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
}

func TestInChatResumeFreezesSourcesAndRejectsReplacementRoots(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	paths, cwd, cfg := pickerFixture(t)
	instructions := filepath.Join(cwd, "AGENTS.md")
	if err := os.WriteFile(instructions, []byte("original instructions"), 0600); err != nil {
		t.Fatal(err)
	}
	target := newPickerFixtureThread(t, ctx, paths, cwd)
	environment := &chatEnvironment{}
	loads := 0
	source, err := openChatResourcesWithHooks(ctx, cfg, paths, "", false, cwd, chatResourceHooks{
		environment: environment,
		loadHooks: func(ctx context.Context, cwd, home string) (domain.HookSnapshot, error) {
			loads++
			return hook.NewLoader().Load(ctx, cwd, home)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer environment.close()
	defer source.close(context.Background())
	oldInstructions, oldExtensions := environment.instructions, environment.extensions
	if err = os.WriteFile(instructions, []byte("changed on disk"), 0600); err != nil {
		t.Fatal(err)
	}
	request := tui.PickerRequest{ID: 1, Operation: tui.PickerOpen, Source: source.identity.ThreadID, ThreadID: target}
	candidate := preparePickerCandidate(ctx, ctx, request, cfg, paths, cwd, nil, environment)
	if candidate.result.Error != "" || candidate.resources == nil {
		t.Fatalf("target did not recover with frozen sources: %q", candidate.result.Error)
	}
	if err = candidate.resources.close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if environment.instructions != oldInstructions || environment.extensions != oldExtensions || loads != 1 {
		t.Fatal("activation reloaded frozen sources")
	}
	for _, dir := range []string{paths.sessionRoot, cwd} {
		if err = os.Rename(dir, dir+"-original"); err != nil {
			t.Fatal(err)
		}
		if err = os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
		candidate = preparePickerCandidate(ctx, ctx, request, cfg, paths, cwd, nil, environment)
		entries, listErr := os.ReadDir(dir)
		if candidate.resources != nil || candidate.result.Error == "" || listErr != nil || len(entries) != 0 {
			t.Fatalf("recovery used the substituted root: %q, %v, %v", candidate.result.Error, entries, listErr)
		}
		if err = os.Remove(dir); err != nil {
			t.Fatal(err)
		}
		if err = os.Rename(dir+"-original", dir); err != nil {
			t.Fatal(err)
		}
	}
}

func TestTerminalOutputFailureDuringHandoffWaitsForOwnedLeases(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	paths, cwd, cfg := pickerFixture(t)
	target := newPickerFixtureThread(t, ctx, paths, cwd)
	environment := &chatEnvironment{}
	source, err := openChatResourcesWithHooks(ctx, cfg, paths, "", false, cwd, chatResourceHooks{interactiveApprovals: true, environment: environment})
	if err != nil {
		t.Fatal(err)
	}
	defer environment.close()
	chat, err := assembleChatModel(ctx, source)
	if err != nil {
		source.close(context.Background())
		t.Fatal(err)
	}
	blocked := &blockedPickerClose{managedJournal: source.writer, release: make(chan struct{})}
	source.writer = blocked
	var unblock sync.Once
	release := func() { unblock.Do(func() { close(blocked.release) }) }
	defer release()
	owner := newPickerOwner(newPickerExecutor(cfg, paths, cwd, nil, environment))
	owner.initial = source
	shell := tui.NewChatShell(owner, tui.PickerConfig{Family: domain.ProviderOpenAI, Wire: "responses", Model: "gpt-test", CWD: cwd}, chat)
	input, user := io.Pipe()
	defer input.Close()
	defer user.Close()
	output := &failingHandoffOutput{failed: make(chan struct{})}
	done := make(chan Outcome, 1)
	terminalCtx, stopTerminal := context.WithCancel(ctx)
	defer stopTerminal()
	go func() {
		done <- runPickerTerminal(terminalCtx, stopTerminal, Options{Input: input, Output: output}, owner, shell)
	}()
	defer func() {
		cancel()
		release()
		<-owner.done
	}()
	go func() { _, _ = io.WriteString(user, "/resume "+string(target)+"\r") }()
	select {
	case <-output.failed:
	case <-ctx.Done():
		t.Fatal("host did not render the accepted handoff")
	}
	select {
	case <-done:
		t.Fatal("host exited before final writer close")
	default:
	}
	repo := environment.roots.repository
	requirePickerLeaseBusy(t, ctx, repo, source.identity.ThreadID)
	requirePickerLeaseBusy(t, ctx, repo, target)
	release()
	select {
	case outcome := <-done:
		if outcome.Class != ExitRuntimeFailure || outcome.Failure.Code != fault.CodeOutput {
			t.Fatalf("output failure was lost: %#v", outcome)
		}
	case <-ctx.Done():
		t.Fatal("host did not finish cleanup", ctx.Err())
	}
	for _, id := range []domain.ThreadID{source.identity.ThreadID, target} {
		lease, err := repo.Open(ctx, id)
		if err != nil {
			t.Fatalf("host retained lease %s: %v", id, err)
		}
		if err = lease.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

type blockedPickerClose struct {
	managedJournal
	release chan struct{}
}

func (j *blockedPickerClose) Close(ctx context.Context) error {
	select {
	case <-j.release:
		return j.managedJournal.Close(ctx)
	case <-ctx.Done():
		return ctx.Err()
	}
}

type failingHandoffOutput struct {
	failed chan struct{}
	once   sync.Once
}

func (w *failingHandoffOutput) Write(p []byte) (int, error) {
	if bytes.Contains(p, []byte("Switching sessions")) {
		w.once.Do(func() { close(w.failed) })
		return 0, io.ErrClosedPipe
	}
	return len(p), nil
}

func pickerFixture(t *testing.T) (dataPaths, string, config.Config) {
	t.Helper()
	base := privateAppTempDir(t)
	return dataPaths{sessionRoot: filepath.Join(base, "sessions"), catalogPath: filepath.Join(base, "catalog.json")}, privateAppTempDir(t),
		config.Config{Provider: config.Provider{Family: domain.ProviderOpenAI, BaseURL: "https://example.invalid", APIKey: secret.New("picker-fixture"), Model: "gpt-test"}}
}

func newPickerFixtureThread(t *testing.T, ctx context.Context, paths dataPaths, cwd string) domain.ThreadID {
	t.Helper()
	service, err := openSessionService(paths.sessionRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer service.close()
	target, err := service.create(ctx, testSessionProvider(&fakeProviderFactory{family: domain.ProviderOpenAI}, "responses"), "gpt-test", cwd)
	if err != nil {
		t.Fatal(err)
	}
	if err = target.writer.Close(ctx); err != nil {
		t.Fatal(err)
	}
	return target.identity.ThreadID
}

func submitPickerRequest(t *testing.T, owner *pickerOwner, request tui.PickerRequest) {
	t.Helper()
	if !owner.Submit(request) {
		t.Fatalf("picker refused request %#v", request)
	}
}

func awaitPickerStage(t *testing.T, ctx context.Context, owner *pickerOwner, stage tui.PickerStage) tui.PickerResult {
	t.Helper()
	select {
	case result, ok := <-owner.Results():
		if !ok || result.Fatal || result.Stage != stage {
			t.Fatalf("picker result = %#v, open=%t, want stage %d", result, ok, stage)
		}
		return result
	case <-ctx.Done():
		t.Fatal(ctx.Err())
		return tui.PickerResult{}
	}
}

func requirePickerLeaseBusy(t *testing.T, ctx context.Context, repository *session.Repository, thread domain.ThreadID) {
	t.Helper()
	lease, err := repository.Open(ctx, thread)
	if lease != nil {
		_ = lease.Close()
	}
	if !session.IsJournalBusy(err) {
		t.Fatalf("expected active lease for %s, got %v", thread, err)
	}
}

func readPickerJournal(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
