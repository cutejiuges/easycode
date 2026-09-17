// Package tui 实现 Bubble Tea 交互界面和 RuntimeEvent 投影。
package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"easycode/internal/protocol"
)

// RuntimeEventMsg 将 RuntimeEvent 送入 Bubble Tea update loop。
type RuntimeEventMsg struct {
	Event protocol.Event
}

// ErrorMsg 将异步命令错误送入 Bubble Tea update loop。
type ErrorMsg struct {
	Err error
}

// Model 保存纯 UI 投影状态，不持有 provider 或工具执行器。
type Model struct {
	version string
	width   int
	height  int
	events  []protocol.EventKind
	err     error
}

// NewModel 创建初始 Bubble Tea 模型。
func NewModel(version string) Model {
	return Model{version: version}
}

// Init 返回初始命令；真实 Session 订阅将在 P5 阶段接入。
func (Model) Init() tea.Cmd {
	return nil
}

// Update 只更新 UI 投影并将耗时操作留给 tea.Cmd。
func (model Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch value := message.(type) {
	case tea.KeyMsg:
		switch value.String() {
		case "ctrl+c", "q":
			return model, tea.Quit
		}
	case tea.WindowSizeMsg:
		model.width = value.Width
		model.height = value.Height
	case RuntimeEventMsg:
		model.events = append(model.events, value.Event.Kind)
	case ErrorMsg:
		model.err = value.Err
	}
	return model, nil
}

// View 渲染当前 UI 投影。
func (model Model) View() string {
	var view strings.Builder
	_, _ = fmt.Fprintf(&view, "EasyCode %s\n\n", model.version)
	view.WriteString("工程脚手架已就绪。\n")
	_, _ = fmt.Fprintf(&view, "窗口：%dx%d，事件：%d\n", model.width, model.height, len(model.events))
	if model.err != nil {
		_, _ = fmt.Fprintf(&view, "Error: %s\n", model.err)
	}
	view.WriteString("按 q 或 Ctrl+C 退出。\n")
	return view.String()
}
