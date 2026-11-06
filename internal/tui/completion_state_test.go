package tui

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"easycode/internal/protocol"
	tea "github.com/charmbracelet/bubbletea"
)

func TestCompletionAcceptDismissAndSingleDispatch(t *testing.T) {
	for _, key := range []tea.KeyType{tea.KeyTab, tea.KeyEnter} {
		m := completionModel(t, "$rev")
		m, cmd := updateModelWithCommand(t, m, tea.KeyMsg{Type: key})
		if cmd != nil || m.composer.text != "$review " || m.completion.rows != 0 || m.pending != nil {
			t.Fatal("Skill acceptance executed", key)
		}
		m, cmd = updateModelWithCommand(t, m, tea.KeyMsg{Type: tea.KeyEnter})
		if cmd == nil || m.pending == nil {
			t.Fatal("independent Enter did not submit")
		}
		cmd()
		s := m.session.(*fakeChatSession)
		if !reflect.DeepEqual(s.submits, []string{"$review "}) {
			t.Fatal(s.submits)
		}
	}
	for _, name := range []string{"cache", "context", "compact"} {
		s := &cacheFixtureSession{}
		m := NewModel(context.Background(), "test", s)
		m.composer.replace(0, 0, "/"+name)
		m, cmd := updateModelWithCommand(t, m, tea.KeyMsg{Type: tea.KeyEnter})
		if cmd == nil || m.pending == nil || len(s.submits) != 0 {
			t.Fatal("command did not share dispatch", name)
		}
		cmd()
		if name == "cache" && len(s.queries) != 1 {
			t.Fatal("cache was not queried exactly once")
		}
	}
	for _, command := range []string{"/context bad", "/cache bad", "/skills bad", "/hooks bad"} {
		m := completionModel(t, command)
		m.composer.moveTo(strings.IndexByte(command, ' '))
		m.prepareCompletion()
		m, cmd := updateModelWithCommand(t, m, tea.KeyMsg{Type: tea.KeyEnter})
		if cmd != nil || !strings.Contains(m.errorSummary, "does not accept arguments") || m.composer.text != command {
			t.Fatalf("argument validation lost: %s %s", command, m.errorSummary)
		}
	}
}

func TestCompletionEscapeQueueAndDirectStop(t *testing.T) {
	m := completionModel(t, "/con")
	m.activeTurnID = "01900000-0000-7000-8000-000000000001"
	m.state = stateStreaming
	m.inputs = []inputPreview{{id: "01900000-0000-7000-8000-000000000002", text: "queued"}}
	draft := m.composer
	m, cmd := updateModelWithCommand(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if cmd != nil || m.completion.rows != 0 || m.cancelling || !reflect.DeepEqual(draft, m.composer) || m.retracting != nil {
		t.Fatal("first escape affected execution")
	}
	delta, _ := protocol.NewAssistantTextDelta("output")
	delta.TurnID = m.activeTurnID
	m = updateModel(t, m, RuntimeEventMsg{Event: delta})
	m = updateModel(t, m, tea.WindowSizeMsg{Width: 28, Height: 10})
	if m.completion.rows != 0 {
		t.Fatal("stream or resize reopened dismissal")
	}
	m, cmd = updateModelWithCommand(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil || m.retracting == nil || m.cancelling {
		t.Fatal("second escape did not retract")
	}
	for _, compaction := range []bool{false, true} {
		s := &contextFixtureSession{}
		x := completionModel(t, "/con")
		x.session = s
		if compaction {
			x.activeCompactionID = "01900000-0000-7000-8000-000000000003"
		} else {
			x.activeTurnID = m.activeTurnID
		}
		x, stop := updateModelWithCommand(t, x, tea.KeyMsg{Type: tea.KeyCtrlC})
		if stop == nil || x.completion.rows != 0 {
			t.Fatal("control C swallowed or left choices")
		}
		stop()
		if compaction && s.cancelled != x.activeCompactionID || !compaction && s.interrupts != 1 {
			t.Fatal("wrong stop target")
		}
	}
}

func TestCompletionSuppressionReopenAndPaste(t *testing.T) {
	m := completionModel(t, "/con")
	m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyTab})
	if m.completion.rows == 0 || m.composer.text != "/con" {
		t.Fatal("explicit Tab did not only reopen")
	}
	m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyTab})
	if m.composer.text != "/context " {
		t.Fatal("second Tab did not fill")
	}
	for _, kind := range []tea.KeyType{tea.KeyEnter, tea.KeyEsc, tea.KeyCtrlC, tea.KeyTab} {
		x := completionModel(t, "/con")
		x = updateModel(t, x, tea.KeyMsg{Type: kind, Runes: []rune("text\r\n\t\x1b\x03"), Paste: true})
		if x.composer.text != "/context\n\t\x1b\x03" || x.pending != nil || x.cancelling || x.completion.rows != 0 {
			t.Fatalf("paste interpreted keys: %q", x.composer.text)
		}
	}
	x := completionModel(t, "")
	x = updateModel(t, x, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("$rev"), Paste: true})
	if x.completion.rows != 0 {
		t.Fatal("paste opened suggestions")
	}
	x, cmd := updateModelWithCommand(t, x, tea.KeyMsg{Type: tea.KeyEnter})
	cmd()
	if got := x.session.(*fakeChatSession).submits; !reflect.DeepEqual(got, []string{"$rev"}) {
		t.Fatal("paste Enter selected a similar Skill", got)
	}
	x = completionModel(t, "$rev")
	x = updateModel(t, x, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(" args"), Paste: true})
	x.composer.moveTo(4)
	x = updateModel(t, x, struct{}{})
	if x.completion.rows != 0 {
		t.Fatal("moving back into a pasted first token reopened completion")
	}
	x = updateModel(t, x, tea.KeyMsg{Type: tea.KeyTab})
	if x.completion.rows == 0 || x.composer.text != "$rev args" {
		t.Fatal("explicit reopen changed pasted arguments")
	}
}

