package tui

import (
	"context"
	"strings"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/protocol"
	tea "github.com/charmbracelet/bubbletea"
)

type fakeHookSession struct {
	fakeChatSession
	entries   []domain.HookListEntry
	decisions []protocol.HookTrustDecision
	resets    int
}

func (s *fakeHookSession) HookEntries() []domain.HookListEntry {
	return append([]domain.HookListEntry(nil), s.entries...)
}
func (s *fakeHookSession) ResolveHookTrust(_ context.Context, _ domain.TurnID, _ protocol.HookTrustRequestedPayload, d protocol.HookTrustDecision) error {
	s.decisions = append(s.decisions, d)
	return nil
}
func (s *fakeHookSession) ResetHookTrust(_ context.Context, ref domain.HookReference) error {
	s.resets++
	for i := range s.entries {
		if s.entries[i].Declaration.Reference == ref {
			s.entries[i].State = "untrusted"
		}
	}
	return nil
}
func hookUIEntries(t *testing.T) []domain.HookListEntry {
	t.Helper()
	d, err := domain.NewHookDeclaration(domain.HookSourceProject, domain.HookPreToolUse, domain.HookFeedbackNone, strings.Repeat("a", 64), "check", 7, "cat >/dev/null\nprintf '{}'", 5)
	if err != nil {
		t.Fatal(err)
	}
	return []domain.HookListEntry{{Declaration: d, State: "untrusted", Code: domain.HookOK}}
}
func hookUIRequest(t *testing.T, model Model) protocol.Event {
	t.Helper()
	p := protocol.HookTrustRequestedPayload{RequestID: "01900000-0000-7000-8000-000000000011", Target: testHookTarget(t, "01900000-0000-7000-8000-000000000012", domain.HookPatch), PlanDigest: strings.Repeat("b", 64), Declaration: hookUIEntries(t)[0].Declaration}
	e, err := protocol.NewHookTrustRequested(p)
	if err != nil {
		t.Fatal(err)
	}
	e.TurnID = model.activeTurnID
	return e
}
func hookUIRequestForEvent(t *testing.T, model Model, event domain.HookEvent) protocol.Event {
	t.Helper()
	e := hookUIRequest(t, model)
	if event == domain.HookPreToolUse {
		return e
	}
	p, _ := protocol.DecodeHookTrustRequested(e)
	d := p.Declaration
	var err error
	p.Declaration, err = domain.NewHookDeclaration(d.Reference.Source, event, domain.HookFeedbackModel, d.Reference.Revision, d.Reference.ID, testToolHookMatcher(t, d), d.Command, 5)
	if err != nil {
		t.Fatal(err)
	}
	e, err = protocol.NewHookTrustRequested(p)
	if err != nil {
		t.Fatal(err)
	}
	e.TurnID = model.activeTurnID
	return e
}
func TestHookTrustDefaultDenyInputIsolationAndCancellation(t *testing.T) {
	for _, event := range []domain.HookEvent{domain.HookPreToolUse, domain.HookPostToolUse} {
		t.Run(string(event), func(t *testing.T) { testHookTrustDefaultDenyInputIsolationAndCancellation(t, event) })
	}
}
func testHookTrustDefaultDenyInputIsolationAndCancellation(t *testing.T, event domain.HookEvent) {
	for _, key := range []tea.KeyType{tea.KeyEnter, tea.KeyEsc} {
		s := &fakeHookSession{entries: hookUIEntries(t)}
		m := NewModel(context.Background(), "test", s)
		m.activeTurnID = "01900000-0000-7000-8000-000000000001"
		m.composer.replace(0, 0, "keep draft")
		m = updateModel(t, m, RuntimeEventMsg{Event: hookUIRequestForEvent(t, m, event)})
		m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/hooks")})
		m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyEnter, Paste: true})
		if m.composer.text != "keep draft" || m.hookTrust.trust || m.hookTrust.submitted || m.hooks.open {
			t.Fatal("overlay key escaped")
		}
		updated, cmd := m.Update(tea.KeyMsg{Type: key})
		m = updated.(Model)
		if cmd == nil {
			t.Fatal("no deny command")
		}
		cmd()
		if len(s.decisions) != 1 || s.decisions[0] != protocol.HookTrustDeny {
			t.Fatal("default granted")
		}
		updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
		m = updated.(Model)
		if cmd == nil || !m.cancelling {
			t.Fatal("submitted overlay swallowed cancel")
		}
		cmd()
		if s.interrupts != 1 || m.hookTrust == nil {
			t.Fatal("cleanup ownership lost")
		}
		old := m.hookTrust.request
		m = updateModel(t, m, hookTrustFailedMsg{request: "stale", turn: m.activeTurnID})
		if m.hookTrust.request != old {
			t.Fatal("stale response changed overlay")
		}
	}
}

func TestHookAndQuestionOverlaysRemainMutuallyExclusive(t *testing.T) {
	for _, first := range []string{"question", "hook"} {
		t.Run(first, func(t *testing.T) {
			session := &fakeHookSession{entries: hookUIEntries(t)}
			model := NewModel(context.Background(), "test", session)
			model.activeTurnID = "01900000-0000-7000-8000-000000000003"
			model.composer.replace(0, 0, "preserve draft")
			question := questionUIEvent(t, 1)
			hook := hookUIRequest(t, model)
			one, two := question, hook
			if first == "hook" {
				one, two = hook, question
			}
			model = updateModel(t, model, RuntimeEventMsg{Event: one})
			updated, cmd := model.Update(RuntimeEventMsg{Event: two})
			model = updated.(Model)
			if cmd == nil || (model.question != nil) == (model.hookTrust != nil) || model.composer.text != "preserve draft" {
				t.Fatal("overlapping question and hook overlays accepted")
			}
		})
	}
}

// testToolHookMatcher 保证原工具声明测试不混入回合检查范围。
func testToolHookMatcher(t *testing.T, d domain.HookDeclaration) domain.HookMatcher {
	t.Helper()
	m, ok := d.Scope.ToolMatcher()
	if !ok {
		t.Fatal("expected tool hook scope")
	}
	return m
}

// testHookTarget 创建严格的真实工具目标，避免测试复用旧身份形状。
func testHookTarget(t *testing.T, id string, cap domain.HookCapability) domain.HookTarget {
	t.Helper()
	target, err := domain.NewToolHookTarget(id, cap)
	if err != nil {
		t.Fatal(err)
	}
	return target
}
