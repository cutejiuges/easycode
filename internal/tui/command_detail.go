package tui

import (
	"fmt"
	"strings"

	"easycode/internal/protocol"
	tea "github.com/charmbracelet/bubbletea"
)

// commandDetail 仅保存宿主已接收的候选与瞬态结果投影，不读取外部资源。
type commandDetail struct {
	fullCommand   string
	activity      protocol.CommandActivityPayload
	resultPreview string
	terminal      bool
}

func (model Model) handleCommandDetailKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.Type {
	case tea.KeyEsc:
		model.detailOpen = false
	case tea.KeyPgUp, tea.KeyUp:
		model.detailScroll = max(0, model.detailScroll-1)
	case tea.KeyPgDown, tea.KeyDown:
		model.detailScroll++
	case tea.KeyCtrlC:
		if model.state != stateIdle {
			model.cancelling = true
			if model.events == nil {
				model.interruptNext = true
				return model, nil
			}
			return model, interruptSession(model.session)
		}
	}
	return model, nil
}

func (model Model) renderCommandDetail() string {
	detail, exists := model.commandViews[model.latestCommand]
	if !exists {
		return "Command details unavailable\n"
	}
	width := max(16, model.width-4)
	command := detail.fullCommand
	if command == "" {
		command = "Full command unavailable"
	}
	output := detail.activity.Recent
	if detail.terminal {
		output = detail.resultPreview
	}
	if output == "" {
		output = "Waiting for output…"
	}
	lines := wrapApprovalDiff("Command:\n"+commandDisplayText(command)+"\n\nOutput:\n"+commandDisplayText(output), width)
	visible := max(1, model.height-7)
	scroll := min(max(0, model.detailScroll), max(0, len(lines)-visible))
	end := min(len(lines), scroll+visible)
	state := "Running"
	if detail.terminal {
		state = detail.activity.Status + " (" + detail.activity.Code + ")"
		if detail.activity.ExitKnown {
			state += fmt.Sprintf(" exit=%d", detail.activity.ExitCode)
		}
	}
	if detail.activity.Truncated {
		state += " · truncated"
	}
	if model.cancelling && !detail.terminal {
		state = "Cancelling — waiting for cleanup"
	}
	workdir := detail.activity.Workdir
	if workdir == "" {
		workdir = "."
	}
	var view strings.Builder
	view.WriteString("Command details\n")
	view.WriteString(truncateRunes(state, width) + "\n")
	view.WriteString(truncateRunes("Workdir: "+commandDisplayText(workdir), width) + "\n")
	fmt.Fprintf(&view, "%d ms · %d bytes\n", detail.activity.ElapsedMillis, detail.activity.StdoutBytes+detail.activity.StderrBytes)
	fmt.Fprintf(&view, "Lines %d-%d of %d\n", scroll+1, end, len(lines))
	view.WriteString(strings.Join(lines[scroll:end], "\n") + "\n")
	view.WriteString(truncateRunes("PgUp/PgDn: scroll · Ctrl+O/Esc: close", width) + "\n")
	if model.state != stateIdle {
		view.WriteString("Ctrl+C: interrupt\n")
	} else {
		view.WriteString("\n")
	}
	return view.String()
}

func commandSummary(command string, viewportWidth int) string {
	if viewportWidth <= 0 {
		viewportWidth = 80
	}
	command = truncateRunes(commandDisplayText(command), 160)
	lines := wrapApprovalDiff(command, max(1, viewportWidth-10))
	if len(lines) <= 2 {
		return truncateRunes(strings.Join(lines, "\n"), 160)
	}
	last := []rune(lines[1])
	if len(last) > 0 {
		last[len(last)-1] = '…'
	}
	return truncateRunes(lines[0]+"\n"+string(last), 160)
}
