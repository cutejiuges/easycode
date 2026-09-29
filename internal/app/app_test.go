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
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"easycode/internal/config"
	"easycode/internal/domain"
	"easycode/internal/fault"
	"easycode/internal/headless"
	"easycode/internal/protocol"
	"easycode/internal/provider"
	chatRuntime "easycode/internal/runtime"
	"easycode/internal/secret"
	"easycode/internal/session"
)

func TestRunPrintModeUsesExistingChatResources(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		writeOpenAIAppTurn(writer, 1)
	}))
	defer server.Close()
	var output bytes.Buffer
	outcome := Run(context.Background(), Options{
		Mode: headless.ModeText, Prompt: "hello",
		ConfigPath:      writeAppConfig(t, t.TempDir(), domain.ProviderOpenAI, server.URL),
		SessionDataRoot: filepath.Join(t.TempDir(), "sessions"), Output: &output,
	})
	if outcome.ExitCode() != 0 {
		t.Fatalf("print mode outcome: %#v", outcome)
	}
	if output.String() != "answer-1\n" {
		t.Fatalf("print output = %q", output.String())
	}
}

func TestRunVersionDoesNotReadConfiguration(t *testing.T) {
	var output bytes.Buffer
	outcome := Run(context.Background(), Options{
		ShowVersion: true,
		ConfigPath:  filepath.Join(t.TempDir(), "missing.json"),
		Output:      &output,
	})
	if outcome.ExitCode() != 0 {
		t.Fatalf("run version: %#v", outcome)
	}
	if !strings.Contains(output.String(), Version) {
		t.Fatalf("version output: %q", output.String())
	}
}

func TestRunRejectsInvalidResolvedPromptBeforeConfigurationOrSession(t *testing.T) {
	dataRoot := filepath.Join(t.TempDir(), "sessions")
	outcome := Run(context.Background(), Options{
		Mode: headless.ModeText, Prompt: " \n\t",
		ConfigPath:      filepath.Join(t.TempDir(), "missing.json"),
		SessionDataRoot: dataRoot, Output: io.Discard,
	})
	if outcome.ExitCode() != 2 || outcome.Failure.Code != fault.CodeInvalidInput {
		t.Fatalf("invalid prompt outcome = %#v", outcome)
	}
	if _, err := os.Stat(dataRoot); !os.IsNotExist(err) {
		t.Fatalf("invalid prompt created session root: %v", err)
	}
}

func TestRunRejectsMissingExplicitConfiguration(t *testing.T) {
	var output bytes.Buffer
	missingPath := filepath.Join(t.TempDir(), "private-path-secret", "missing.json")
	outcome := Run(context.Background(), Options{
		ConfigPath: missingPath,
		Output:     &output,
	})
	if outcome.Failure.Code != fault.CodeInvalidConfiguration {
		t.Fatalf("missing configuration outcome: %#v", outcome)
	}
	if strings.Contains(outcome.Failure.Message, missingPath) || strings.Contains(outcome.Failure.Message, "private-path-secret") {
		t.Fatalf("configuration error leaked path: %#v", outcome)
	}
}

