package app

import (
	"context"
	"easycode/internal/config"
	"easycode/internal/domain"
	"easycode/internal/secret"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// TestPluginsTerminalHelper 只由显式 PTY 验收启动，复用现有终端确认协议。
func TestPluginsTerminalHelper(t *testing.T) {
	if os.Getenv("EASYCODE_PLUGINS_TERMINAL_SMOKE") != "1" {
		return
	}
	t.Setenv("HOME", privateAppTempDir(t))
	fd, err := strconv.Atoi(os.Getenv("EASYCODE_PLUGINS_ACK_FD"))
	if err != nil {
		t.Fatal(err)
	}
	ack := os.NewFile(uintptr(fd), "plugins-ack")
	defer ack.Close()
	workspace := privateAppTempDir(t)
	local := filepath.Join(workspace, ".easycode", "skills", "plugins", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(local), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(local, []byte("---\ndescription: Reserved local workflow\n---\nReserved workflow"), 0600); err != nil {
		t.Fatal(err)
	}
	family := domain.ProviderFamily(os.Getenv("EASYCODE_PLUGINS_FIXTURE_FAMILY"))
	if family != domain.ProviderOpenAI && family != domain.ProviderAnthropic {
		t.Fatal("plugin terminal fixture provider is invalid")
	}
	cfg := config.Config{Provider: config.Provider{Family: family, Model: "fixture-model", BaseURL: os.Getenv("EASYCODE_PLUGINS_FIXTURE_URL"), APIKey: secret.New("fixture-key")}}
	resources, err := openChatResourcesWithHooks(context.Background(), cfg, dataPaths{sessionRoot: filepath.Join(privateAppTempDir(t), "sessions")}, "", false, workspace, chatResourceHooks{interactiveApprovals: true, pluginDirs: []string{appPluginFixture(t, "claude")}})
	if err != nil {
		t.Fatal(err)
	}
	defer resources.close(context.Background())
	model, err := assembleChatModel(context.Background(), resources)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tea.NewProgram(completionTerminalModel{model: model, ack: json.NewEncoder(ack)}, tea.WithInput(os.Stdin), tea.WithOutput(os.Stdout)).Run(); err != nil {
		t.Fatal(err)
	}
	if err := resources.close(context.Background()); err != nil {
		t.Fatal(err)
	}
	fmt.Fprintln(os.Stdout, "PLUGINS_EXIT=0")
}
