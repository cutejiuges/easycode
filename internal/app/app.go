// Package app 负责装配应用依赖和管理顶层生命周期。
package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"easycode/internal/config"
	"easycode/internal/domain"
	"easycode/internal/fault"
	"easycode/internal/headless"
	"easycode/internal/provider"
	"easycode/internal/provider/anthropic"
	"easycode/internal/provider/openai"
	chatRuntime "easycode/internal/runtime"
	"easycode/internal/session"
	"easycode/internal/session/catalog"
	"easycode/internal/tui"
)

const Version = "0.0.0-dev"

const shutdownTimeout = 5 * time.Second

var _ headless.ChatSession = (*chatRuntime.ChatSession)(nil)

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
}

type providerResource interface {
	provider.Factory
	Close() error
}

type chatResources struct {
	identity session.Identity
	provider providerResource
	service  *sessionService
	writer   managedJournal
	session  *chatRuntime.ChatSession
	history  domain.SemanticHistoryView
	repair   session.RepairReport
	resumed  bool
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
	if options.Mode != headless.ModeInteractive && options.Mode != headless.ModeText && options.Mode != headless.ModeJSON {
		return runtimeFailure(fault.New(fault.CodeTurnFailed, "application mode is invalid"))
	}
	if options.Mode != headless.ModeInteractive {
		if err := headless.ValidateResolvedPrompt(options.Prompt); err != nil {
			return usageFailure(err.Error())
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
	resources, err := openChatResourcesWithSelection(
		ctx, applicationConfig, paths, options.ResumeThreadID, options.ContinueSession, creationCWD,
	)
	if err != nil {
		return runtimeFailure(err)
	}

	var outcome Outcome
	if options.Mode == headless.ModeInteractive {
		outcome = runTUI(ctx, options, resources)
	} else {
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

func runTUI(ctx context.Context, options Options, resources *chatResources) Outcome {
	model := tui.NewModel(ctx, Version, resources.session, resources.history)
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
	if continueSession {
		threadID, selectErr := selectContinueThread(
			ctx, paths, service.repository, applicationConfig, wire, creationCWD,
		)
		if selectErr != nil {
			_ = service.close()
			return nil, selectErr
		}
		resumeThreadID = string(threadID)
		if hooks.afterContinueSelection != nil {
			hooks.afterContinueSelection(threadID)
		}
	}
	providerInstance, _, err := newProviderResource(applicationConfig)
	if err != nil {
		_ = service.close()
		return nil, err
	}
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
		_ = service.close()
		_ = providerInstance.Close()
		return nil, err
	}
	runtimeInstance, err := chatRuntime.New(assembled.conversation, chatRuntime.Config{
		SessionID: assembled.identity.SessionID, ThreadID: assembled.identity.ThreadID,
		Journal: assembled.writer,
	})
	if err != nil {
		_ = assembled.writer.Close(context.Background())
		_ = service.close()
		_ = providerInstance.Close()
		return nil, err
	}
	return &chatResources{
		identity: assembled.identity,
		provider: providerInstance, service: service, writer: assembled.writer,
		session: chatRuntime.NewChatSession(runtimeInstance), history: assembled.history,
		repair: assembled.repair, resumed: resumeThreadID != "",
	}, nil
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

func newProviderResource(applicationConfig config.Config) (providerResource, string, error) {
	switch applicationConfig.Provider.Family {
	case domain.ProviderOpenAI:
		instance, err := openai.New(openai.Config{
			BaseURL: applicationConfig.Provider.BaseURL,
			APIKey:  applicationConfig.Provider.APIKey,
			Model:   applicationConfig.Provider.Model,
		})
		return instance, "responses", err
	case domain.ProviderAnthropic:
		instance, err := anthropic.New(anthropic.Config{
			BaseURL: applicationConfig.Provider.BaseURL,
			APIKey:  applicationConfig.Provider.APIKey,
			Model:   applicationConfig.Provider.Model,
		})
		return instance, "messages", err
	default:
		return nil, "", fault.New(fault.CodeProviderUnavailable, "provider is unavailable")
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
	var shutdownErr error
	providerClosed := false
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
	var writerErr error
	if resources.writer != nil {
		writerErr = resources.writer.Close(closeContext)
	}
	var repositoryErr error
	if resources.service != nil {
		repositoryErr = resources.service.close()
	}
	var providerErr error
	if resources.provider != nil && !providerClosed {
		providerErr = resources.provider.Close()
	}
	return errors.Join(shutdownErr, writerErr, repositoryErr, providerErr)
}
