//go:build !darwin && !linux

package builtin

import "testing"

func TestOpenWorkspaceFailsClosedOnUnsupportedPlatform(t *testing.T) {
	if workspace, err := OpenWorkspace(t.TempDir()); err == nil || workspace != nil {
		t.Fatal("不支持平台不得回退到普通路径读取")
	}
}
