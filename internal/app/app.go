// Package app 负责装配应用依赖和管理顶层生命周期。
package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"easycode/internal/config"
	contextplan "easycode/internal/context"
	"easycode/internal/context/projectinstructions"
	"easycode/internal/domain"
	"easycode/internal/fault"
	"easycode/internal/headless"
	"easycode/internal/provider"
	"easycode/internal/provider/anthropic"
	"easycode/internal/provider/openai"
	chatRuntime "easycode/internal/runtime"
	"easycode/internal/session"
	"easycode/internal/session/artifact"
	"easycode/internal/session/catalog"
	"easycode/internal/tool"
	"easycode/internal/tool/builtin"
	"easycode/internal/tui"
)

const Version = "0.0.0-dev"

const shutdownTimeout = 5 * time.Second

var _ headless.ChatSession = (*chatRuntime.ChatSession)(nil)
var _ headless.StreamControlLoop = (*chatRuntime.AgentLoop)(nil)

// Options 描述应用入口参数，避免入口层直接依赖具体实现细节。
type Options struct {
	ShowVersion        bool
	Mode               headless.Mode
	Prompt             string
	ConfigPath         string
	ResumeThreadID     string
	ContinueSession    bool
	SessionDataRoot    string
	SessionCatalogPath string
	Input              io.Reader
	Output             io.Writer
}

type dataPaths struct {
	sessionRoot string
	catalogPath string
}

type chatResourceHooks struct {
	afterContinueSelection func(domain.ThreadID)
	interactiveApprovals   bool
	newExecExecutor        func(*builtin.Workspace, builtin.CommandEnvironment, tool.ExecOutputRenderer) (tool.ExecExecutor, error)
	wrapOutputStore        func(tool.CommandOutputStore) tool.CommandOutputStore
}

type providerResource interface {
	provider.Factory
	Close() error
}

type chatResources struct {
	identity          session.Identity
	provider          providerResource
	workspace         *builtin.Workspace
	service           *sessionService
	artifactLayout    *artifact.Layout
	artifactStore     *artifact.Store
	transcriptQueries *transcriptQueries
	writer            managedJournal
	runtime           *chatRuntime.Runtime
	session           *chatRuntime.ChatSession
	history           domain.SemanticHistoryView
	repair            session.RepairReport
	resumed           bool
	providerClosed    bool
	closeMu           sync.Mutex
	closeDone         chan struct{}
	closeErr          error
}

// resourceCleanup 在应用装配成功前按取得顺序持有资源，并按严格逆序释放。
type resourceCleanup struct {
	actions []func() error
}

func (cleanup *resourceCleanup) add(action func() error) {
	cleanup.actions = append(cleanup.actions, action)
}

func (cleanup *resourceCleanup) close() error {
	var result error
	for index := len(cleanup.actions) - 1; index >= 0; index-- {
		result = errors.Join(result, cleanup.actions[index]())
	}
	cleanup.actions = nil
	return result
}

func (cleanup *resourceCleanup) release() {
	cleanup.actions = nil
}

