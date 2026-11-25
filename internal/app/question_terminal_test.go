package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"easycode/internal/config"
	"easycode/internal/protocol"
)

type questionTerminalObserver struct {
	model tea.Model
	ack   *os.File
}

func (m questionTerminalObserver) Init() tea.Cmd { return m.model.Init() }
func (m questionTerminalObserver) View() string  { return m.model.View() }
func (m questionTerminalObserver) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	next, cmd := m.model.Update(msg)
	m.model = next
	if item, ok := msg.(protocol.ControlItem); ok {
		if e, ok := item.TurnEvent(); ok && (e.Kind == protocol.EventTurnCompleted || e.Kind == protocol.EventTurnFailed) {
			fmt.Fprintln(m.ack, "TURN_IDLE")
		}
	}
	if size, ok := msg.(tea.WindowSizeMsg); ok {
		fmt.Fprintf(m.ack, "SIZE %d %d\n", size.Width, size.Height)
	}
	return m, cmd
}

// TestQuestionTerminalHelper 由真实 PTY 脚本显式启动，普通测试不接管终端。
func TestQuestionTerminalHelper(t *testing.T) {
	if os.Getenv("EASYCODE_QUESTION_TERMINAL_SMOKE") != "1" {
		return
	}
	for _, name := range []string{"EASYCODE_PROVIDER", "EASYCODE_BASE_URL", "EASYCODE_API_KEY", "EASYCODE_MODEL"} {
		t.Setenv(name, "")
	}
	dir := privateAppTempDir(t)
	configuration := filepath.Join(dir, "config.json")
	text := fmt.Sprintf(`{"provider":"openai","base_url":%q,"api_key":"fixture-key","model":"fixture-model"}`, os.Getenv("EASYCODE_QUESTION_FIXTURE_URL"))
	writeAppTestFile(t, configuration, []byte(text), 0600)
	options := Options{ConfigPath: configuration, SessionDataRoot: filepath.Join(dir, "sessions"), SessionCatalogPath: filepath.Join(dir, "catalog.json"), Input: os.Stdin, Output: os.Stdout}
	var outcome Outcome
	if os.Getenv("EASYCODE_QUESTION_PICKER") == "1" {
		data := readAppTestFile(t, "../session/testdata/current/question_openai_complete.jsonl")
		root, _, _ := installPreviewFixture(t, data)
		options.SessionDataRoot = root
		outcome = RunSessionPicker(context.Background(), options, true)
	} else {
		ack := openTerminalAck(t, "EASYCODE_QUESTION_ACK_FD", "question-ack")
		defer ack.Close()
		cfg, err := config.Load(configuration)
		if err != nil {
			t.Fatal(err)
		}
		cwd, err := os.Getwd()
		if err != nil {
			t.Fatal(err)
		}
		resources := mustOpenChatResourcesWithHooks(t, context.Background(), cfg, dataPaths{sessionRoot: options.SessionDataRoot}, "", false, cwd, chatResourceHooks{interactiveApprovals: true})
		model, err := assembleChatModel(context.Background(), resources)
		if err != nil {
			t.Fatal(err)
		}
		_, err = tea.NewProgram(questionTerminalObserver{model: model, ack: ack}, tea.WithInput(os.Stdin), tea.WithOutput(os.Stdout)).Run()
		if err != nil {
			t.Fatal(err)
		}
		closeAppTestResources(t, resources)
		outcome = Outcome{Class: ExitSuccess}
	}
	fmt.Fprintf(os.Stdout, "\nQUESTION_EXIT=%d\n", outcome.ExitCode())
}