func TestRunRejectsInvalidConfigurationWithoutLeakingSecret(t *testing.T) {
	clearProviderEnvironment(t)
	path := filepath.Join(t.TempDir(), "config.json")
	content := `{"provider":"openai","base_url":"https://example.com/v1","api_key":"top-secret"}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write configuration: %v", err)
	}
	var output bytes.Buffer
	outcome := Run(context.Background(), Options{ConfigPath: path, Output: &output})
	if outcome.Failure.Code != fault.CodeInvalidConfiguration {
		t.Fatalf("invalid configuration outcome: %#v", outcome)
	}
	if strings.Contains(outcome.Failure.Message, "top-secret") || strings.Contains(output.String(), "top-secret") {
		t.Fatalf("configuration error leaked API key: %#v", outcome)
	}
}

func TestNewChatResourcesRejectsMissingConfiguration(t *testing.T) {
	_, err := newTestChatResources(t, config.Config{})
	if !errors.Is(err, &fault.Error{Code: fault.CodeInvalidConfiguration}) {
		t.Fatalf("missing configuration error: %v", err)
	}
}

func TestNewChatResourcesRejectsUnknownProviderWithoutNetwork(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requests++
	}))
	defer server.Close()

	resources, err := newTestChatResources(t, config.Config{Provider: config.Provider{
		Family:  domain.ProviderFamily("unknown"),
		BaseURL: server.URL,
		APIKey:  secret.New("top-secret"),
		Model:   "unknown-model",
	}})
	if resources != nil || !errors.Is(err, &fault.Error{Code: fault.CodeInvalidConfiguration}) {
		t.Fatalf("unknown provider resources=%#v error=%v", resources, err)
	}
	if requests != 0 {
		t.Fatalf("unknown provider made network requests: %d", requests)
	}
	if strings.Contains(err.Error(), "top-secret") {
		t.Fatalf("unknown provider error leaked API key: %v", err)
	}
}

func clearProviderEnvironment(t *testing.T) {
	t.Helper()
	for _, name := range []string{"EASYCODE_PROVIDER", "EASYCODE_BASE_URL", "EASYCODE_API_KEY", "EASYCODE_MODEL"} {
		t.Setenv(name, "")
	}
}

func TestNewChatResourcesBuildsAnthropicConversationWithoutNetwork(t *testing.T) {
	resources, err := newTestChatResources(t, config.Config{Provider: config.Provider{
		Family:  domain.ProviderAnthropic,
		BaseURL: "https://example.com/v1",
		APIKey:  secret.New("top-secret"),
		Model:   "claude-test",
	}})
	if err != nil {
		t.Fatalf("new Anthropic resources: %v", err)
	}
	shutdownContext, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := resources.close(shutdownContext); err != nil {
		t.Fatalf("close Anthropic resources: %v", err)
	}
}

func TestNewChatResourcesBuildsOpenAIConversationWithoutNetwork(t *testing.T) {
	for _, baseURL := range []string{"https://example.com", "https://example.com/openai/v1"} {
		t.Run(baseURL, func(t *testing.T) {
			resources, err := newTestChatResources(t, config.Config{Provider: config.Provider{
				Family:  domain.ProviderOpenAI,
				BaseURL: baseURL,
				APIKey:  secret.New("test-key"),
				Model:   "gpt-test",
			}})
			if err != nil {
				t.Fatalf("new resources: %v", err)
			}
			shutdownContext, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if err := resources.close(shutdownContext); err != nil {
				t.Fatalf("close resources: %v", err)
			}
		})
	}
}

func TestLoadedConfigurationStaysTypedAcrossAppBoundary(t *testing.T) {
	clearProviderEnvironment(t)
	path := filepath.Join(t.TempDir(), "config.json")
	content := `{"provider":"openai","base_url":"https://example.com/openai/v1","api_key":"boundary-secret","model":"gpt-test"}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write configuration: %v", err)
	}
	loaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("load configuration: %v", err)
	}
	resources, err := newTestChatResources(t, loaded)
	if err != nil {
		t.Fatalf("new chat resources: %v", err)
	}
	shutdownContext, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := resources.close(shutdownContext); err != nil {
		t.Fatalf("close resources: %v", err)
	}
	if strings.Contains(loaded.String(), "boundary-secret") || strings.Contains(loaded.String(), path) {
		t.Fatalf("typed configuration leaked sensitive input: %s", loaded)
	}
}

