package tui

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/protocol"
	tea "github.com/charmbracelet/bubbletea"
)

func stopSnapshotModel(t *testing.T, width, height int, screen string) Model {
	t.Helper()
	d, err := domain.NewStopHookDeclaration(domain.HookSourceProject, domain.HookFeedbackModel, domain.HookDigest(nil), "completion", "cat >/dev/null\nprintf '%s' '{\"decision\":\"block\",\"reason\":\"Verify the result.\"}'", 5)
	if err != nil {
		t.Fatal(err)
	}
	s := &fakeHookSession{entries: []domain.HookListEntry{{Declaration: d, State: "untrusted", Code: domain.HookOK}}}
	m := NewModel(context.Background(), "test", s).WithHooks(s.entries)
	m = updateModel(t, m, tea.WindowSizeMsg{Width: width, Height: height})
	if screen == "list" || screen == "details" {
		m.hooks.open = true
		m.hooks.detail = screen == "details"
		return m
	}
	m.activeTurnID = "01900000-0000-7000-8000-000000000001"
	plan, _ := domain.NewStopChecksPlan(domain.AssistantSampleReference{TurnID: m.activeTurnID}, 0, []domain.HookReference{d.Reference})
	request := protocol.HookTrustRequestedPayload{RequestID: "01900000-0000-7000-8000-000000000002", Target: plan.Target(), PlanDigest: plan.Digest(), Declaration: d}
	event, err := protocol.NewHookTrustRequested(request)
	if err != nil {
		t.Fatal(err)
	}
	event.TurnID = m.activeTurnID
	return updateModel(t, m, RuntimeEventMsg{Event: event})
}
func TestStopHookSnapshots(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	for _, size := range [][2]int{{80, 24}, {100, 30}, {40, 16}} {
		for _, screen := range []string{"list", "details", "trust"} {
			name := fmt.Sprintf("stop_hooks_%dx%d_%s", size[0], size[1], screen)
			t.Run(name, func(t *testing.T) {
				m := stopSnapshotModel(t, size[0], size[1], screen)
				want, err := os.ReadFile("testdata/" + name + ".golden")
				if err != nil {
					t.Fatal(err)
				}
				if m.View()+"\n" != string(want) {
					t.Fatalf("snapshot mismatch:\n%s", m.View())
				}
				if strings.Contains(m.View(), "Matcher:") {
					t.Fatal("Stop displayed a tool matcher")
				}
			})
		}
	}
}
func TestStopTrustDefaultDenyPasteAndPhaseOrdering(t *testing.T) {
	m := stopSnapshotModel(t, 80, 24, "trust")
	m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("\n"), Paste: true})
	if m.hookTrust.trust || m.hookTrust.submitted {
		t.Fatal("paste granted trust")
	}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("no default decision")
	}
	_ = cmd()
	s := m.session.(*fakeHookSession)
	if len(s.decisions) != 1 || s.decisions[0] != protocol.HookTrustDeny {
		t.Fatal("Enter did not deny")
	}
	m.hookTrust = nil
	p := protocol.StopHookCommittedPayload{Target: m.stopTestTarget(t), PlanDigest: domain.HookDigest(nil), DetailSequence: 10, Phase: protocol.StopHookPlanned}
	for _, step := range []struct {
		phase       protocol.StopHookPhase
		disposition domain.StopDisposition
		status      string
	}{{protocol.StopHookPlanned, "", "Checking completion"}, {protocol.StopHookDecided, domain.StopContinued, "Preparing continuation"}, {protocol.StopHookDelivered, domain.StopContinued, "Continuing after completion check"}} {
		p.Phase, p.Disposition = step.phase, step.disposition
		p.DetailSequence++
		event, _ := protocol.NewStopHookCommitted(p)
		event.TurnID = m.activeTurnID
		m = updateModel(t, m, RuntimeEventMsg{Event: event})
		if m.stopStatus != step.status || !m.activeTurnID.Valid() {
			t.Fatal("wrong completion state", m.stopStatus)
		}
	}
	old := p
	old.DetailSequence--
	old.Phase = protocol.StopHookPlanned
	old.Disposition = ""
	event, _ := protocol.NewStopHookCommitted(old)
	event.TurnID = m.activeTurnID
	m = updateModel(t, m, RuntimeEventMsg{Event: event})
	if m.stopStatus != "Continuing after completion check" {
		t.Fatal("late result overwrote state")
	}
}
func (m Model) stopTestTarget(t *testing.T) domain.HookTarget {
	t.Helper()
	target, err := domain.NewStopHookTarget(domain.AssistantSampleReference{TurnID: m.activeTurnID})
	if err != nil {
		t.Fatal(err)
	}
	return target
}
