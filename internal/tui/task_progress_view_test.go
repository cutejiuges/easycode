package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/protocol"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func taskViewBounds(t testing.TB, view string, width, height int) {
	t.Helper()
	if renderedChatRows(view) > height {
		t.Fatalf("view exceeds height %d: %s", height, view)
	}
	for _, line := range strings.Split(view, "\n") {
		if ansi.StringWidth(line) > width {
			t.Fatalf("view exceeds width %d: %q", width, line)
		}
	}
}

func TestTasksFixedSnapshotsAndSafeText(t *testing.T) {
	content := "检查中文任务👩‍💻 e\u0301 " + strings.Repeat("长", 100) + "\u202e"
	active := "正在检查👩‍💻 中文\u2066 active"
	update := taskTestUpdate(t, []domain.TaskStep{
		taskTestStep(t, content, active, domain.TaskInProgress),
		taskTestStep(t, "Review result", "", domain.TaskPending),
		taskTestStep(t, "Completed report", "", domain.TaskCompleted),
	}, "First line\n\tIndented explanation\u202e stays stored.")
	state := taskTestSourceState(t, 60, taskTestTurn, taskTestCall, update)
	stamp := state.ContentStamp()
	for _, test := range []struct {
		size  tea.WindowSizeMsg
		panel bool
	}{
		{tea.WindowSizeMsg{Width: 80, Height: 24}, false},
		{tea.WindowSizeMsg{Width: 80, Height: 24}, true},
		{tea.WindowSizeMsg{Width: 13, Height: 6}, false},
	} {
		name := fmt.Sprintf("tasks_%dx%d_colorfalse_panel%t", test.size.Width, test.size.Height, test.panel)
		t.Run(name, func(t *testing.T) {
			m := taskTestObserve(t, taskTestModel(t), state).WithMarkdownColor(false)
			m = updateModel(t, m, test.size)
			m.tasks.open = test.panel
			got := m.View()
			checkTaskSnapshot(t, name, got)
			taskViewBounds(t, got, test.size.Width, test.size.Height)
			if strings.ContainsAny(got, "\x1b\u202e\u2066") || m.tasks.snapshot.Progress.ContentStamp() != stamp || !m.tasks.snapshot.Progress.Equal(state) {
				t.Fatal("rendering leaked controls or altered canonical facts")
			}
			if test.panel {
				var complete strings.Builder
				for _, row := range m.taskPanelRows() {
					complete.WriteString(row.text)
				}
				if !strings.Contains(complete.String(), `\u202e`) {
					t.Fatal("bidi format was not visibly escaped in scrollable text")
				}
			}
		})
	}
	if safeComposerText("\x1b[2J\a\x7f\u0085\u202e") != `\u001b[2J\u0007\u007f\u0085\u202e` {
		t.Fatal("shared text renderer did not defend terminal controls")
	}
	if update.Items()[0].Content() != content || update.Explanation() != "First line\n\tIndented explanation\u202e stays stored." {
		t.Fatal("display changed the original text")
	}
}

