// Package app 负责装配应用依赖和管理顶层生命周期。
package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"easycode/internal/config"
	"easycode/internal/domain"
	"easycode/internal/fault"
	"easycode/internal/provider/openai"
	chatRuntime "easycode/internal/runtime"
	"easycode/internal/tui"
)

const Version = "0.0.0-dev"

const shutdownTimeout = 5 * time.Second

// Options 描述应用入口参数，避免入口层直接依赖具体实现细节。
type Options struct {
	ShowVersion bool
	Headless    bool
	Input       io.Reader
	Output      io.Writer
}

type chatResources struct {
	provider *openai.Provider
	session  *chatRuntime.ChatSession
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

	resources, err := newChatResources(config.LoadFromEnv())
	if err != nil {
		return err
	}

	model := tui.NewModel(Version, resources.session)
	programOptions := []tea.ProgramOption{
		tea.WithContext(ctx),
		tea.WithOutput(options.Output),
	}
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

func newChatResources(applicationConfig config.Config) (*chatResources, error) {
	if err := applicationConfig.ValidateProvider(); err != nil {
		return nil, err
	}
	if applicationConfig.Provider.Family != domain.ProviderOpenAI {
		return nil, fault.New(fault.CodeProviderUnavailable, "interactive chat only supports OpenAI Responses")
	}

	providerInstance, err := openai.New(openai.Config{
		BaseURL: applicationConfig.Provider.BaseURL,
		APIKey:  applicationConfig.Provider.APIKey,
		Model:   applicationConfig.Provider.Model,
	})
	if err != nil {
		return nil, err
	}
	conversation := providerInstance.NewConversation()
	runtimeInstance := chatRuntime.New(conversation)
	return &chatResources{
		provider: providerInstance,
		session:  chatRuntime.NewChatSession(runtimeInstance),
	}, nil
}

func (resources *chatResources) close(ctx context.Context) error {
	if resources == nil {
		return nil
	}
	var shutdownErr error
	if resources.session != nil {
		shutdownErr = resources.session.Shutdown(ctx)
	}
	var providerErr error
	if resources.provider != nil {
		providerErr = resources.provider.Close()
	}
	return errors.Join(shutdownErr, providerErr)
}
