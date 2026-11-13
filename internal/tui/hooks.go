package tui

import (
	"context"
	"fmt"
	"strings"

	"easycode/internal/domain"
	"easycode/internal/protocol"
	tea "github.com/charmbracelet/bubbletea"
)

type hookSession interface {
	HookEntries() []domain.HookListEntry
	ResolveHookTrust(context.Context, domain.TurnID, protocol.HookTrustRequestedPayload, protocol.HookTrustDecision) error
	ResetHookTrust(context.Context, domain.HookReference) error
}
type hookBrowser struct {
	entries          []domain.HookListEntry
	open, detail     bool
	selected, scroll int
	generation       uint64
}
type hookTrustOverlay struct {
	request          protocol.HookTrustRequestedPayload
	turn             domain.TurnID
	trust, submitted bool
	scroll           int
}
type hookTrustFailedMsg struct {
	request string
	turn    domain.TurnID
	err     error
}
type hookResetMsg struct {
	generation uint64
	entries    []domain.HookListEntry
	err        error
}

// WithHooks 接收已完成来源读取的快照，不在构造或 View 执行 I/O。
func (m Model) WithHooks(entries []domain.HookListEntry) Model {
	m.hooks.entries = append([]domain.HookListEntry(nil), entries...)
	return m
}
func (m Model) openHooks() (tea.Model, tea.Cmd) {
	m.hooks.open = true
	m.hooks.detail = false
	m.hooks.selected = 0
	m.hooks.scroll = 0
	m.hooks.generation++
	if target, ok := m.session.(hookSession); ok {
		m.hooks.entries = target.HookEntries()
	}
	return m, nil
}
func (m Model) handleHooksKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if key.Paste {
		return m, nil
	}
	switch key.Type {
	case tea.KeyEsc:
		if m.hooks.detail {
			m.hooks.detail = false
			m.hooks.scroll = 0
		} else {
			m.hooks.open = false
			m.hooks.generation++
		}
	case tea.KeyCtrlC:
		m.hooks.open = false
		m.hooks.generation++
		if m.activeTurnID.Valid() {
			m.cancelling = true
			return m, interruptSession(m.session)
		}
	case tea.KeyEnter:
		if len(m.hooks.entries) > 0 {
			m.hooks.detail = true
			m.hooks.scroll = 0
		}
	case tea.KeyUp:
		if m.hooks.detail {
			m.hooks.scroll = max(0, m.hooks.scroll-1)
		} else {
			m.hooks.selected = max(0, m.hooks.selected-1)
		}
	case tea.KeyDown:
		if m.hooks.detail {
			m.hooks.scroll++
		} else {
			m.hooks.selected = min(max(0, len(m.hooks.entries)-1), m.hooks.selected+1)
		}
	case tea.KeyPgUp:
		m.hooks.scroll = max(0, m.hooks.scroll-max(1, m.heightOrDefault()-3))
	case tea.KeyPgDown:
		m.hooks.scroll += max(1, m.heightOrDefault()-3)
	case tea.KeyRunes:
		if string(key.Runes) == "r" && m.hooks.detail && !m.activeTurnID.Valid() && len(m.hooks.entries) > 0 {
			entry := m.hooks.entries[m.hooks.selected]
			if entry.Declaration.Validate() != nil || entry.State == "unsupported" {
				return m, nil
			}
			target, ok := m.session.(hookSession)
			if !ok {
				return m, nil
			}
			generation := m.hooks.generation
			ref := entry.Declaration.Reference
			return m, func() tea.Msg {
				err := target.ResetHookTrust(context.Background(), ref)
				var entries []domain.HookListEntry
				if err == nil {
					entries = target.HookEntries()
				}
				return hookResetMsg{generation, entries, err}
			}
		}
	}
	return m, nil
}
func hookPanel(lines []string, width, height, scroll int) string {
	var wrapped []string
	for _, line := range lines {
		wrapped = append(wrapped, wrapApprovalDiff(commandDisplayText(line), max(1, width))...)
	}
	scroll = min(max(0, scroll), max(0, len(wrapped)-height))
	return strings.Join(wrapped[scroll:min(len(wrapped), scroll+max(1, height))], "\n")
}
func (m Model) renderHooks() string {
	lines := []string{"Hooks · Local checks", "Enter: details · Esc: back"}
	if m.hooks.detail && len(m.hooks.entries) > 0 {
		e := m.hooks.entries[m.hooks.selected]
		d := e.Declaration
		lines = []string{"Hook details · Esc: back · ↑/↓: scroll · r: reset trust", fmt.Sprintf("Source: %s · %s [%s]", d.Reference.Source, d.Reference.ID, e.State), "Event: " + string(d.Reference.Event) + " · feedback: " + string(d.Reference.Feedback), hookScopeLabel(d), fmt.Sprintf("Timeout: %d seconds", d.Reference.TimeoutSeconds), "Workspace: . · read-only · network denied", "Workspace scripts can change after review.", "Reset only removes trust; the next matching check asks again.", "Raw output is discarded. Only trusted model feedback is sent and saved.", "", "Command:", d.Command}
		if e.State == "invalid" || e.State == "unsupported" {
			lines = append(lines, "Unavailable: "+string(e.Code))
		}
		return hookPanel(lines, m.widthOrDefault(), m.heightOrDefault(), m.hooks.scroll)
	}
	if len(m.hooks.entries) == 0 {
		lines = append(lines, "", "No local hooks found.")
	}
	selectedRow := 0
	for i, e := range m.hooks.entries {
		marker := "  "
		if i == m.hooks.selected {
			marker = "> "
			selectedRow = len(lines)
		}
		if e.Declaration.Validate() != nil {
			lines = append(lines, fmt.Sprintf("%s%s [%s]", marker, e.Declaration.Reference.Source, e.State))
		} else {
			lines = append(lines, fmt.Sprintf("%s%s:%s · %s · %s [%s]", marker, e.Declaration.Reference.Source, e.Declaration.Reference.ID, e.Declaration.Reference.Event, e.Declaration.Reference.Feedback, e.State))
		}
		if e.Code != domain.HookOK {
			lines = append(lines, "  "+string(e.Code))
		}
	}
	scroll := m.hooks.scroll
	if selectedRow >= m.heightOrDefault() {
		scroll = max(scroll, selectedRow-m.heightOrDefault()+2)
	}
	return hookPanel(lines, m.widthOrDefault(), m.heightOrDefault(), scroll)
}
func (m Model) handleHookTrustKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if key.Paste {
		return m, nil
	}
	if key.Type == tea.KeyCtrlC {
		m.cancelling = true
		return m, interruptSession(m.session)
	}
	if m.hookTrust.submitted {
		return m, nil
	}
	switch key.Type {
	case tea.KeyTab, tea.KeyLeft, tea.KeyRight:
		m.hookTrust.trust = !m.hookTrust.trust
	case tea.KeyUp:
		m.hookTrust.trust = true
	case tea.KeyDown:
		m.hookTrust.trust = false
	case tea.KeyPgUp:
		m.hookTrust.scroll = max(0, m.hookTrust.scroll-max(1, m.heightOrDefault()-3))
	case tea.KeyPgDown:
		m.hookTrust.scroll += max(1, m.heightOrDefault()-3)
	case tea.KeyEsc:
		m.hookTrust.trust = false
		return m, m.submitHookTrust()
	case tea.KeyEnter:
		return m, m.submitHookTrust()
	}
	return m, nil
}
func (m *Model) submitHookTrust() tea.Cmd {
	o := m.hookTrust
	target, ok := m.session.(hookSession)
	if o == nil || !ok {
		m.errorSummary = "hook_trust_unavailable: interactive trust is unavailable"
		return nil
	}
	decision := protocol.HookTrustDeny
	if o.trust {
		decision = protocol.HookTrustSession
	}
	request, turn := o.request, o.turn
	o.submitted = true
	return func() tea.Msg {
		err := target.ResolveHookTrust(context.Background(), turn, request, decision)
		if err != nil {
			return hookTrustFailedMsg{request.RequestID, turn, err}
		}
		return nil
	}
}
func (m Model) renderHookTrust() string {
	o := m.hookTrust
	d := o.request.Declaration
	trust, deny := "  Trust for this session", "> Deny"
	if o.trust {
		trust, deny = "> Trust for this session", "  Deny"
	}
	lines := []string{"Trust this hook?", fmt.Sprintf("Source: %s · %s", d.Reference.Source, d.Reference.ID), string(d.Reference.Event) + ": " + d.Scope.String(), "Feedback: " + string(d.Reference.Feedback), fmt.Sprintf("Timeout: %d seconds", d.Reference.TimeoutSeconds), "Workspace: . · read-only · network denied", "This does not approve the model's tool call.", "Workspace scripts can change after review.", "PgUp/PgDn: full command · Tab/↑/↓: choose · Esc: deny", "", trust, deny, "", "Command:", d.Command}
	if d.Reference.Event == domain.HookStop {
		lines = append(lines[:1], append([]string{"Checks the saved answer before this turn completes.", "Earlier tool effects are retained; this check does not roll them back.", "feedback=model sends a blocking reason to the provider and saves it in Session."}, lines[1:]...)...)
	}
	if d.Reference.Event == domain.HookPostToolUse {
		lines = append(lines[:1], append([]string{"Runs only after the tool succeeds.", "feedback=model sends additionalContext to the provider and saves it in Session."}, lines[1:]...)...)
	}
	if d.Reference.Event == domain.HookPostToolUseFailure {
		lines = append(lines[:1], append([]string{"Runs only after a definite execution-attempt failure.", "Admission does not prove the process started. Normal nonzero exits use PostToolUse.", "feedback=model sends additionalContext to the provider and saves it in Session."}, lines[1:]...)...)
	}
	if o.submitted {
		lines = append(lines, "Waiting for confirmation")
	}
	if m.cancelling {
		lines = append(lines, "Cancelling — waiting for cleanup")
	}
	return hookPanel(lines, m.widthOrDefault(), m.heightOrDefault(), o.scroll)
}
func (m Model) projectHookEvent(e protocol.Event) (tea.Model, tea.Cmd) {
	if e.TurnID != m.activeTurnID {
		return m, nil
	}
	switch e.Kind {
	case protocol.EventStopHookCommitted:
		return m.projectStopHookCommit(e)
	case protocol.EventPostHookCommitted:
		return m.projectPostHookCommit(e)
	case protocol.EventHookTrustRequested:
		p, err := protocol.DecodeHookTrustRequested(e)
		if err != nil || m.approval != nil || m.question != nil || m.hookTrust != nil {
			m.errorSummary = "stream_protocol_error: invalid hook trust request"
			return m, interruptSession(m.session)
		}
		m.hookTrust = &hookTrustOverlay{request: p, turn: e.TurnID}
	case protocol.EventHookTrustResolved:
		p, err := protocol.DecodeHookTrustResolved(e)
		if err != nil {
			m.errorSummary = "stream_protocol_error: invalid hook trust resolution"
			return m, nil
		}
		if m.hookTrust == nil || p.RequestID != m.hookTrust.request.RequestID || p.Target != m.hookTrust.request.Target || p.PlanDigest != m.hookTrust.request.PlanDigest || p.Reference != m.hookTrust.request.Declaration.Reference {
			return m, nil
		}
		m.hookTrust = nil
		for i := range m.hooks.entries {
			if m.hooks.entries[i].Declaration.Reference == p.Reference {
				m.hooks.entries[i].State = "denied"
				if p.Decision == protocol.HookTrustSession {
					m.hooks.entries[i].State = "trusted"
				}
			}
		}
	case protocol.EventHookActivity:
		p, err := protocol.DecodeHookActivity(e)
		if err != nil {
			m.errorSummary = "stream_protocol_error: invalid hook activity"
			return m, nil
		}
		if p.Reference.Event == domain.HookStop {
			if m.stopCommit == nil || m.stopCommit.Target != p.Target || m.stopCommit.PlanDigest != p.PlanDigest {
				return m, nil
			}
			return m, nil
		}
		index, ok := m.toolItems[p.InvocationID()]
		if !ok || index >= len(m.transcript) {
			return m, nil
		}
		if m.toolClosed[p.InvocationID()] && p.Reference.Event == domain.HookPreToolUse {
			return m, nil
		}
		if p.State == "checking" && m.toolClosed[p.InvocationID()] {
			post, ok := m.postHooks[p.InvocationID()]
			if !ok || post.Phase != protocol.PostHookStarted {
				return m, nil
			}
		}
		if p.Reference.Event.Post() {
			post, exists := m.postHooks[p.InvocationID()]
			if !exists || post.PlanDigest != p.PlanDigest {
				return m, nil
			}
			if !m.toolClosed[p.InvocationID()] {
				return m, nil
			}
			activity := m.transcript[index].activity
			if activity == nil {
				return m, nil
			}
			if p.Reference.Event == domain.HookPostToolUse && activity.Status != "success" || p.Reference.Event == domain.HookPostToolUseFailure && activity.Status != "error" && activity.Status != "conflict" {
				return m, nil
			}
		}
		m.transcript[index].hook = &p
	}
	return m, nil
}

func hookScopeLabel(d domain.HookDeclaration) string {
	if d.Reference.Event == domain.HookStop {
		return "Scope: Turn completion"
	}
	return "Matcher: " + d.Scope.String()
}
