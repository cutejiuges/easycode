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
	"easycode/internal/provider"
	"easycode/internal/provider/anthropic"
	"easycode/internal/provider/openai"
	chatRuntime "easycode/internal/runtime"
	"easycode/internal/session"
	"easycode/internal/tui"
)

const Version = "0.0.0-dev"

const shutdownTimeout = 5 * time.Second

// Options 描述应用入口参数，避免入口层直接依赖具体实现细节。
type Options struct {
	ShowVersion     bool
	Headless        bool
	ConfigPath      string
	ResumeThreadID  string
	SessionDataRoot string
	Input           io.Reader
	Output          io.Writer
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
}

// Run 根据入口参数启动 Bubble Tea 交互界面。
func Run(ctx context.Context, options Options) error {
	if options.Output == nil {
		return fmt.Errorf("output is required")
	}
	if options.ShowVersion {
		_, err := fmt.Fprintf(options.Output, "easycode %s\n", Version)
		return err
	}
	if options.Headless {
		return fault.New(fault.CodeNotImplemented, "--print is not implemented")
	}
	applicationConfig, err := config.Load(options.ConfigPath)
	if err != nil {
		return err
	}
	dataRoot, err := resolveSessionDataRoot(options.SessionDataRoot)
	if err != nil {
		return err
	}
	creationCWD, err := os.Getwd()
	if err != nil {
		return fault.New(fault.CodeSessionWrite, "current working directory is unavailable")
	}
	resources, err := newChatResources(
		ctx, applicationConfig, dataRoot, options.ResumeThreadID, creationCWD,
	)
	if err != nil {
		return err
	}

	model := tui.NewModel(Version, resources.session, resources.history)
	programOptions := []tea.ProgramOption{tea.WithContext(ctx), tea.WithOutput(options.Output)}
	if options.Input != nil {
		programOptions = append(programOptions, tea.WithInput(options.Input))
	}
	_, runErr := tea.NewProgram(model, programOptions...).Run()
	shutdownContext, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	closeErr := resources.close(shutdownContext)
	if runErr != nil {
		runErr = fmt.Errorf("run TUI: %w", runErr)
	}
	return errors.Join(runErr, closeErr)
}

func newChatResources(
	ctx context.Context,
	applicationConfig config.Config,
	dataRoot string,
	resumeThreadID string,
	creationCWD string,
) (*chatResources, error) {
	if err := applicationConfig.ValidateProvider(); err != nil {
		return nil, err
	}
	providerInstance, wire, err := newProviderResource(applicationConfig)
	if err != nil {
		return nil, err
	}
	service, err := newSessionService(dataRoot)
	if err != nil {
		_ = providerInstance.Close()
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
		repair: assembled.repair,
	}, nil
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

func resolveSessionDataRoot(explicit string) (string, error) {
	if explicit != "" {
		absolute, err := filepath.Abs(explicit)
		if err != nil {
			return "", fault.New(fault.CodeSessionWrite, "session data root is invalid")
		}
		return filepath.Clean(absolute), nil
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "", fault.New(fault.CodeSessionWrite, "user home directory is unavailable")
	}
	return filepath.Join(home, ".easycode", "sessions"), nil
}

func (resources *chatResources) close(ctx context.Context) error {
	if resources == nil {
		return nil
	}
	if resources.session != nil {
		if err := resources.session.Shutdown(ctx); err != nil {
			return err
		}
	}
	var writerErr error
	if resources.writer != nil {
		writerErr = resources.writer.Close(ctx)
	}
	var repositoryErr error
	if resources.service != nil {
		repositoryErr = resources.service.close()
	}
	var providerErr error
	if resources.provider != nil {
		providerErr = resources.provider.Close()
	}
	return errors.Join(writerErr, repositoryErr, providerErr)
}
