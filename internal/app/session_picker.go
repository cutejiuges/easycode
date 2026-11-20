package app

import (
	"context"
	"errors"
	"io"
	"os"
	"sync"
	"time"

	"easycode/internal/config"
	"easycode/internal/extension/plugin"
	"easycode/internal/fault"
	"easycode/internal/session"
	"easycode/internal/tui"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/term"
)

// RunSessionPicker 冻结配置和目录，在单一终端程序内发现并恢复会话。
func RunSessionPicker(ctx context.Context, options Options, all bool) Outcome {
	if err := plugin.ValidateDirectories(options.PluginDirs); err != nil {
		return usageFailure(err.Error())
	}
	options.PluginDirs = append([]string(nil), options.PluginDirs...)
	if options.Input == nil || options.Output == nil {
		return usageFailure("session picker input and output are required")
	}
	applicationConfig, err := config.Load(options.ConfigPath)
	if err != nil {
		return runtimeFailure(err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return sessionListFailure("current working directory is unavailable")
	}
	cwd, err = session.NormalizeCreationCWD(cwd)
	if err != nil {
		return sessionListFailure("current working directory is invalid")
	}
	paths, err := resolveDataPaths(options.SessionDataRoot, options.SessionCatalogPath)
	if err != nil {
		return runtimeFailure(err)
	}
	return runInteractiveProgram(ctx, options, applicationConfig, paths, cwd, true, all)
}

type pickerOutput struct {
	target io.Writer
	cancel context.CancelFunc
	mu     sync.Mutex
	failed bool
}

func (w *pickerOutput) Write(p []byte) (int, error) {
	n, err := w.target.Write(p)
	if err == nil && n != len(p) {
		err = io.ErrShortWrite
	}
	if err != nil {
		w.mu.Lock()
		w.failed = true
		w.mu.Unlock()
		w.cancel()
	}
	return n, err
}
func (w *pickerOutput) hasFailed() bool { w.mu.Lock(); defer w.mu.Unlock(); return w.failed }
func runInteractiveProgram(ctx context.Context, options Options, cfg config.Config, paths dataPaths, cwd string, startupPicker, all bool) Outcome {
	binding, err := bindProvider(cfg.Provider.Family)
	if err != nil {
		return runtimeFailure(err)
	}
	location := time.Now().Location()
	_, _ = time.Now().In(location).Zone()
	pickerConfig := tui.PickerConfig{Location: location, Family: cfg.Provider.Family, Wire: binding.wire, Model: cfg.Provider.Model, CWD: cwd, All: all}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	environment := &chatEnvironment{}
	owner := newPickerOwner(newPickerExecutor(cfg, paths, cwd, options.PluginDirs, environment))
	var model tui.StartupModel
	if startupPicker {
		model = tui.NewStartupModel(owner, pickerConfig)
	} else {
		lifetime, closeLifetime := context.WithCancel(runCtx)
		resources, err := openChatResourcesWithHooks(runCtx, cfg, paths, options.ResumeThreadID, options.ContinueSession, cwd, chatResourceHooks{
			interactiveApprovals: true, pluginDirs: options.PluginDirs, pluginCWD: cwd, environment: environment, lifetime: lifetime,
		})
		if err != nil {
			closeLifetime()
			return runtimeFailure(errors.Join(err, environment.close()))
		}
		resources.cancelLifetime = closeLifetime
		chat, err := assembleChatModel(lifetime, resources)
		if err != nil {
			return runtimeFailure(errors.Join(err, closePickerResources(resources), environment.close()))
		}
		owner.initial = resources
		model = tui.NewChatShell(owner, pickerConfig, chat)
	}
	outcome := runPickerTerminal(runCtx, cancel, options, owner, model)
	if err := environment.close(); err != nil {
		return runtimeFailure(err)
	}
	return outcome
}

func runPickerTerminal(runCtx context.Context, cancel context.CancelFunc, options Options, owner *pickerOwner, model tui.StartupModel) Outcome {
	owner.Start(runCtx)
	output := &pickerOutput{target: options.Output, cancel: cancel}
	var terminalOutput io.Writer = output
	if file, ok := options.Output.(term.File); ok {
		terminalOutput = &pickerTerminalOutput{pickerOutput: output, file: file}
	}
	// 进程信号由调用方的 context 统一处理，避免事件循环退出后 Bubble Tea 的信号处理器阻塞。
	programOptions := []tea.ProgramOption{tea.WithContext(runCtx), tea.WithOutput(terminalOutput), tea.WithoutSignalHandler()}
	if options.Input != nil {
		programOptions = append(programOptions, tea.WithInput(options.Input))
	}
	final, err := tea.NewProgram(model, programOptions...).Run()
	cancelled := runCtx.Err() != nil
	owner.Stop()
	cancel()
	cleanupErr := owner.Wait()
	if errors.Is(cleanupErr, context.DeadlineExceeded) {
		return sessionListFailure("session cleanup timed out")
	}
	if cleanupErr != nil {
		return sessionListFailure("session picker cleanup or recovery failed")
	}
	if output.hasFailed() {
		return runtimeFailure(fault.New(fault.CodeOutput, "write session picker output failed"))
	}
	if cancelled {
		return Outcome{Class: ExitCancelled, Report: ReportComplete}
	}
	if err != nil {
		return sessionListFailure("session picker terminal failed")
	}
	switch final.(tui.StartupModel).ExitCode() {
	case 130:
		return Outcome{Class: ExitCancelled, Report: ReportComplete}
	case 1:
		return sessionListFailure("session picker failed")
	}
	return Outcome{Class: ExitSuccess}
}

// pickerTerminalOutput 保留实际终端描述符，避免错误监测包装隐藏 resize 能力。
type pickerTerminalOutput struct {
	*pickerOutput
	file term.File
}

func (w *pickerTerminalOutput) Fd() uintptr                { return w.file.Fd() }
func (w *pickerTerminalOutput) Read(p []byte) (int, error) { return w.file.Read(p) }
func (w *pickerTerminalOutput) Close() error               { return w.file.Close() }