// Run 根据入口参数运行交互或 headless 宿主并返回稳定结果。
func Run(ctx context.Context, options Options) Outcome {
	if options.Output == nil {
		return runtimeFailure(fault.New(fault.CodeTurnFailed, "output is required"))
	}
	if options.ShowVersion {
		_, err := fmt.Fprintf(options.Output, "easycode %s\n", Version)
		if err != nil {
			return runtimeFailure(fault.New(fault.CodeTurnFailed, "write version output failed"))
		}
		return Outcome{Class: ExitSuccess}
	}
	if options.ContinueSession && options.ResumeThreadID != "" {
		return usageFailure("--resume and --continue are mutually exclusive")
	}
	if options.Mode != headless.ModeInteractive && options.Mode != headless.ModeText &&
		options.Mode != headless.ModeJSON && options.Mode != headless.ModeStreamJSON {
		return runtimeFailure(fault.New(fault.CodeTurnFailed, "application mode is invalid"))
	}
	if options.Mode == headless.ModeText || options.Mode == headless.ModeJSON {
		if err := headless.ValidateResolvedPrompt(options.Prompt); err != nil {
			return usageFailure(err.Error())
		}
	}
	if options.Mode == headless.ModeStreamJSON {
		if options.Prompt != "" {
			return usageFailure("stream-json mode does not accept a prompt")
		}
		if _, ok := options.Input.(io.ReadCloser); !ok {
			return usageFailure("stream-json input must be closable")
		}
		if _, ok := options.Output.(io.Closer); !ok {
			return usageFailure("stream-json output must be closable")
		}
	}
	applicationConfig, err := config.Load(options.ConfigPath)
	if err != nil {
		return runtimeFailure(err)
	}
	paths, err := resolveDataPaths(options.SessionDataRoot, options.SessionCatalogPath)
	if err != nil {
		return runtimeFailure(err)
	}
	creationCWD, err := os.Getwd()
	if err != nil {
		return runtimeFailure(fault.New(fault.CodeSessionWrite, "current working directory is unavailable"))
	}
	resources, err := openChatResourcesWithHooks(
		ctx, applicationConfig, paths, options.ResumeThreadID, options.ContinueSession, creationCWD,
		chatResourceHooks{interactiveApprovals: options.Mode == headless.ModeInteractive},
	)
	if err != nil {
		return runtimeFailure(err)
	}

	var outcome Outcome
	if options.Mode == headless.ModeInteractive {
		outcome = runTUI(ctx, options, resources)
	} else if options.Mode == headless.ModeStreamJSON {
		outcome = runStreamJSON(ctx, options, resources)
	} else {
		if err := resources.session.Start(ctx); err != nil {
			_ = resources.close(context.Background())
			return runtimeFailure(err)
		}
		result := headless.Run(ctx, resources.session, headless.RunConfig{
			Mode: options.Mode, Prompt: options.Prompt,
			SessionID: resources.identity.SessionID, ThreadID: resources.identity.ThreadID,
			Resumed: resources.resumed, Output: options.Output,
		})
		outcome = outcomeFromHeadless(result)
	}
	shutdownContext, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	closeErr := resources.close(shutdownContext)
	if closeErr != nil {
		if outcome.Class != ExitSuccess && outcome.Report == ReportOutputUnavailable {
			return outcome
		}
		return runtimeFailure(closeErr)
	}
	return outcome
}

func runStreamJSON(ctx context.Context, options Options, resources *chatResources) Outcome {
	input, inputOK := options.Input.(io.ReadCloser)
	outputCloser, outputOK := options.Output.(io.Closer)
	if !inputOK || !outputOK || resources == nil || resources.runtime == nil {
		return runtimeFailure(fault.New(fault.CodeTurnFailed, "stream transport is not configured"))
	}
	loop, err := chatRuntime.NewAgentLoop(resources.runtime, chatRuntime.DefaultAgentLoopConfig())
	if err != nil {
		return runtimeFailure(err)
	}
	runner, err := headless.NewStreamRunner(loop, headless.StreamRunConfig{
		SessionID: resources.identity.SessionID, ThreadID: resources.identity.ThreadID,
		Resumed: resources.resumed, Input: input, Output: options.Output, OutputCloser: outputCloser,
	})
	if err != nil {
		return runtimeFailure(fault.New(fault.CodeTurnFailed, "stream runner is not configured"))
	}
	resources.session = nil
	result, escalationErr := runStreamWithEscalation(ctx, runner, resources, shutdownTimeout)
	if escalationErr != nil {
		return runtimeFailure(escalationErr)
	}
	return outcomeFromHeadless(result)
}

func runStreamWithEscalation(
	ctx context.Context,
	runner *headless.StreamRunner,
	resources *chatResources,
	timeout time.Duration,
) (headless.Result, error) {
	resultDone := make(chan headless.Result, 1)
	go func() { resultDone <- runner.Run(ctx) }()
	select {
	case result := <-resultDone:
		return result, nil
	case <-ctx.Done():
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case result := <-resultDone:
		return result, nil
	case <-timer.C:
		closeErr := resources.forceCloseProvider()
		result := <-resultDone
		return result, errors.Join(context.DeadlineExceeded, closeErr)
	}
}

func runTUI(ctx context.Context, options Options, resources *chatResources) Outcome {
	controlSession, err := startTUIControlSession(ctx, resources.runtime)
	if err != nil {
		return runtimeFailure(err)
	}
	resources.session = controlSession
	model := tui.NewModel(ctx, Version, controlSession).WithInputHistory(resources.history).WithTranscriptQueries(resources.identity.ThreadID, resources.transcriptQueries)
	programOptions := []tea.ProgramOption{tea.WithContext(ctx), tea.WithOutput(options.Output)}
	if options.Input != nil {
		programOptions = append(programOptions, tea.WithInput(options.Input))
	}
	if _, err := tea.NewProgram(model, programOptions...).Run(); err != nil {
		return runtimeFailure(fault.Wrap(fault.CodeTurnFailed, "run TUI failed", err))
	}
	return Outcome{Class: ExitSuccess}
}

