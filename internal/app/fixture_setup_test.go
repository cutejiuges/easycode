package app

import (
	"context"
	"os"
	"strconv"
	"testing"

	"easycode/internal/config"
)

// readAppTestFile 只读取调用方指定的 fixture 或事实文件，不修复或重写内容。
func readAppTestFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func writeAppTestFile(t *testing.T, path string, data []byte, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, data, mode); err != nil {
		t.Fatal(err)
	}
}

// mustOpenChatResources 只复用成功装配；关闭时点和资源所有权仍由用例明确控制。
func mustOpenChatResources(t *testing.T, ctx context.Context, cfg config.Config, root, thread, workspace string) *chatResources {
	t.Helper()
	resources, err := openChatResources(ctx, cfg, root, thread, workspace)
	if err != nil {
		t.Fatal(err)
	}
	return resources
}

func mustOpenChatResourcesWithHooks(t *testing.T, ctx context.Context, cfg config.Config, paths dataPaths, thread string, continueSession bool, workspace string, hooks chatResourceHooks) *chatResources {
	t.Helper()
	resources, err := openChatResourcesWithHooks(ctx, cfg, paths, thread, continueSession, workspace, hooks)
	if err != nil {
		t.Fatal(err)
	}
	return resources
}

func closeAppTestResources(t *testing.T, resources *chatResources) {
	t.Helper()
	if err := resources.close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// openTerminalAck 只接管脚本传入的文件描述符；关闭仍由原终端用例负责。
func openTerminalAck(t *testing.T, variable, name string) *os.File {
	t.Helper()
	fd, err := strconv.Atoi(os.Getenv(variable))
	if err != nil {
		t.Fatal(err)
	}
	return os.NewFile(uintptr(fd), name)
}
