package tui

import (
	"fmt"
	"strings"
)

// renderCommandApproval 按viewport预算保留目录、超时、风险和决定，只滚动命令正文。
func (model Model) renderCommandApproval() string {
	overlay := model.approval
	width := max(20, min(model.width-6, 92))
	contentWidth := width - 4
	workdir := overlay.request.RelativePath
	if workdir == "" {
		workdir = "."
	}
	metadata := wrapApprovalDiff("Workdir: "+commandDisplayText(workdir), contentWidth)
	metadata = append(metadata, wrapApprovalDiff(fmt.Sprintf("Timeout: %d ms", overlay.request.TimeoutMillis), contentWidth)...)
	metadata = append(metadata, wrapApprovalDiff("Workspace writes; no network. Detached processes may survive.", contentWidth)...)
	body := wrapApprovalDiff(commandDisplayText(overlay.request.Command), contentWidth)
	// 边框、标题、位置、三项决定和提示共占八行，其余交给正文。
	visible := max(1, model.height-len(metadata)-8)
	scroll := min(max(0, overlay.scroll), max(0, len(body)-visible))
	end := min(len(body), scroll+visible)
	padding := strings.Repeat(" ", max(0, (model.width-width-2)/2))
	var output strings.Builder
	output.WriteString(padding + "┌" + strings.Repeat("─", width) + "┐\n")
	line := func(value string) {
		fmt.Fprintf(&output, "%s│ %-*s │\n", padding, contentWidth, truncateRunes(value, contentWidth))
	}
	line("Approve command · Waiting")
	for _, value := range metadata {
		line(value)
	}
	line(fmt.Sprintf("Command %d-%d of %d", scroll+1, end, len(body)))
	for _, value := range body[scroll:end] {
		line(value)
	}
	for index, choice := range []string{"Yes", "No", "Abort turn"} {
		marker := " "
		if index == overlay.selected {
			marker = ">"
		}
		line(marker + " " + choice)
	}
	switch {
	case overlay.submitted:
		line("Submitting decision…")
	case overlay.editing:
		line("Feedback: " + commandDisplayText(string(overlay.feedback)))
	default:
		line("Enter: decide · Esc: interrupt")
	}
	output.WriteString(padding + "└" + strings.Repeat("─", width) + "┘\n")
	return output.String()
}