func outcomeFromHeadless(result headless.Result) Outcome {
	if result.Completed {
		return Outcome{Class: ExitSuccess}
	}
	report := ReportPending
	if result.OutputUnavailable {
		report = ReportOutputUnavailable
	} else if result.Reported {
		report = ReportComplete
	}
	return Outcome{Class: ExitRuntimeFailure, Report: report, Failure: result.Failure}
}

func openChatResources(
	ctx context.Context,
	applicationConfig config.Config,
	dataRoot string,
	resumeThreadID string,
	creationCWD string,
) (*chatResources, error) {
	return openChatResourcesWithSelection(
		ctx, applicationConfig, dataPaths{sessionRoot: dataRoot}, resumeThreadID, false, creationCWD,
	)
}

func openChatResourcesWithSelection(
	ctx context.Context,
	applicationConfig config.Config,
	paths dataPaths,
	resumeThreadID string,
	continueSession bool,
	creationCWD string,
) (*chatResources, error) {
	return openChatResourcesWithHooks(
		ctx, applicationConfig, paths, resumeThreadID, continueSession, creationCWD, chatResourceHooks{},
	)
}

func openChatResourcesWithHooks(
	ctx context.Context,
	applicationConfig config.Config,
	paths dataPaths,
	resumeThreadID string,
	continueSession bool,
	creationCWD string,
	hooks chatResourceHooks,
) (*chatResources, error) {
	loader, err := projectinstructions.NewLoader(projectinstructions.DefaultMaxBytes)
	if err != nil {
		return nil, fault.New(fault.CodeProjectInstructionsRead, "project instruction loader is invalid")
	}
	projectInstructions, err := loader.Load(creationCWD)
	if err != nil {
		return nil, err
	}
	return openChatResourcesWithSnapshot(
		ctx, applicationConfig, paths, resumeThreadID, continueSession, creationCWD,
		projectInstructions, hooks,
	)
}

