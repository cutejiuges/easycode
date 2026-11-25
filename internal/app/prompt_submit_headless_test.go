package app

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"easycode/internal/codec"
	contextplan "easycode/internal/context"
	"easycode/internal/domain"
	"easycode/internal/fault"
	"easycode/internal/headless"
	"easycode/internal/protocol"
	chatRuntime "easycode/internal/runtime"
	"easycode/internal/session"
)

func TestPromptSubmitHookFailsBeforeAnyHeadlessRequest(t *testing.T) {
	clearProviderEnvironment(t)
	workspace := privateAppTempDir(t)
	t.Chdir(workspace)
	configDir := filepath.Join(workspace, ".easycode")
	if err := os.Mkdir(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	const script = `cat >/dev/null; exit 2`
	config := `{"hooks":{"UserPromptSubmit":[{"hooks":[{"id":"prompt-policy","type":"command","command":"` + script + `"}]}]}}`
	writeAppTestFile(t, filepath.Join(configDir, "hooks.json"), []byte(config), 0o600)
	var providerRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		providerRequests.Add(1)
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()
	configPath := writeAppConfig(t, t.TempDir(), domain.ProviderOpenAI, server.URL)
	const prompt = "private prompt for one-shot test"
	for _, fixture := range []struct {
		name string
		mode headless.Mode
	}{{"print", headless.ModeText}, {"json", headless.ModeJSON}} {
		t.Run(fixture.name, func(t *testing.T) {
			root := filepath.Join(privateAppTempDir(t), "sessions")
			var output bytes.Buffer
			outcome := Run(context.Background(), Options{Mode: fixture.mode, Prompt: prompt, ConfigPath: configPath, SessionDataRoot: root, Output: &output})
			if outcome.ExitCode() != 1 || outcome.Failure.Code != fault.Code(domain.HookTrustRequired) {
				t.Fatalf("outcome=%#v output=%q", outcome, output.String())
			}
			if fixture.mode == headless.ModeText {
				if output.Len() != 0 || outcome.Report != ReportPending {
					t.Fatalf("text outcome has unexpected stdout or report state: %q, %v", output.String(), outcome.Report)
				}
				return
			}
			lines := strings.Split(strings.TrimSpace(output.String()), "\n")
			var failed headless.TurnFailedEvent
			if len(lines) != 3 || codec.UnmarshalRecord([]byte(lines[2]), &failed) != nil || failed.Type != "turn.failed" || failed.Error.Code != fault.Code(domain.HookTrustRequired) || outcome.Report != ReportComplete {
				t.Fatalf("JSON mode did not report an existing terminal: %s", output.String())
			}
			started := decodeThreadStarted(t, output.String())
			assertHeadlessPromptJournalPrivate(t, loadHeadlessJournal(t, root, started.ThreadID), prompt, script)
		})
	}
	if providerRequests.Load() != 0 {
		t.Fatalf("blocked prompt made %d Provider requests", providerRequests.Load())
	}
}

// promptHeadlessJournal 在首个回合开始前托住 worker，使两个 append 确定共享下一次领取。
type promptHeadlessJournal struct {
	*session.JournalWriter
	release <-chan struct{}
	started bool
}

func (journal *promptHeadlessJournal) AppendBatch(ctx context.Context, drafts []session.RecordDraft) ([]session.Record, error) {
	if !journal.started && drafts[0].EventKind() == session.EventTurnStarted {
		journal.started = true
		select {
		case <-journal.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return journal.JournalWriter.AppendBatch(ctx, drafts)
}

type promptHeadlessLoop struct {
	*chatRuntime.AgentLoop
	release chan<- struct{}
}

func (loop promptHeadlessLoop) CloseInput(ctx context.Context) error {
	defer close(loop.release)
	return loop.AgentLoop.CloseInput(ctx)
}

type promptHeadlessRunner struct{ calls atomic.Int32 }

func (runner *promptHeadlessRunner) Preflight(context.Context) domain.HookCode {
	runner.calls.Add(1)
	return domain.HookOK
}

func (runner *promptHeadlessRunner) Run(context.Context, domain.HookRunIdentity, domain.HookDeclaration, domain.HookInput, time.Duration) domain.HookExecution {
	runner.calls.Add(1)
	return domain.HookExecution{}
}

func TestPromptSubmitHookStreamPreservesStrictAndMixedAutoIdentity(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	workspace := privateAppTempDir(t)
	root := filepath.Join(workspace, "sessions")
	repository, err := session.OpenOrCreateRepository(root)
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	identity := session.Identity{SessionID: runtimeSessionIDForApp, ThreadID: runtimeThreadIDForApp}
	writer, _, err := session.CreateRootJournal(ctx, repository, identity, session.RootJournalConfig{
		Provider: domain.ProviderOpenAI, ProviderWire: domain.ProviderOpenAI.Wire(), Model: "gpt-test", CreationCWD: workspace,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close(context.Background())
	release := make(chan struct{})
	journal := &promptHeadlessJournal{JournalWriter: writer, release: release}
	var providerRequests atomic.Int32
	conversation := &orderedConversation{add: func(string) { providerRequests.Add(1) }}
	runtime, err := chatRuntime.New(conversation, chatRuntime.Config{
		SessionID: identity.SessionID, ThreadID: identity.ThreadID, Journal: journal, ProviderWire: domain.ProviderOpenAI.Wire(),
		InitialTaskProgress: domain.NewNoTaskProgress(), ContextProfile: mustAppContextProfile(t), ToolBinding: mustAppToolBinding(t),
		ProjectInstructions: mustEmptyAppProjectInstructions(t), ContextBudget: contextplan.DisabledBudget(), ContextPlanner: contextplan.NewPlanner(),
	})
	if err != nil {
		t.Fatal(err)
	}
	declaration, err := domain.NewUserPromptSubmitHookDeclaration(domain.HookSourceProject, domain.HookFeedbackNone, domain.HookDigest(nil), "prompt-policy", "cat >/dev/null; exit 2", 5)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := domain.NewHookSnapshot([]domain.HookDeclaration{declaration}, nil)
	if err != nil {
		t.Fatal(err)
	}
	hookRunner := &promptHeadlessRunner{}
	if err := runtime.InstallHooks(snapshot, hookRunner, workspace, domain.HookOK); err != nil {
		t.Fatal(err)
	}
	loop, err := chatRuntime.NewAgentLoop(runtime, chatRuntime.DefaultAgentLoopConfig())
	if err != nil {
		t.Fatal(err)
	}
	input := io.NopCloser(strings.NewReader(`{"version":1,"type":"input.submit","request_id":"strict","text":"private strict prompt"}` + "\n" +
		`{"version":1,"type":"input.append","request_id":"auto-a","text":"private first append"}` + "\n" +
		`{"version":1,"type":"input.append","request_id":"auto-b","text":"private second append"}` + "\n" +
		`{"version":1,"type":"cache.query","request_id":"observe"}` + "\n"))
	output := &appWriteCloser{}
	runner, err := headless.NewStreamRunner(promptHeadlessLoop{AgentLoop: loop, release: release}, headless.StreamRunConfig{
		SessionID: identity.SessionID, ThreadID: identity.ThreadID, Input: input, Output: output, OutputCloser: output,
	})
	if err != nil {
		t.Fatal(err)
	}
	if outcome := runner.Run(ctx); !outcome.Completed || hookRunner.calls.Load() != 0 || providerRequests.Load() != 0 {
		t.Fatalf("headless EOF or execution boundary failed: outcome=%#v hooks=%d requests=%d output=%s", outcome, hookRunner.calls.Load(), providerRequests.Load(), output.String())
	}
	assertHeadlessPromptStream(t, output.String())
	if err := writer.Close(ctx); err != nil {
		t.Fatal(err)
	}
	assertHeadlessPromptJournalPrivate(t, loadHeadlessJournal(t, root, identity.ThreadID), "private strict prompt", "private first append", "private second append", declaration.Command)
}

func assertHeadlessPromptStream(t *testing.T, output string) {
	t.Helper()
	scanner := bufio.NewScanner(strings.NewReader(output))
	accepted := make(map[protocol.RequestID]domain.InputID)
	turns := make(map[protocol.RequestID]domain.TurnID)
	status := make(map[protocol.RequestID][]headless.StreamInputStatusEvent)
	var started, failed []protocol.RequestID
	for scanner.Scan() {
		line := scanner.Bytes()
		var frame struct {
			Type string `json:"type"`
		}
		if err := codec.Unmarshal(line, &frame); err != nil {
			t.Fatal(err)
		}
		switch frame.Type {
		case "thread.started", "cache.snapshot":
		case "control.response":
			var value headless.StreamControlResponseEvent
			if err := codec.UnmarshalRecord(line, &value); err != nil || value.Status != protocol.CommandStatusAccepted {
				t.Fatalf("bad command receipt %s: %v", line, err)
			}
			accepted[value.RequestID] = value.EntryID
		case "turn.started":
			var value headless.StreamTurnStartedEvent
			if err := codec.UnmarshalRecord(line, &value); err != nil || !value.TurnID.Valid() {
				t.Fatalf("invalid turn start %s: %v", line, err)
			}
			if _, ok := accepted[value.InputID]; !ok {
				t.Fatalf("turn started before acceptance: %s", line)
			}
			started = append(started, value.InputID)
			turns[value.InputID] = value.TurnID
		case "turn.failed":
			var value headless.StreamTurnFailedEvent
			if err := codec.UnmarshalRecord(line, &value); err != nil || value.TurnID != turns[value.InputID] || value.Error.Code != fault.Code(domain.HookTrustRequired) {
				t.Fatalf("invalid turn failure %s: %v", line, err)
			}
			failed = append(failed, value.InputID)
		case "input.status":
			var value headless.StreamInputStatusEvent
			if err := codec.UnmarshalRecord(line, &value); err != nil {
				t.Fatalf("input status leaked non-v1 fields: %s: %v", line, err)
			}
			status[value.RequestID] = append(status[value.RequestID], value)
		default:
			t.Fatalf("unexpected v1 frame: %s", line)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	expected := []protocol.RequestID{"strict", "auto-a"}
	_, observed := accepted["observe"]
	if !slices.Equal(started, expected) || !slices.Equal(failed, expected) || !observed || len(accepted) != 4 || len(status) != 2 || accepted["strict"] != "" || accepted["auto-a"] == accepted["auto-b"] || turns["strict"] == turns["auto-a"] {
		t.Fatalf("unexpected strict or shared auto turn attribution: %s", output)
	}
	for _, requestID := range []protocol.RequestID{"auto-a", "auto-b"} {
		frames := status[requestID]
		if len(frames) != 2 || frames[0].State != protocol.InputClaimed || frames[0].Reason != "" || frames[1].State != protocol.InputReturned || frames[1].Reason != protocol.InputReasonFailed {
			t.Fatalf("unexpected input states for %s: %#v", requestID, frames)
		}
		for _, frame := range frames {
			if !frame.EntryID.Valid() || frame.EntryID != accepted[requestID] || frame.TurnID != turns["auto-a"] {
				t.Fatalf("input status lost its independent receipt or shared turn: %#v", frame)
			}
		}
	}
}

func assertHeadlessPromptJournalPrivate(t *testing.T, loaded session.LoadResult, forbidden ...string) {
	t.Helper()
	if _, err := session.NewReplayPlanner().Plan(loaded); err != nil {
		t.Fatalf("headless rejection left invalid journal history: %v", err)
	}
	for _, record := range loaded.Records {
		switch record.EventKind {
		case session.EventHookExecutionStarted, session.EventHookExecutionFinished, session.EventTurnInputsCommitted, session.EventProviderNativeCommit:
			t.Fatalf("untrusted prompt left a process or native input record: %s", record.EventKind)
		}
		for _, value := range forbidden {
			if bytes.Contains(record.Payload, []byte(value)) {
				t.Fatalf("untrusted prompt journal saved private bytes in %s", record.EventKind)
			}
		}
	}
}