func TestCompletionBuiltinBrowsersPreservePartialDraft(t *testing.T) {
	for _, name := range []string{"/sk", "/ho"} {
		s := &fakeHookSession{}
		m := NewModel(context.Background(), "test", s)
		if err := m.composer.replace(0, 0, name); err != nil {
			t.Fatal(err)
		}
		before := m.composer
		m, cmd := updateModelWithCommand(t, m, tea.KeyMsg{Type: tea.KeyEnter})
		if cmd != nil || (!m.skillsOpen && !m.hooks.open) {
			t.Fatal("browser command did not open", name)
		}
		m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyEsc})
		if !reflect.DeepEqual(before, m.composer) || len(s.submits) != 0 || m.completion.rows != 0 {
			t.Fatal("browser completion changed original draft", name)
		}
	}
}

func TestCompletionNewlineRecallAndUnknownInput(t *testing.T) {
	for _, key := range []tea.KeyMsg{{Type: tea.KeyEnter, Alt: true}, {Type: tea.KeyCtrlJ}} {
		m := completionModel(t, "/con")
		m, cmd := updateModelWithCommand(t, m, key)
		if cmd != nil || m.composer.text != "/con\n" || m.completion.rows != 0 {
			t.Fatal("newline accepted choice")
		}
	}
	m := completionModel(t, "/con\\")
	m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.composer.text != "/con\n" || m.pending != nil {
		t.Fatal("backslash newline submitted")
	}
	m = completionModel(t, "")
	m.composer.history.add("$rev")
	m.composer.history.add("other")
	m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyUp})
	m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyUp})
	if !m.composer.history.browsing || m.completion.rows == 0 {
		t.Fatal("recall did not offer completion")
	}
	before := m.composer
	m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyDown})
	if !reflect.DeepEqual(before, m.composer) {
		t.Fatal("candidate movement changed recalled draft")
	}
	m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyTab})
	if m.composer.history.browsing || m.composer.text != "$review " {
		t.Fatal("completion did not adopt recall edit")
	}
	for _, text := range []string{"/tmp/file", "/zzzz", "plain $review"} {
		x := completionModel(t, text)
		x, cmd := updateModelWithCommand(t, x, tea.KeyMsg{Type: tea.KeyEnter})
		cmd()
		if got := x.session.(*fakeChatSession).submits; !reflect.DeepEqual(got, []string{text}) {
			t.Fatal(got)
		}
	}
}

func TestCompletionEveryExclusiveFocusPreservesDraft(t *testing.T) {
	setters := map[string]func(*Model){
		"question":   func(m *Model) { m.question = &questionOverlay{} },
		"trust":      func(m *Model) { m.hookTrust = &hookTrustOverlay{} },
		"hooks":      func(m *Model) { m.hooks.open = true },
		"skills":     func(m *Model) { m.skillsOpen = true },
		"patch":      func(m *Model) { m.approval = &approvalOverlay{} },
		"cache":      func(m *Model) { m.cachePanel = &protocol.CacheSnapshotPayload{} },
		"context":    func(m *Model) { m.contextPanel = &protocol.ContextSnapshotPayload{} },
		"transcript": func(m *Model) { m.detailOpen = true },
		"pending":    func(m *Model) { c := m.composer; m.pending = &c },
		"retract":    func(m *Model) { m.retracting = &inputRetraction{} },
		"cancel":     func(m *Model) { m.cancelling = true },
	}
	for name, set := range setters {
		t.Run(name, func(t *testing.T) {
			m := completionModel(t, "$rev")
			before := m.composer
			set(&m)
			m.prepareCompletion()
			if m.completion.rows != 0 || !reflect.DeepEqual(before, m.composer) {
				t.Fatal("exclusive focus left completion or edited draft")
			}
			// 清除独占投影，模拟它的正常关闭；未编辑时不能自动重开候选。
			m.question = nil
			m.hookTrust = nil
			m.hooks.open = false
			m.skillsOpen = false
			m.approval = nil
			m.cachePanel = nil
			m.contextPanel = nil
			m.detailOpen = false
			m.pending = nil
			m.retracting = nil
			m.cancelling = false
			m.prepareCompletion()
			if m.completion.rows != 0 {
				t.Fatal("modal return reopened completion")
			}
			m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyTab})
			if m.completion.rows == 0 || m.composer.text != before.text {
				t.Fatal("could not explicitly reopen")
			}
		})
	}
}
