package app

import (
	"context"
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
	if err = applicationConfig.ValidateProvider(); err != nil {
		return runtimeFailure(err)
	}
	wire, err := providerWire(applicationConfig.Provider.Family)
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
	location := time.Now().Location()
	_, _ = time.Now().In(location).Zone()
	return runPickerProgram(ctx, options, tui.PickerConfig{Location: location, Family: applicationConfig.Provider.Family, Wire: wire, Model: applicationConfig.Provider.Model, CWD: cwd, All: all}, newPickerExecutor(applicationConfig, paths, cwd, options.PluginDirs))
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
func runPickerProgram(ctx context.Context, options Options, configuration tui.PickerConfig, execute pickerExecutor) Outcome {
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	owner := newPickerOwner(execute)
	owner.Start(runCtx)
	output := &pickerOutput{target: options.Output, cancel: cancel}
	var terminalOutput io.Writer = output
	if file, ok := options.Output.(term.File); ok {
		terminalOutput = &pickerTerminalOutput{pickerOutput: output, file: file}
	}
	model := tui.NewStartupModel(owner, configuration)
	final, err := tea.NewProgram(model, tea.WithContext(runCtx), tea.WithInput(options.Input), tea.WithOutput(terminalOutput), tea.WithoutSignalHandler()).Run()
	owner.Stop()
	cleanupErr := owner.Wait()
	if cleanupErr != nil {
		return sessionListFailure("session picker cleanup or recovery failed")
	}
	if output.hasFailed() {
		return runtimeFailure(fault.New(fault.CodeOutput, "write session picker output failed"))
	}
	if ctx.Err() != nil {
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
