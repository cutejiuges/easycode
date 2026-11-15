//go:build darwin

package app

import (
	"testing"

	"easycode/internal/session"
	"easycode/internal/tool"
)

// mustEntryInputEvidence 与真实消费者同属 Darwin 的后置执行回归。
func mustEntryInputEvidence(t *testing.T, entry session.ToolHistoryEntry) tool.InputResultEvidence {
	t.Helper()
	evidence, err := entry.InputResultEvidence()
	if err != nil {
		t.Fatal(err)
	}
	return evidence
}