func TestRunStartsTUIFromSupportedConfigurationSourcesWithoutNetwork(t *testing.T) {
	tests := []struct {
		name      string
		family    domain.ProviderFamily
		baseURL   string
		configure func(*testing.T, string) Options
	}{
		{
			name:    "explicit OpenAI host-only file",
			family:  domain.ProviderOpenAI,
			baseURL: "https://example.com",
			configure: func(t *testing.T, baseURL string) Options {
				path := writeAppConfig(t, t.TempDir(), domain.ProviderOpenAI, baseURL)
				return Options{ConfigPath: path}
			},
		},
		{
			name:    "explicit Anthropic path-prefix file",
			family:  domain.ProviderAnthropic,
			baseURL: "https://example.com/anthropic/v1",
			configure: func(t *testing.T, baseURL string) Options {
				path := writeAppConfig(t, t.TempDir(), domain.ProviderAnthropic, baseURL)
				return Options{ConfigPath: path}
			},
		},
		{
			name:    "default Anthropic file",
			family:  domain.ProviderAnthropic,
			baseURL: "https://example.com/v1",
			configure: func(t *testing.T, baseURL string) Options {
				home := t.TempDir()
				t.Setenv("HOME", home)
				writeAppConfigAt(t, filepath.Join(home, ".config", "easycode", "config.json"), domain.ProviderAnthropic, baseURL)
				return Options{}
			},
		},
		{
			name:    "Anthropic environment only",
			family:  domain.ProviderAnthropic,
			baseURL: "https://example.com/v1",
			configure: func(t *testing.T, baseURL string) Options {
				t.Setenv("HOME", t.TempDir())
				t.Setenv("EASYCODE_PROVIDER", "anthropic")
				t.Setenv("EASYCODE_BASE_URL", baseURL)
				t.Setenv("EASYCODE_API_KEY", "environment-secret")
				t.Setenv("EASYCODE_MODEL", "claude-test")
				return Options{}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			clearProviderEnvironment(t)
			options := test.configure(t, test.baseURL)
			options.SessionDataRoot = filepath.Join(t.TempDir(), "sessions")
			var output bytes.Buffer
			options.Input = strings.NewReader("\x03")
			options.Output = &output
			if outcome := Run(context.Background(), options); outcome.ExitCode() != 0 {
				t.Fatalf("run TUI: %#v", outcome)
			}
			if !strings.Contains(output.String(), "Status: idle") {
				t.Fatalf("TUI did not enter idle state: %q", output.String())
			}
			if strings.Contains(output.String(), "file-secret") ||
				strings.Contains(output.String(), "environment-secret") ||
				strings.Contains(strings.ToLower(output.String()), "x-api-key") ||
				strings.Contains(strings.ToLower(output.String()), "anthropic-version") {
				t.Fatalf("TUI output leaked API key: %q", output.String())
			}
		})
	}
}

func TestRunCanRecoverAfterConfigurationError(t *testing.T) {
	clearProviderEnvironment(t)
	directory := t.TempDir()
	path := filepath.Join(directory, "config.json")
	if err := os.WriteFile(path, []byte(`{"provider":"openai"}`), 0o600); err != nil {
		t.Fatalf("write invalid configuration: %v", err)
	}
	var output bytes.Buffer
	if outcome := Run(context.Background(), Options{ConfigPath: path, Output: &output}); outcome.Failure.Code != fault.CodeInvalidConfiguration {
		t.Fatalf("invalid configuration outcome: %#v", outcome)
	}

	writeAppConfigAt(t, path, domain.ProviderOpenAI, "https://example.com/v1")
	output.Reset()
	if outcome := Run(context.Background(), Options{
		ConfigPath:      path,
		SessionDataRoot: filepath.Join(t.TempDir(), "sessions"),
		Input:           strings.NewReader("\x03"),
		Output:          &output,
	}); outcome.ExitCode() != 0 {
		t.Fatalf("run after configuration repair: %#v", outcome)
	}
}