func openChatResourcesWithSnapshot(
	ctx context.Context,
	applicationConfig config.Config,
	paths dataPaths,
	resumeThreadID string,
	continueSession bool,
	creationCWD string,
	projectInstructions domain.ProjectInstructionsSnapshot,
	hooks chatResourceHooks,
) (resources *chatResources, resultErr error) {
	cleanup := &resourceCleanup{}
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(resultErr, cleanup.close())
		}
	}()
	if err := applicationConfig.ValidateProvider(); err != nil {
		return nil, err
	}
	wire, err := providerWire(applicationConfig.Provider.Family)
	if err != nil {
		return nil, err
	}
	service, err := openSessionService(paths.sessionRoot)
	if err != nil {
		return nil, err
	}
	cleanup.add(service.close)
	artifactLayout, err := openCommandArtifactBoundary(ctx, service.repository.RootPath(), creationCWD)
	if err != nil {
		return nil, err
	}
	cleanup.add(artifactLayout.Close)
	workspace, err := builtin.OpenWorkspace(creationCWD)
	if err != nil {
		return nil, fault.New(fault.CodeInvalidConfiguration, "workspace is unavailable")
	}
	cleanup.add(workspace.Close)
	readExecutor, err := builtin.NewReadExecutor(workspace)
	if err != nil {
		return nil, fault.New(fault.CodeInvalidConfiguration, "Read executor is unavailable")
	}
	globExecutor, err := builtin.NewGlobExecutor(workspace)
	if err != nil {
		return nil, fault.New(fault.CodeInvalidConfiguration, "Glob executor is unavailable")
	}
	grepExecutor, err := builtin.NewGrepExecutor(workspace)
	if err != nil {
		return nil, fault.New(fault.CodeInvalidConfiguration, "Grep executor is unavailable")
	}
	patchPreparer, err := builtin.NewPatchPreparer(workspace)
	if err != nil {
		return nil, fault.New(fault.CodeInvalidConfiguration, "Patch preparer is unavailable")
	}
	patchExecutor, err := builtin.NewPatchExecutor(workspace)
	if err != nil {
		return nil, fault.New(fault.CodeInvalidConfiguration, "Patch executor is unavailable")
	}
	writePreparer, err := builtin.NewWritePreparer(workspace)
	if err != nil {
		return nil, fault.New(fault.CodeInvalidConfiguration, "Write preparer is unavailable")
	}
	writeExecutor, err := builtin.NewWriteExecutor(workspace)
	if err != nil {
		return nil, fault.New(fault.CodeInvalidConfiguration, "Write executor is unavailable")
	}
	toolPolicy := tool.NewApprovalPolicy()
	execPreparer := builtin.NewExecPlanner()
	if continueSession {
		threadID, selectErr := selectContinueThread(
			ctx, paths, service.repository, applicationConfig, wire, creationCWD,
		)
		if selectErr != nil {
			return nil, selectErr
		}
		resumeThreadID = string(threadID)
		if hooks.afterContinueSelection != nil {
			hooks.afterContinueSelection(threadID)
		}
	}
	providerInstance, err := newProviderResource(applicationConfig)
	if err != nil {
		return nil, err
	}
	cleanup.add(providerInstance.Close)
	var assembled assembledSession
	if resumeThreadID == "" {
		assembled, err = service.create(
			ctx, providerInstance, wire, applicationConfig.Provider.Model, creationCWD,
		)
	} else {
		threadID, parseErr := domain.ParseThreadID(resumeThreadID)
		if parseErr != nil {
			err = fault.New(fault.CodeSessionNotFound, "resume thread ID is invalid")
		} else {
			assembled, err = service.resume(
				ctx, providerInstance, wire, applicationConfig.Provider.Model, threadID,
			)
		}
	}
	if err != nil {
		return nil, err
	}
	cleanup.add(func() error { return assembled.writer.Close(context.Background()) })
	artifactStore, err := artifact.Open(ctx, artifactLayout, assembled.identity.ThreadID)
	if err != nil {
		return nil, fault.New(fault.CodeSessionWrite, "artifact storage is unavailable")
	}
	cleanup.add(artifactStore.Close)
	artifactReferences, err := tool.NewArtifactReferences(assembled.identity.ThreadID, session.ToolArtifactReferences(assembled.tools))
	if err != nil {
		return nil, fault.New(fault.CodeSessionCorruption, "artifact references are invalid")
	}
	if err = artifactStore.Clean(ctx, session.ToolArtifactReferences(assembled.tools)); err != nil {
		return nil, fault.New(fault.CodeSessionWrite, "artifact cleanup failed")
	}
	readArtifactExecutor, err := builtin.NewReadArtifactExecutor(artifactRangeReader{store: artifactStore, references: artifactReferences})
	if err != nil {
		return nil, err
	}
	var execExecutor tool.ExecExecutor
	if hooks.newExecExecutor != nil {
		execExecutor, err = hooks.newExecExecutor(workspace, builtin.OpenCommandEnvironment(), tool.RenderExecOutput)
	} else {
		var outputStore tool.CommandOutputStore = artifactOutputStore{store: artifactStore}
		if hooks.wrapOutputStore != nil {
			outputStore = hooks.wrapOutputStore(outputStore)
		}
		execExecutor, err = builtin.NewExecExecutor(workspace, builtin.OpenCommandEnvironment(), tool.RenderExecOutput, outputStore)
	}
	if err != nil {
		return nil, fault.New(fault.CodeInvalidConfiguration, "Exec executor is unavailable")
	}
	execExecutor = &commandArtifactBoundary{layout: artifactLayout, executor: execExecutor}
	toolCatalog, err := builtin.NewCatalog(
		readExecutor, globExecutor, grepExecutor, patchPreparer, patchExecutor, writePreparer, writeExecutor, execPreparer, execExecutor, tool.RenderExecOutput, toolPolicy, readArtifactExecutor,
	)
	if err != nil {
		return nil, fault.New(fault.CodeInvalidConfiguration, "tool catalog is invalid")
	}
	contextProfile, err := contextplan.NewProviderProfile(
		applicationConfig.Provider.Family, applicationConfig.Provider.Model,
	)
	if err != nil {
		return nil, fault.Wrap(fault.CodeInvalidConfiguration, "context profile is invalid", err)
	}
	queries := newTranscriptQueries(assembled.identity.ThreadID, artifactRangeReader{store: artifactStore, references: artifactReferences}, assembled.tools)
	projector, ok := assembled.conversation.(provider.TranscriptProjector)
	if !ok {
		return nil, fault.New(fault.CodeStreamProtocol, "transcript projection is unavailable")
	}
	if err = queries.bindTranscript(projector, toolCatalog, assembled.nativeCommits); err != nil {
		return nil, err
	}
	cleanup.add(queries.Close)
	runtimeInstance, err := chatRuntime.New(assembled.conversation, chatRuntime.Config{
		ArtifactReferences: artifactReferences,
		TextOutputStore:    artifactOutputStore{store: artifactStore},
		ToolHistory:        queries,
		SessionID:          assembled.identity.SessionID, ThreadID: assembled.identity.ThreadID,
		Journal: assembled.writer, ContextProfile: contextProfile,
		ToolCatalog:          toolCatalog,
		ReadExecutor:         readExecutor,
		ReadArtifactExecutor: readArtifactExecutor,
		GlobExecutor:         globExecutor,
		GrepExecutor:         grepExecutor,
		PatchPreparer:        patchPreparer,
		PatchExecutor:        patchExecutor,
		WritePreparer:        writePreparer,
		WriteExecutor:        writeExecutor,
		ExecPreparer:         execPreparer,
		ExecExecutor:         execExecutor,
		ToolPolicy:           toolPolicy,
		InteractiveApprovals: hooks.interactiveApprovals,
		ProjectInstructions:  projectInstructions,
		ContextBudget:        applicationConfig.ContextBudget, ContextPlanner: contextplan.NewPlanner(),
	})
	if err != nil {
		return nil, err
	}
	if assembled.toolRecovery != nil {
		if err := runtimeInstance.ReconcileToolTurn(ctx, assembled.toolRecovery.Clone()); err != nil {
			return nil, err
		}
		assembled.history = assembled.conversation.ProjectHistory()
	}
	resources = &chatResources{
		artifactLayout: artifactLayout, artifactStore: artifactStore, transcriptQueries: queries,
		identity: assembled.identity,
		provider: providerInstance, workspace: workspace, service: service, writer: assembled.writer,
		runtime: runtimeInstance, session: chatRuntime.NewChatSession(runtimeInstance), history: assembled.history,
		repair: assembled.repair, resumed: resumeThreadID != "",
	}
	cleanup.release()
	return resources, nil
}