func TestTasksFocusPrioritiesAndActiveCancellation(t *testing.T) {
	m := taskTestObserve(t, taskTestModel(t), taskTestState(t, 80, 20, 17, false))
	m.composer = composerWithText(t, "kept draft")
	m, _ = taskTestQuery(t, m, true)
	m.tasks.scroll = 8
	draft := m.composer
	for name, set := range map[string]func(*Model){
		"question": func(m *Model) {
			payload, err := protocol.DecodeUserQuestionRequested(questionUIEvent(t, 1))
			if err != nil {
				t.Fatal(err)
			}
			m.question = newQuestionOverlay(payload, taskTestTurn, m.widthOrDefault(), m.heightOrDefault())
		},
		"approval": func(m *Model) { m.approval = &approvalOverlay{} },
		"trust":    func(m *Model) { m.hookTrust = &hookTrustOverlay{} },
		"hooks":    func(m *Model) { m.hooks.open = true },
	} {
		t.Run(name, func(t *testing.T) {
			x := m
			set(&x)
			x = updateModel(t, x, tea.KeyMsg{Type: tea.KeyCtrlT})
			if !x.tasks.open || x.tasks.scroll != 8 || !x.tasks.expanded || !reflect.DeepEqual(x.composer, draft) || x.completion.rows != 0 {
				t.Fatal("higher priority focus leaked Ctrl+T into task or composer")
			}
			x.question, x.approval, x.hookTrust = nil, nil, nil
			x.hooks.open = false
			if !strings.Contains(x.View(), "Tasks ·") || x.tasks.scroll != 8 {
				t.Fatal("return from higher priority interaction lost the panel")
			}
		})
	}
	m.activeTurnID, m.state = taskTestTurn, stateStreaming
	m, command := updateModelWithCommand(t, m, tea.KeyMsg{Type: tea.KeyCtrlC})
	if command == nil || m.tasks.open || !m.cancelling || !reflect.DeepEqual(m.composer, draft) {
		t.Fatal("panel Ctrl+C did not follow active-turn cancellation")
	}
	command()
	if m.session.(*taskFixtureSession).interrupts != 1 || len(m.session.(*taskFixtureSession).submits) != 0 {
		t.Fatal("cancellation leaked a model input")
	}

}

// checkTaskSnapshot 只固化少量可独立检出展示风险的代表布局。
func checkTaskSnapshot(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name+".golden")
	if os.Getenv("EASYCODE_UPDATE_TASK_SNAPSHOTS") == "1" {
		if err := os.WriteFile(path, []byte(got), 0600); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil || got != string(want) {
		t.Fatal("task snapshot differs", name, err, got)
	}
}

func TestTasksAndFilesShareHeightAndModalFocus(t *testing.T) {
	m := taskTestObserve(t, taskTestModel(t), taskTestState(t, 90, 20, 17, false))
	files := &fileQueryFixture{results: make(chan domain.FileSearchSnapshot, 1)}
	m = m.WithFileQueries(files)
	m.composer = composerWithText(t, "inspect @sr\nnext draft row\n第三行草稿")
	m.composer.moveTo(len("inspect @sr"))
	m.inputs = []inputPreview{{id: "01900000-0000-7000-8000-000000000010", text: "queued input"}}
	m.effortNotice = []string{"effort selection"}
	m = updateModel(t, m, tea.WindowSizeMsg{Width: 40, Height: 12})
	identity := m.files.identity
	m = fileReply(t, m, identity, 1, []string{"src/a.go", "src/b.go", "src/c.go", "src/d.go", "src/e.go"}, domain.FileSearchComplete, domain.FileDiscoveryReport{})
	m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyDown})
	before := m.composer
	for _, action := range []tea.Msg{tea.KeyMsg{Type: tea.KeyCtrlT}, RuntimeEventMsg{Event: taskTestEvent(t, taskTestState(t, 91, 20, 18, false))}} {
		m = updateModel(t, m, action)
		shared := m.auxiliaryLayout(m.candidateRows())
		if m.files.rows == 0 || m.completion.rows != 0 || m.files.identity != identity || shared.effortRows+len(shared.taskLines)+m.candidateRows() > shared.space || len(shared.taskLines) > 1 {
			t.Fatal("task update displaced the active file candidate")
		}
		if !reflect.DeepEqual(m.composer, before) || !strings.Contains(m.View(), "> @src/b.go [file]") {
			t.Fatal("file selection or draft was lost")
		}
		taskViewBounds(t, m.View(), 40, 12)
	}
	checkTaskSnapshot(t, "tasks_file_pressure_40x12", m.View())
	m, _ = taskTestQuery(t, m, true)
	m.prepareCompletion()
	if m.files.active || m.files.rows != 0 || len(files.cancels) != 1 {
		t.Fatal("task panel did not cancel the file session")
	}
	m = fileReply(t, m, identity, 2, []string{"src/stale.go"}, domain.FileSearchComplete, domain.FileDiscoveryReport{})
	if !m.tasks.open || m.files.rows != 0 || m.files.ready {
		t.Fatal("late file result stole task panel focus")
	}
	m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.tasks.open || !reflect.DeepEqual(m.composer, before) {
		t.Fatal("task panel lost the file draft on return")
	}
}