func TestChatResourcesCloseOrdersSessionJournalAndProvider(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	var timeline []string
	add := func(value string) {
		mu.Lock()
		timeline = append(timeline, value)
		mu.Unlock()
	}
	journal := &orderedManagedJournal{add: add}
	conversation := &orderedConversation{add: add}
	runtimeInstance, err := chatRuntime.New(conversation, chatRuntime.Config{
		SessionID: runtimeSessionIDForApp, ThreadID: runtimeThreadIDForApp, Journal: journal,
		GenerateTurnID: func() (domain.TurnID, error) { return runtimeTurnIDForApp, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	chat := chatRuntime.NewChatSession(runtimeInstance)
	events, err := chat.Submit(context.Background(), "hello")
	if err != nil {
		t.Fatal(err)
	}
	if event := <-events; event.Kind != protocol.EventTurnStarted {
		t.Fatalf("first event = %s", event.Kind)
	}
	providerResource := &orderedProviderResource{factory: fakeProviderFactory{family: domain.ProviderOpenAI}, add: add}
	resources := &chatResources{provider: providerResource, writer: journal, session: chat}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := resources.close(ctx); err != nil {
		t.Fatal(err)
	}
	for range events {
	}
	mu.Lock()
	got := append([]string(nil), timeline...)
	mu.Unlock()
	want := []string{"append:turn_started", "provider:stream", "append:turn_failed", "journal:close", "provider:close"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("close timeline = %#v, want %#v", got, want)
	}
}

func TestChatResourcesWriterCloseFailureStillClosesProvider(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	var timeline []string
	add := func(value string) {
		mu.Lock()
		timeline = append(timeline, value)
		mu.Unlock()
	}
	journal := &orderedManagedJournal{add: add, closeErr: errors.New("fixture sync failure")}
	providerResource := &orderedProviderResource{factory: fakeProviderFactory{family: domain.ProviderOpenAI}, add: add}
	resources := &chatResources{
		provider: providerResource, writer: journal,
		session: chatRuntime.NewChatSession(mustIdleAppRuntime(t, journal)),
	}
	err := resources.close(context.Background())
	if err == nil || !strings.Contains(err.Error(), "fixture sync failure") {
		t.Fatalf("close error = %v", err)
	}
	mu.Lock()
	got := strings.Join(timeline, ",")
	mu.Unlock()
	if got != "journal:close,provider:close" {
		t.Fatalf("close timeline = %q", got)
	}
}

func TestChatResourcesShutdownTimeoutEscalatesAndClosesDependencies(t *testing.T) {
	t.Parallel()
	streamStarted := make(chan struct{})
	release := make(chan struct{})
	conversation := &orderedConversation{release: release, started: streamStarted}
	journal := &orderedManagedJournal{}
	runtimeInstance, err := chatRuntime.New(conversation, chatRuntime.Config{
		SessionID: runtimeSessionIDForApp, ThreadID: runtimeThreadIDForApp, Journal: journal,
		GenerateTurnID: func() (domain.TurnID, error) { return runtimeTurnIDForApp, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	chat := chatRuntime.NewChatSession(runtimeInstance)
	events, err := chat.Submit(context.Background(), "hello")
	if err != nil {
		t.Fatal(err)
	}
	<-events
	<-streamStarted
	var releaseOnce sync.Once
	providerResource := &orderedProviderResource{
		factory: fakeProviderFactory{family: domain.ProviderOpenAI},
		force:   func() { releaseOnce.Do(func() { close(release) }) },
	}
	resources := &chatResources{provider: providerResource, writer: journal, session: chat}
	ctx, cancel := context.WithCancel(context.Background())
	closeDone := make(chan error, 1)
	go func() { closeDone <- resources.close(ctx) }()
	cancel()
	if err := <-closeDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("close timeout error = %v", err)
	}
	if journal.closeCalls.Load() != 1 || providerResource.closeCalls.Load() != 1 {
		t.Fatalf("dependencies not closed after escalation: journal=%d provider=%d", journal.closeCalls.Load(), providerResource.closeCalls.Load())
	}
	for range events {
	}
}

const (
	runtimeSessionIDForApp = domain.SessionID("00000000-0020-7000-8000-000000000020")
	runtimeThreadIDForApp  = domain.ThreadID("00000000-0021-7000-8000-000000000021")
	runtimeTurnIDForApp    = domain.TurnID("00000000-0022-7000-8000-000000000022")
)

type orderedManagedJournal struct {
	add        func(string)
	closeErr   error
	closeCalls atomic.Int32
}

func (journal *orderedManagedJournal) AppendBatch(_ context.Context, drafts []session.RecordDraft) ([]session.Record, error) {
	if journal.add != nil {
		journal.add("append:" + string(drafts[0].EventKind()))
	}
	return make([]session.Record, len(drafts)), nil
}

func (*orderedManagedJournal) Poisoned() bool { return false }

func (journal *orderedManagedJournal) Close(context.Context) error {
	journal.closeCalls.Add(1)
	if journal.add != nil {
		journal.add("journal:close")
	}
	return journal.closeErr
}

type orderedConversation struct {
	add     func(string)
	release <-chan struct{}
	started chan<- struct{}
}

func (*orderedConversation) Family() domain.ProviderFamily { return domain.ProviderOpenAI }

func (*orderedConversation) Capabilities() provider.Capabilities {
	return provider.Capabilities{Streaming: true}
}

func (*orderedConversation) ProjectHistory() domain.SemanticHistoryView {
	return domain.SemanticHistoryView{Provider: domain.ProviderOpenAI, Turns: []domain.SemanticTurn{}}
}

func (conversation *orderedConversation) Stream(ctx context.Context, _ provider.TurnInput) (<-chan provider.StreamEvent, error) {
	if conversation.add != nil {
		conversation.add("provider:stream")
	}
	if conversation.started != nil {
		close(conversation.started)
	}
	stream := make(chan provider.StreamEvent, 1)
	go func() {
		<-ctx.Done()
		if conversation.release != nil {
			<-conversation.release
		}
		stream <- provider.StreamEvent{Kind: provider.StreamEventCancelled, Err: context.Canceled}
		close(stream)
	}()
	return stream, nil
}

type orderedProviderResource struct {
	factory    fakeProviderFactory
	add        func(string)
	force      func()
	closeCalls atomic.Int32
}

func (resource *orderedProviderResource) Family() domain.ProviderFamily {
	return resource.factory.Family()
}

func (resource *orderedProviderResource) Capabilities() provider.Capabilities {
	return resource.factory.Capabilities()
}

func (resource *orderedProviderResource) NewConversation() provider.Conversation {
	return resource.factory.NewConversation()
}

func (resource *orderedProviderResource) RestoreConversation(commits []provider.NativeCommitEnvelope) (provider.Conversation, error) {
	return resource.factory.RestoreConversation(commits)
}

func (resource *orderedProviderResource) Close() error {
	resource.closeCalls.Add(1)
	if resource.force != nil {
		resource.force()
	}
	if resource.add != nil {
		resource.add("provider:close")
	}
	return nil
}

func mustIdleAppRuntime(t *testing.T, journal chatRuntime.Journal) *chatRuntime.Runtime {
	t.Helper()
	runtimeInstance, err := chatRuntime.New(&orderedConversation{}, chatRuntime.Config{
		SessionID: runtimeSessionIDForApp, ThreadID: runtimeThreadIDForApp, Journal: journal,
		GenerateTurnID: func() (domain.TurnID, error) { return runtimeTurnIDForApp, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	return runtimeInstance
}

func newTestChatResources(t *testing.T, applicationConfig config.Config) (*chatResources, error) {
	t.Helper()
	return openChatResources(
		context.Background(), applicationConfig,
		filepath.Join(t.TempDir(), "sessions"), "", t.TempDir(),
	)
}

func writeAppConfig(t *testing.T, directory string, family domain.ProviderFamily, baseURL string) string {
	t.Helper()
	path := filepath.Join(directory, "config.json")
	writeAppConfigAt(t, path, family, baseURL)
	return path
}

func writeAppConfigAt(t *testing.T, path string, family domain.ProviderFamily, baseURL string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("create configuration directory: %v", err)
	}
	model := "gpt-test"
	if family == domain.ProviderAnthropic {
		model = "claude-test"
	}
	content := `{"provider":"` + string(family) + `","base_url":"` + baseURL + `","api_key":"file-secret","model":"` + model + `"}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write configuration: %v", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatalf("chmod configuration: %v", err)
	}
}