func selectContinueThread(
	ctx context.Context,
	paths dataPaths,
	repository *session.Repository,
	applicationConfig config.Config,
	wire string,
	creationCWD string,
) (domain.ThreadID, error) {
	normalizedCWD, err := session.NormalizeCreationCWD(creationCWD)
	if err != nil {
		return "", fault.New(fault.CodeSessionCatalog, "session catalog selector is invalid")
	}
	instance, err := catalog.New(catalog.Config{
		DatabasePath: paths.catalogPath, SessionRoot: paths.sessionRoot,
	})
	if err != nil {
		return "", fault.New(fault.CodeSessionCatalog, "session catalog configuration is invalid")
	}
	if err := instance.Open(ctx); err != nil {
		return "", fault.New(fault.CodeSessionCatalog, "session catalog is unavailable")
	}
	defer func() { _ = instance.Close() }()
	report, err := instance.Reconcile(ctx, repository)
	if err != nil {
		return "", fault.New(fault.CodeSessionCatalog, "session catalog reconciliation failed")
	}
	if report.BusyUnindexed {
		return "", fault.New(fault.CodeSessionBusy, "session catalog contains an active unindexed thread")
	}
	entry, found, err := instance.LatestCompatible(ctx, catalog.Selector{
		CreationCWD: normalizedCWD, ProviderFamily: applicationConfig.Provider.Family,
		ProviderWire: wire, Model: applicationConfig.Provider.Model,
	})
	if err != nil {
		return "", fault.New(fault.CodeSessionCatalog, "session catalog query failed")
	}
	if !found {
		return "", fault.New(fault.CodeSessionNotFound, "compatible session thread was not found")
	}
	if err := instance.Close(); err != nil {
		return "", fault.New(fault.CodeSessionCatalog, "close session catalog failed")
	}
	return entry.ThreadID, nil
}

