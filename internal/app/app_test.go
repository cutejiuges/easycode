package app

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	contextplan "easycode/internal/context"
	"easycode/internal/context/estimate"
	"easycode/internal/domain"
	"easycode/internal/fault"
	"easycode/internal/headless"
	"easycode/internal/protocol"
	"easycode/internal/provider"
	chatRuntime "easycode/internal/runtime"
	"easycode/internal/session"
	"easycode/internal/tool"
)

func clearProviderEnvironment(t *testing.T) {
	t.Helper()
	for _, name := range []string{"EASYCODE_PROVIDER", "EASYCODE_BASE_URL", "EASYCODE_API_KEY", "EASYCODE_MODEL"} {
		t.Setenv(name, "")
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
		ContextProfile: mustAppContextProfile(t), ProjectInstructions: mustEmptyAppProjectInstructions(t),
		ToolCatalog: mustAppToolCatalog(t), QuestionExecutor: catalogQuestionFixture{}, SkillExecutor: catalogSkillExecutor{}, ReadArtifactExecutor: catalogArtifactExecutor{}, ReadExecutor: appReadExecutor{},
		GlobExecutor: appGlobExecutor{}, GrepExecutor: appGrepExecutor{},
		PatchPreparer: catalogPatchPreparer{}, PatchExecutor: catalogPatchExecutor{}, WritePreparer: catalogWritePreparer{}, WriteExecutor: catalogWriteExecutor{}, ExecPreparer: catalogExecFixture{}, ExecExecutor: catalogExecFixture{}, ToolPolicy: tool.NewApprovalPolicy(),
		ContextBudget:  contextplan.DisabledBudget(),
		ContextPlanner: contextplan.NewPlanner(),
	})
	if err != nil {
		t.Fatal(err)
	}
	chat := chatRuntime.NewChatSession(runtimeInstance)
	if err := chat.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = chat.Shutdown(context.Background()) })
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

func TestStreamEscalationClosesProviderBeforeLoopAndJournal(t *testing.T) {
	var mu sync.Mutex
	var timeline []string
	add := func(value string) {
		mu.Lock()
		timeline = append(timeline, value)
		mu.Unlock()
	}
	release := make(chan struct{})
	loop := newEscalationStreamLoop(release, add)
	inputReader, inputWriter := io.Pipe()
	defer inputWriter.Close()
	output := &appWriteCloser{}
	runner, err := headless.NewStreamRunner(loop, headless.StreamRunConfig{
		SessionID: runtimeSessionIDForApp, ThreadID: runtimeThreadIDForApp,
		Input: inputReader, Output: output, OutputCloser: output,
	})
	if err != nil {
		t.Fatal(err)
	}
	providerResource := &orderedProviderResource{
		factory: fakeProviderFactory{family: domain.ProviderOpenAI},
		force: func() {
			add("provider:close")
			close(release)
		},
	}
	journal := &orderedManagedJournal{add: add}
	resources := &chatResources{
		provider: providerResource, writer: journal,
		session: chatRuntime.NewChatSession(mustIdleAppRuntime(t, journal)),
	}
	ctx, cancel := context.WithCancel(context.Background())
	resultDone := make(chan struct {
		result headless.Result
		err    error
	}, 1)
	go func() {
		result, runErr := runStreamWithEscalation(ctx, runner, resources, 0)
		resultDone <- struct {
			result headless.Result
			err    error
		}{result: result, err: runErr}
	}()
	<-loop.ready
	cancel()
	response := <-resultDone
	if !errors.Is(response.err, context.DeadlineExceeded) || response.result.Failure.Code == "" {
		t.Fatalf("stream escalation result=%#v error=%v", response.result, response.err)
	}
	if err := resources.close(context.Background()); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	got := strings.Join(timeline, ",")
	mu.Unlock()
	if got != "provider:close,loop:done,journal:close" {
		t.Fatalf("stream escalation timeline = %q", got)
	}
	if providerResource.closeCalls.Load() != 1 || journal.closeCalls.Load() != 1 {
		t.Fatalf("close calls provider=%d journal=%d", providerResource.closeCalls.Load(), journal.closeCalls.Load())
	}
}

type escalationStreamLoop struct {
	ready   chan struct{}
	done    chan struct{}
	outputs chan protocol.ControlItem
	release <-chan struct{}
	add     func(string)
}

func newEscalationStreamLoop(release <-chan struct{}, add func(string)) *escalationStreamLoop {
	return &escalationStreamLoop{
		ready: make(chan struct{}), done: make(chan struct{}), outputs: make(chan protocol.ControlItem),
		release: release, add: add,
	}
}

func (loop *escalationStreamLoop) Run(ctx context.Context) error {
	close(loop.ready)
	<-loop.release
	if loop.add != nil {
		loop.add("loop:done")
	}
	close(loop.outputs)
	close(loop.done)
	// 强制关闭已取消的循环必须返回取消结果，不能依赖 transport 通知的调度次序。
	return ctx.Err()
}

