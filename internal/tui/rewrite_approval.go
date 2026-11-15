package tui

import "easycode/internal/protocol"

// rewriteApprovalTooSmall 在必要审查字段无法呈现时停用批准，不改变原默认拒绝。
func (m Model) rewriteApprovalTooSmall() bool {
	return m.approval != nil && m.approval.request.InputRewrite.Kind() == protocol.PreInputSelected && (m.widthOrDefault() < 32 || m.heightOrDefault() < 18)
}