func newProviderResource(applicationConfig config.Config) (providerResource, error) {
	switch applicationConfig.Provider.Family {
	case domain.ProviderOpenAI:
		instance, err := openai.New(openai.Config{
			BaseURL: applicationConfig.Provider.BaseURL,
			APIKey:  applicationConfig.Provider.APIKey,
			Model:   applicationConfig.Provider.Model,
		})
		return instance, err
	case domain.ProviderAnthropic:
		instance, err := anthropic.New(anthropic.Config{
			BaseURL: applicationConfig.Provider.BaseURL,
			APIKey:  applicationConfig.Provider.APIKey,
			Model:   applicationConfig.Provider.Model,
		})
		return instance, err
	default:
		return nil, fault.New(fault.CodeProviderUnavailable, "provider is unavailable")
	}
}

func providerWire(family domain.ProviderFamily) (string, error) {
	switch family {
	case domain.ProviderOpenAI:
		return "responses", nil
	case domain.ProviderAnthropic:
		return "messages", nil
	default:
		return "", fault.New(fault.CodeProviderUnavailable, "provider is unavailable")
	}
}

func resolveDataPaths(sessionRoot string, catalogPath string) (dataPaths, error) {
	if sessionRoot != "" {
		absolute, err := filepath.Abs(sessionRoot)
		if err != nil {
			return dataPaths{}, fault.New(fault.CodeSessionWrite, "session data root is invalid")
		}
		sessionRoot = filepath.Clean(absolute)
	}
	if catalogPath != "" {
		absolute, err := filepath.Abs(catalogPath)
		if err != nil {
			return dataPaths{}, fault.New(fault.CodeSessionCatalog, "session catalog path is invalid")
		}
		catalogPath = filepath.Clean(absolute)
	}
	if sessionRoot != "" && catalogPath != "" {
		return dataPaths{sessionRoot: sessionRoot, catalogPath: catalogPath}, nil
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		if sessionRoot == "" {
			return dataPaths{}, fault.New(fault.CodeSessionWrite, "user home directory is unavailable")
		}
		return dataPaths{}, fault.New(fault.CodeSessionCatalog, "user home directory is unavailable")
	}
	dataHome := filepath.Join(home, ".easycode")
	if sessionRoot == "" {
		sessionRoot = filepath.Join(dataHome, "sessions")
	}
	if catalogPath == "" {
		catalogPath = filepath.Join(dataHome, "state.sqlite")
	}
	return dataPaths{sessionRoot: sessionRoot, catalogPath: catalogPath}, nil
}

func (resources *chatResources) close(ctx context.Context) error {
	if resources == nil {
		return nil
	}
	resources.closeMu.Lock()
	if resources.closeDone != nil {
		done := resources.closeDone
		resources.closeMu.Unlock()
		<-done
		return resources.closeErr
	}
	resources.closeDone = make(chan struct{})
	resources.closeMu.Unlock()
	var shutdownErr error
	providerClosed := resources.providerClosed
	if resources.session != nil {
		shutdownErr = resources.session.Shutdown(ctx)
		if shutdownErr != nil {
			// 超时后先关闭 transport 迫使阻塞流退出，再等待 Runtime 持久化唯一终态。
			if resources.provider != nil {
				shutdownErr = errors.Join(shutdownErr, resources.provider.Close())
				providerClosed = true
			}
			shutdownErr = errors.Join(shutdownErr, resources.session.Shutdown(context.Background()))
		}
	}
	closeContext := ctx
	if ctx.Err() != nil {
		closeContext = context.Background()
	}
	if resources.transcriptQueries != nil {
		_ = resources.transcriptQueries.Close()
	}
	var artifactErr error
	if resources.artifactStore != nil {
		artifactErr = resources.artifactStore.Close()
	}
	var writerErr error
	if resources.writer != nil {
		writerErr = resources.writer.Close(closeContext)
	}
	var providerErr error
	if resources.provider != nil && !providerClosed {
		providerErr = resources.provider.Close()
	}
	var workspaceErr error
	if resources.workspace != nil {
		workspaceErr = resources.workspace.Close()
	}
	var repositoryErr error
	if resources.artifactLayout != nil {
		repositoryErr = resources.artifactLayout.Close()
	}
	if resources.service != nil {
		repositoryErr = errors.Join(repositoryErr, resources.service.close())
	}
	resources.closeErr = errors.Join(shutdownErr, artifactErr, writerErr, providerErr, workspaceErr, repositoryErr)
	close(resources.closeDone)
	return resources.closeErr
}

func (resources *chatResources) forceCloseProvider() error {
	if resources == nil || resources.provider == nil || resources.providerClosed {
		return nil
	}
	resources.providerClosed = true
	return resources.provider.Close()
}
