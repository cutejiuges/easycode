package tui

import (
	"easycode/internal/domain"
	"fmt"
	"strings"

	"easycode/internal/protocol"
)

// toolActivityState 只保留活动工具的瞬态状态；终态后删除，由宿主历史查询提供冻结结果。
type toolActivityState struct {
	activity protocol.ToolActivityPayload
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

// renderTranscript 只处理当前有界展示材料，相邻正常读取和搜索合并，异常打断分组。
func renderTranscript(messages []transcriptMessage, width int) []string {
	var lines []string
	files := make(map[string]struct{})
	searches, calls := 0, 0
	flush := func() {
		if calls == 0 {
			return
		}
		lines = append(lines, fmt.Sprintf("● Read %d files · %d searches", len(files), searches))
		clear(files)
		searches, calls = 0, 0
	}
	for _, message := range messages {
		a := message.activity
		if a != nil && message.saving == "" && a.Status == "success" && (a.Tool == protocol.ToolRead || a.Tool == protocol.ToolGlob || a.Tool == protocol.ToolGrep) {
			calls++
			if a.Tool == protocol.ToolRead {
				files[a.Target] = struct{}{}
			} else {
				searches++
			}
			continue
		}
		flush()
		if a == nil {
			body := message.role + ": " + commandDisplayText(message.text)
			if message.more {
				body += " … [Ctrl+O for full text]"
			}
			lines = append(lines, wrapApprovalDiff(body, width)...)
			continue
		}
		title := "● " + string(a.Tool) + "(" + commandDisplayText(a.Summary) + ")"
		if a.Status != "success" {
			title += " · " + a.Status
		}
		if a.ExitKnown && a.ExitCode != 0 {
			title += fmt.Sprintf(" · exit %d", a.ExitCode)
		}
		if a.Code != "" && a.Code != "ok" {
			title += " · " + a.Code
		}
		if h := message.hook; h != nil && h.State != "continue" {
			label := string(h.State)
			if h.Reference.Event == domain.HookPostToolUse {
				switch h.State {
				case protocol.HookActivityCompleted:
					label = "checks passed"
					if h.Code != domain.HookOK {
						label = "checks failed"
					}
				case protocol.HookActivitySkipped:
					label = "checks skipped"
				case protocol.HookActivityChecking:
					label = "checking"
				default:
					label = "checks " + label
				}
			} else {
				label = "check " + label
			}
			title += " · " + label
		}
		lines = append(lines, wrapApprovalDiff(title, width)...)
		if h := message.hook; h != nil {
			if h.Reference.Event == domain.HookPostToolUse && (h.State == protocol.HookActivityCompleted || h.State == protocol.HookActivitySkipped) {
				lines = append(lines, wrapApprovalDiff("  PostToolUse checks: "+string(h.Code)+" · Ctrl+O: details", width)...)
			} else {
				lines = append(lines, wrapApprovalDiff(fmt.Sprintf("  %s check %s:%s · %s · timeout %ds · stdout %d bytes · stderr %d bytes", h.Reference.Event, h.Reference.Source, h.Reference.ID, h.State, h.Reference.TimeoutSeconds, h.StdoutBytes, h.StderrBytes), width)...)
				if h.State != "checking" {
					lines = append(lines, wrapApprovalDiff("  Raw hook output was not recorded.", width)...)
				}
			}
		}

		if message.saving != "" {
			lines = append(lines, wrapApprovalDiff("  "+message.saving, width)...)
		}
		body := message.preview
		if body == "" {
			body = a.Recent
		}
		if body != "" {
			output := wrapApprovalDiff(commandDisplayText(strings.TrimSuffix(body, "\n")), max(1, width-2))
			count := min(3, len(output))
			for _, line := range output[:count] {
				lines = append(lines, "  "+line)
			}
			if len(output) > count {
				lines = append(lines, "  … Ctrl+O for details")
			}
		}
	}
	flush()
	return lines
}

func savingNotice(outputs ...protocol.ToolOutputSummary) string {
	partial, unavailable := false, false
	for _, output := range outputs {
		partial = partial || output.Status == "partial"
		unavailable = unavailable || output.Status == "unavailable"
	}
	if partial && unavailable {
		return "Saved output: partial prefix; some output unavailable"
	}
	if partial {
		return "Saved output: partial prefix"
	}
	if unavailable {
		return "Saved output unavailable"
	}
	return ""
}
