package tui

import (
	"strings"

	"easycode/internal/protocol"
)

// commandDetail 只保留活动命令的瞬态进度；终态后删除，由宿主历史查询提供冻结结果。
type commandDetail struct {
	activity protocol.CommandActivityPayload
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
