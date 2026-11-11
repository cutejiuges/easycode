package tui

import (
	"context"
	"os"
	"path/filepath"
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
	p := protocol.HookTrustRequestedPayload{RequestID: "01900000-0000-7000-8000-000000000011", InvocationID: "01900000-0000-7000-8000-000000000012", Capability: domain.HookPatch, Declaration: hookUIEntries(t)[0].Declaration}
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
	p.Declaration, err = domain.NewHookDeclaration(d.Reference.Source, event, domain.HookFeedbackModel, d.Reference.Revision, d.Reference.ID, d.Matcher, d.Command, 5)
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
func hookSnapshotModel(t *testing.T, name string) Model {
	t.Helper()
	s := &fakeHookSession{entries: hookUIEntries(t)}
	if strings.Contains(name, "post") || strings.Contains(name, "failure") {
		event := domain.HookPostToolUse
		if strings.Contains(name, "failure") {
			event = domain.HookPostToolUseFailure
		}
		d := s.entries[0].Declaration
		d, err := domain.NewHookDeclaration(d.Reference.Source, event, domain.HookFeedbackModel, d.Reference.Revision, d.Reference.ID, d.Matcher, d.Command, 5)
		if err != nil {
			t.Fatal(err)
		}
		s.entries[0].Declaration = d
	}
	m := NewModel(context.Background(), "test", s).WithHooks(s.entries)
	width := 80
	if strings.Contains(name, "narrow") {
		width = 28
	}
	m = updateModel(t, m, tea.WindowSizeMsg{Width: width, Height: 24})
	if strings.Contains(name, "trust") {
		m.activeTurnID = "01900000-0000-7000-8000-000000000001"
		if strings.Contains(name, "long") {
			d := m.hooks.entries[0].Declaration
			d.Command = strings.Repeat("printf 'review every line'\n", 50)
			d, _ = domain.NewHookDeclaration(d.Reference.Source, domain.HookPreToolUse, domain.HookFeedbackNone, d.Reference.Revision, d.Reference.ID, d.Matcher, d.Command, 5)
			s.entries[0].Declaration = d
		}
		e := hookUIRequest(t, m)
		if strings.Contains(name, "long") || strings.Contains(name, "post") || strings.Contains(name, "failure") {
			p, _ := protocol.DecodeHookTrustRequested(e)
			p.Declaration = s.entries[0].Declaration
			e, _ = protocol.NewHookTrustRequested(p)
			e.TurnID = m.activeTurnID
		}
		m = updateModel(t, m, RuntimeEventMsg{Event: e})
	} else {
		if name == "hooks_empty" {
			s.entries = nil
			m = m.WithHooks(nil)
		}
		if name == "hooks_invalid" {
			s.entries = append(s.entries, domain.HookListEntry{State: "invalid", Code: domain.HookConfigInvalid, Declaration: domain.HookDeclaration{Reference: domain.HookReference{Source: domain.HookSourceUser}}})
		}
		m.composer.replace(0, 0, "/hooks")
		updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		m = updated.(Model)
		if cmd != nil || len(s.submits) != 0 {
			t.Fatal("browser caused I/O")
		}
		if name == "hooks_detail" || name == "hooks_post_detail" || name == "hooks_failure_detail" {
			m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyEnter})
		}
	}
	return m
}
func TestHookSnapshots(t *testing.T) {
	for _, name := range []string{"hooks", "hooks_empty", "hooks_invalid", "hooks_detail", "hooks_narrow", "hooks_trust", "hooks_trust_narrow", "hooks_trust_long", "hooks_post_detail", "hooks_post_narrow", "hooks_post_trust", "hooks_post_trust_narrow", "hooks_failure", "hooks_failure_detail", "hooks_failure_trust", "hooks_failure_trust_narrow"} {
		t.Run(name, func(t *testing.T) {
			m := hookSnapshotModel(t, name)
			want, err := os.ReadFile(filepath.Join("testdata", name+".golden"))
			if err != nil {
				t.Fatal(err)
			}
			if m.View()+"\n" != string(want) {
				t.Fatalf("snapshot mismatch:\n%s", m.View())
			}
		})
	}
}
func TestHookBrowserResetPreservesDraftAndNeverGrants(t *testing.T) {
	s := &fakeHookSession{entries: hookUIEntries(t)}
	s.entries[0].State = "trusted"
	m := NewModel(context.Background(), "test", s).WithHooks(s.entries)
	m.composer.replace(0, 0, "/hooks")
	m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("reset not routed")
	}
	m = updateModel(t, m, cmd())
	if s.resets != 1 || m.hooks.entries[0].State != "untrusted" || len(s.decisions) != 0 || len(s.submits) != 0 {
		t.Fatal("reset granted or submitted")
	}
	m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.hooks.open || m.composer.text != "/hooks" {
		t.Fatal("browser lost draft")
	}
	m.hooks.open = true
	m.hooks.detail = true
	m.activeTurnID = "01900000-0000-7000-8000-000000000001"
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	if cmd != nil {
		t.Fatal("active reset allowed")
	}
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

