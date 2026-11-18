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
	for _, command := range []string{"/context bad", "/cache bad", "/skills bad", "/hooks bad", "/plugins bad"} {
		m := completionModel(t, command)
		m.composer.moveTo(strings.IndexByte(command, ' '))
		m.prepareCompletion()
		m, cmd := updateModelWithCommand(t, m, tea.KeyMsg{Type: tea.KeyEnter})
		if cmd != nil || !strings.Contains(m.errorSummary, "does not accept arguments") || m.composer.text != command {
			t.Fatalf("argument validation lost: %s %s", command, m.errorSummary)
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
