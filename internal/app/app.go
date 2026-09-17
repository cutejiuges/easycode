// Package app 负责装配应用依赖和管理顶层生命周期。
package app

import (
	"context"
	"fmt"
	"io"

	tea "github.com/charmbracelet/bubbletea"

	"easycode/internal/tui"
)

const Version = "0.0.0-dev"

// Options 描述应用入口参数，避免入口层直接依赖具体实现细节。
type Options struct {
	ShowVersion bool
	Headless    bool
	Input       io.Reader
	Output      io.Writer
}

// Run 根据入口参数启动 headless 模式或 Bubble Tea 交互界面。
func Run(ctx context.Context, options Options) error {
	if options.Output == nil {
		return fmt.Errorf("output is required")
	}

	if options.ShowVersion {
		_, err := fmt.Fprintf(options.Output, "easycode %s\n", Version)
		return err
	}

	if options.Headless {
		_, err := fmt.Fprintln(options.Output, "EasyCode scaffold is ready.")
		return err
	}

	model := tui.NewModel(Version)
	programOptions := []tea.ProgramOption{
		tea.WithContext(ctx),
		tea.WithOutput(options.Output),
	}
	if options.Input != nil {
		programOptions = append(programOptions, tea.WithInput(options.Input))
	}

	_, err := tea.NewProgram(model, programOptions...).Run()
	if err != nil {
		return fmt.Errorf("run TUI: %w", err)
	}
	return nil
}