func TestHookOverlayMutualExclusionAndCheckingProjection(t *testing.T) {
	for _, event := range []domain.HookEvent{domain.HookPreToolUse, domain.HookPostToolUse} {
		t.Run(string(event), func(t *testing.T) { testHookOverlayMutualExclusionAndCheckingProjection(t, event) })
	}
}
func testHookOverlayMutualExclusionAndCheckingProjection(t *testing.T, event domain.HookEvent) {
	s := &fakeHookSession{entries: hookUIEntries(t)}
	m := NewModel(context.Background(), "test", s)
	m.activeTurnID = "01900000-0000-7000-8000-000000000001"
	approval := mustApprovalRequested(t)
	approval.TurnID = m.activeTurnID
	m = updateModel(t, m, RuntimeEventMsg{Event: approval})
	next, cmd := m.Update(RuntimeEventMsg{Event: hookUIRequestForEvent(t, m, event)})
	m = next.(Model)
	if cmd == nil || m.hookTrust != nil || m.approval == nil {
		t.Fatal("overlapping overlays accepted")
	}
	m.approval = nil
	m = updateModel(t, m, RuntimeEventMsg{Event: hookUIRequestForEvent(t, m, event)})
	next, cmd = m.Update(RuntimeEventMsg{Event: approval})
	m = next.(Model)
	if cmd == nil || m.hookTrust == nil || m.approval != nil {
		t.Fatal("tool approval bypassed hook overlay")
	}
	m.hookTrust = nil
	id := "01900000-0000-7000-8000-000000000012"
	payload := protocol.ToolActivityPayload{InvocationID: id, Tool: protocol.ToolWrite, Summary: "marker", Status: "waiting"}
	ready, err := protocol.NewToolReady(payload)
	if err != nil {
		t.Fatal(err)
	}
	ready.TurnID = m.activeTurnID
	m = updateModel(t, m, RuntimeEventMsg{Event: ready})
	h := protocol.HookActivityPayload{InvocationID: id, Reference: s.entries[0].Declaration.Reference, State: "checking", Code: domain.HookOK}
	e, err := protocol.NewHookActivity(h)
	if err != nil {
		t.Fatal(err)
	}
	e.TurnID = m.activeTurnID
	m = updateModel(t, m, RuntimeEventMsg{Event: e})
	if !strings.Contains(m.View(), "check checking") || strings.Contains(m.View(), "running") {
		t.Fatal("hook masqueraded as running tool", m.View())
	}
	old := m.activeTurnID
	m.activeTurnID = "01900000-0000-7000-8000-000000000099"
	h.State = "denied"
	h.Code = domain.HookDenied
	e, _ = protocol.NewHookActivity(h)
	e.TurnID = old
	m = updateModel(t, m, RuntimeEventMsg{Event: e})
	if m.transcript[0].hook.State != "checking" {
		t.Fatal("stale hook activity changed next turn")
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
