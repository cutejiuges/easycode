package app

import (
	"easycode/internal/tool/builtin"
	"os"
	"testing"
)

// TestMain 保留命令子进程入口，并隔离所有宿主测试的用户来源目录。
func TestMain(m *testing.M) {
	if handled, err := builtin.RunCommandChild(os.Args[1:]); handled {
		if err != nil {
			os.Exit(125)
		}
		os.Exit(0)
	}
	home, err := os.MkdirTemp("", "easycode-test-home-")
	if err != nil {
		os.Exit(2)
	}
	if err := os.Setenv("HOME", home); err != nil {
		os.RemoveAll(home)
		os.Exit(2)
	}
	code := m.Run()
	if err := os.RemoveAll(home); err != nil && code == 0 {
		code = 2
	}
	os.Exit(code)
}