func (loop *escalationStreamLoop) Ready() <-chan struct{} { return loop.ready }
func (loop *escalationStreamLoop) Done() <-chan struct{}  { return loop.done }
func (loop *escalationStreamLoop) Outputs() <-chan protocol.ControlItem {
	return loop.outputs
}
func (*escalationStreamLoop) Submit(context.Context, protocol.Command) (protocol.CommandResult, error) {
	return protocol.CommandResult{}, fault.New(fault.CodeTurnFailed, "stream loop is closing")
}
func (*escalationStreamLoop) CloseInput(context.Context) error { return nil }
func (loop *escalationStreamLoop) Stop(context.Context) error {
	<-loop.done
	return nil
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

func (*orderedConversation) HistoryFootprint() (domain.NativeHistoryFootprint, error) {
	estimated, _ := domain.NewEstimatedTokenEstimate(estimate.MethodByteHeuristic, 0)
	return domain.NewNativeHistoryFootprint(domain.ProviderOpenAI, 0, estimated)
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
		event, err := provider.NewCancelledStreamEvent(context.Canceled)
		if err == nil {
			stream <- event
		}
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

func (resource *orderedProviderResource) NewConversation(threadID domain.ThreadID) (provider.Conversation, error) {
	return resource.factory.NewConversation(threadID)
}

func (resource *orderedProviderResource) RestoreConversation(threadID domain.ThreadID, commits []provider.RestoreRecord) (provider.Conversation, error) {
	return resource.factory.RestoreConversation(threadID, commits)
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
		ContextProfile: mustAppContextProfile(t), ProjectInstructions: mustEmptyAppProjectInstructions(t),
		ToolCatalog: mustAppToolCatalog(t), QuestionExecutor: catalogQuestionFixture{}, SkillExecutor: catalogSkillExecutor{}, ReadArtifactExecutor: catalogArtifactExecutor{}, ReadExecutor: appReadExecutor{},
		GlobExecutor: appGlobExecutor{}, GrepExecutor: appGrepExecutor{},
		PatchPreparer: catalogPatchPreparer{}, PatchExecutor: catalogPatchExecutor{}, WritePreparer: catalogWritePreparer{}, WriteExecutor: catalogWriteExecutor{}, ExecPreparer: catalogExecFixture{}, ExecExecutor: catalogExecFixture{}, ToolPolicy: tool.NewApprovalPolicy(),
		ContextBudget:  contextplan.DisabledBudget(),
		ContextPlanner: contextplan.NewPlanner(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return runtimeInstance
}

func mustAppContextProfile(t *testing.T) contextplan.ProviderProfile {
	t.Helper()
	profile, err := contextplan.NewProviderProfile(domain.ProviderOpenAI, "gpt-test")
	if err != nil {
		t.Fatal(err)
	}
	return profile
}

type appReadExecutor struct{}

func (appReadExecutor) Execute(context.Context, tool.ReadInvocation) tool.ReadExecution {
	return tool.ReadExecution{}
}

type appGlobExecutor struct{}

func (appGlobExecutor) Execute(context.Context, tool.GlobInvocation, tool.TextOutputCapture) tool.SearchExecution {
	return tool.SearchExecution{}
}

type appGrepExecutor struct{}

func (appGrepExecutor) Execute(context.Context, tool.GrepInvocation, tool.TextOutputCapture) tool.SearchExecution {
	return tool.SearchExecution{}
}

func mustAppToolCatalog(t *testing.T) tool.CatalogSnapshot {
	t.Helper()
	catalog, err := tool.NewCatalogSnapshot(appReadExecutor{}, appGlobExecutor{}, appGrepExecutor{}, catalogPatchPreparer{}, catalogPatchExecutor{}, catalogWritePreparer{}, catalogWriteExecutor{}, catalogExecFixture{}, catalogExecFixture{}, tool.RenderExecOutput, tool.NewApprovalPolicy(), catalogArtifactExecutor{}, catalogSkillExecutor{}, catalogQuestionFixture{})
	if err != nil {
		t.Fatal(err)
	}
	return catalog
}

func mustEmptyAppProjectInstructions(t *testing.T) domain.ProjectInstructionsSnapshot {
	t.Helper()
	snapshot, err := domain.NewEmptyProjectInstructionsSnapshot(32 << 10)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
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

// catalogPatchPreparer 只用于验证目录装配，测试不会通过它访问文件系统。
type catalogPatchPreparer struct{}

func (catalogPatchPreparer) Prepare(context.Context, tool.PatchInvocation) (tool.PatchCandidate, error) {
	return tool.PatchCandidate{}, nil
}

type catalogPatchExecutor struct{}

func (catalogPatchExecutor) Execute(context.Context, tool.PatchCandidate) tool.InvocationResult {
	return tool.InvocationResult{}
}

// catalogWritePreparer 只用于验证目录装配，测试不会通过它访问文件系统。
type catalogWritePreparer struct{}

func (catalogWritePreparer) Prepare(context.Context, tool.WriteInvocation, tool.ReadObservation) (tool.WriteCandidate, error) {
	return tool.WriteCandidate{}, nil
}

type catalogWriteExecutor struct{}

func (catalogWriteExecutor) Execute(context.Context, tool.WriteCandidate) tool.InvocationResult {
	return tool.InvocationResult{}
}

// ProjectAssistantSample 在不模拟原生采样的测试替身中明确失败，不猜测展示列表。
func (*orderedConversation) ProjectAssistantSample(domain.AssistantSampleReference) (domain.AssistantSampleText, error) {
	return domain.AssistantSampleText{}, domain.HookConfigInvalid
}
