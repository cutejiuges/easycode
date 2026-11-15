package tui

import (
	"context"
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

type effortFixtureSession struct {
	contextFixtureSession
	choices  []domain.EffortChoice
	requests []protocol.RequestID
}

func (s *effortFixtureSession) QueryEffort(_ context.Context, id protocol.RequestID) (protocol.CommandResult, error) {
	s.requests = append(s.requests, id)
	cmd, _ := protocol.NewQueryEffortCommand(id)
	return protocol.NewAcceptedCommandResult(cmd, protocol.CommandDispositionQueried)
}
func (s *effortFixtureSession) SetEffort(_ context.Context, id protocol.RequestID, c domain.EffortChoice) (protocol.CommandResult, error) {
	s.requests = append(s.requests, id)
	s.choices = append(s.choices, c)
	cmd, _ := protocol.NewSetEffortCommand(id, c)
	return protocol.NewAcceptedCommandResult(cmd, protocol.CommandDispositionUpdated)
}

func TestEffortParserAndNonModalDispatch(t *testing.T) {
	for _, text := range []string{"/effort", " /effort STATUS ", "/effort\ncurrent", "/effort auto", "/effort UNSET", "/effort reset", "/effort HIGH", "/effort none", "/effort --HELP", "/effort -h"} {
		if _, local, err := parseEffortCommand(text); !local || err != nil {
			t.Fatal(text, err)
		}
	}
	for _, text := range []string{"/Effort high", "/effort-file", "/effort/high", "prefix /effort high"} {
		if _, local, _ := parseEffortCommand(text); local {
			t.Fatal("nonexact command captured", text)
		}
	}
	for _, text := range []string{"/effort high extra", "/effort unknown", "/effort 中文", "/effort HİGH"} {
		s := &effortFixtureSession{}
		m := NewModel(context.Background(), "test", s)
		m.composer.replace(0, 0, text)
		m.composer.moveTo(2)
		before := m.composer
		m, cmd := updateModelWithCommand(t, m, tea.KeyMsg{Type: tea.KeyEnter})
		if cmd != nil || m.errorSummary == "" || !reflect.DeepEqual(before, m.composer) || len(s.requests) != 0 {
			t.Fatal("invalid input modified draft", text)
		}
	}
	for _, text := range []string{"/effort help", "/effort -h", "/effort --help"} {
		s := &effortFixtureSession{}
		m := NewModel(context.Background(), "test", s)
		m.composer.replace(0, 0, text)
		m, cmd := updateModelWithCommand(t, m, tea.KeyMsg{Type: tea.KeyEnter})
		if cmd != nil || len(s.requests) != 0 || len(m.effortNotice) != 6 || m.contextPanel != nil || m.composer.text != "" {
			t.Fatal("help submitted control")
		}
	}
	s := &effortFixtureSession{}
	m := NewModel(context.Background(), "test", s)
	m.composer.replace(0, 0, "/effort low")
	m, cmd := updateModelWithCommand(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil || m.pending != nil {
		t.Fatal("effort became modal")
	}
	if msg := cmd(); msg != nil {
		t.Fatal("method success generated duplicate receipt")
	}
	if len(s.choices) != 1 || s.choices[0].Level() != "low" {
		t.Fatal("setting not submitted once")
	}
}

func effortAck(t *testing.T, m Model) Model {
	t.Helper()
	f := m.effortFeedback
	var cmd protocol.Command
	if f.kind == protocol.CommandQueryEffort {
		cmd, _ = protocol.NewQueryEffortCommand(f.requestID)
	} else {
		cmd, _ = protocol.NewSetEffortCommand(f.requestID, domain.NewAutomaticEffortChoice())
	}
	disposition := protocol.CommandDispositionUpdated
	if f.kind == protocol.CommandQueryEffort {
		disposition = protocol.CommandDispositionQueried
	}
	result, _ := protocol.NewAcceptedCommandResult(cmd, disposition)
	item, _ := protocol.NewCommandResultItem(result)
	next, _ := m.projectControlItem(item)
	return next.(Model)
}
func effortEvent(t *testing.T, m Model, id protocol.RequestID, p protocol.EffortSnapshotPayload) Model {
	t.Helper()
	event, err := protocol.NewEffortSnapshot("01900000-0000-7000-8000-000000000001", "01900000-0000-7000-8000-000000000002", p)
	if err != nil {
		t.Fatal(err)
	}
	item, err := protocol.NewCorrelatedContextItem(id, event)
	if err != nil {
		t.Fatal(err)
	}
	next, _ := m.projectControlItem(item)
	return next.(Model)
}
func TestEffortRevisionAndLocalFeedbackAreIndependent(t *testing.T) {
	s := &effortFixtureSession{}
	m := NewModel(context.Background(), "test", s)
	m.composer.replace(0, 0, "/effort status")
	m, _ = updateModelWithCommand(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	oldID := m.effortFeedback.requestID
	m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("new draft")})
	before := m.composer
	p := testEffortSnapshot("idle")
	p.Source = "runtime"
	p.Selected = protocol.EffortValuePayload{State: "explicit", Value: "low"}
	p.SelectionRevision = 1
	m = effortEvent(t, m, oldID, p)
	if !reflect.DeepEqual(before, m.composer) || len(m.effortNotice) != 0 || m.effortSelection.revision != 1 {
		t.Fatal("late feedback damaged draft")
	}
	m.composer.replace(0, len(m.composer.text), "/effort high")
	m, _ = updateModelWithCommand(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	id := m.effortFeedback.requestID
	m = effortAck(t, m)
	p.Selected.Value = "high"
	p.SelectionRevision = 2
	m = effortEvent(t, m, id, p)
	if m.composer.text != "" || len(m.effortNotice) != 2 {
		t.Fatal("current feedback lost")
	}
	notice := append([]string(nil), m.effortNotice...)
	p.Selected.Value = "low"
	p.SelectionRevision = 1
	m = effortEvent(t, m, oldID, p)
	if m.effortSelection.selected.Value != "high" || !reflect.DeepEqual(notice, m.effortNotice) {
		t.Fatal("older observation regressed")
	}
	m = updateModel(t, m, tea.WindowSizeMsg{Width: 28, Height: 10})
	if !reflect.DeepEqual(notice, m.effortNotice) {
		t.Fatal("resize changed notice")
	}
	m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("next"), Paste: true})
	if len(m.effortNotice) != 0 || m.composer.text != "next" {
		t.Fatal("editing retained notice or lost paste")
	}
}

func TestEffortFixedSnapshotsAndComposerPriority(t *testing.T) {
	for _, size := range []tea.WindowSizeMsg{{Width: 80, Height: 24}, {Width: 28, Height: 10}, {Width: 20, Height: 5}, {Width: 1, Height: 1}} {
		for _, help := range []bool{false, true} {
			for _, color := range []bool{false, true} {
				name := fmt.Sprintf("effort_%dx%d_help%t_color%t.golden", size.Width, size.Height, help, color)
				m := NewModel(context.Background(), "test", &effortFixtureSession{}).WithMarkdownColor(color)
				m = updateModel(t, m, size)
				if help {
					next, _ := m.submitEffortCommand(localEffortCommand{help: true})
					m = next.(Model)
				} else {
					p := testEffortSnapshot("turn")
					p.Selected = protocol.EffortValuePayload{State: "explicit", Value: "low"}
					p.Source = "runtime"
					p.SelectionRevision = 1
					p.Pending = true
					m.effortNotice = effortSummary(p)
				}
				m.composer.replace(0, 0, "next draft")
				got := m.View()
				if len(strings.Split(strings.TrimSuffix(got, "\n"), "\n")) > size.Height {
					t.Fatalf("notice exceeds height: %s", got)
				}
				for _, line := range strings.Split(got, "\n") {
					if ansi.StringWidth(line) > size.Width {
						t.Fatal("notice exceeds width", line)
					}
				}
				if strings.Contains(got, "\x1b") || len(m.effortNoticeLines()) > 6 {
					t.Fatal("notice depends on color or unbounded")
				}
				checkEffortGolden(t, name, got)
			}
		}
	}
}

func checkEffortGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if os.Getenv("EASYCODE_UPDATE_EFFORT_GOLDENS") == "1" {
		if err := os.WriteFile(path, []byte(got), 0600); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil || string(want) != got {
		t.Fatal("effort snapshot changed", name, err)
	}
}

func TestEffortSeventhBuiltinKeepsAllSkillsAndNames(t *testing.T) {
	entries := make([]domain.SkillListEntry, domain.MaxSkills)
	for i := range entries {
		entries[i] = completionSkillEntry(t, fmt.Sprintf("skill-%03d", i), "skill")
	}
	entries[0] = completionSkillEntry(t, "effort", "same name skill")
	catalog, err := newCompletionCatalog(entries)
	if err != nil || len(catalog.entries) != domain.MaxSkills+7 {
		t.Fatal("skill capacity shrank", err)
	}
	m := completionModel(t, "/eff").WithSkills(entries)
	m, cmd := updateModelWithCommand(t, m, tea.KeyMsg{Type: tea.KeyTab})
	if cmd != nil || m.composer.text != "/effort " {
		t.Fatal("Tab executed builtin")
	}
	m = completionModel(t, "$eff").WithSkills(entries)
	m, cmd = updateModelWithCommand(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil || m.composer.text != "$effort " || m.effortFeedback != nil {
		t.Fatal("same name skill captured as control")
	}
	m.skillsOpen, m.skillSelection = true, 0
	m, cmd = updateModelWithCommand(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil || m.skillsOpen || m.composer.text != "$effort " {
		t.Fatal("Skill picker did not preserve the explicit same-name invocation")
	}
}

func TestEffortSameRevisionOldOperationAndNewOverlayCannotRestoreFeedback(t *testing.T) {
	m := NewModel(context.Background(), "test", &effortFixtureSession{})
	m.composer.replace(0, 0, "/effort status")
	m, _ = updateModelWithCommand(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	id := m.effortFeedback.requestID
	m = effortAck(t, m)
	old := testEffortSnapshot("idle")
	started := protocol.NewTurnStarted()
	started.SessionID = "01900000-0000-7000-8000-000000000001"
	started.ThreadID = "01900000-0000-7000-8000-000000000002"
	started.TurnID = "01900000-0000-7000-8000-000000000003"
	m = updateModel(t, m, RuntimeEventMsg{Event: started})
	draft := m.composer
	question := &questionOverlay{}
	m.question = question
	m = effortEvent(t, m, id, old)
	if m.question != question || len(m.effortNotice) != 0 || !reflect.DeepEqual(draft, m.composer) {
		t.Fatal("same revision stale operation restored feedback")
	}
}

func TestEffortRejectedSettingPreservesCursorAndHistory(t *testing.T) {
	m := NewModel(context.Background(), "test", &effortFixtureSession{})
	m.composer.replace(0, 0, "/effort max")
	m.composer.moveTo(3)
	before := m.composer
	m, _ = updateModelWithCommand(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	f := m.effortFeedback
	choice, _ := domain.NewExplicitEffortChoice("max")
	command, _ := protocol.NewSetEffortCommand(f.requestID, choice)
	result, _ := protocol.NewRejectedCommandResult(command, protocol.ControlErrorEffortConflict)
	item, _ := protocol.NewCommandResultItem(result)
	next, _ := m.projectControlItem(item)
	m = next.(Model)
	if !reflect.DeepEqual(before, m.composer) || !strings.Contains(m.errorSummary, "effort_conflict") || len(m.effortNotice) != 0 {
		t.Fatal("rejection changed local draft")
	}
}

func TestEffortBusyTinyViewportPrioritizesComposer(t *testing.T) {
	for _, size := range []tea.WindowSizeMsg{{Width: 20, Height: 5}, {Width: 1, Height: 1}} {
		m := NewModel(context.Background(), "test", &effortFixtureSession{})
		m = updateModel(t, m, size)
		m.activeTurnID = "01900000-0000-7000-8000-000000000003"
		m.state = stateStreaming
		m.cancelling = true
		m.markdown.lines = []string{"streamed text"}
		m.effortNotice = []string{"Next effort: low", "Captured: high"}
		got := m.View()
		if len(strings.Split(strings.TrimSuffix(got, "\n"), "\n")) > size.Height {
			t.Fatalf("busy tiny viewport exceeds height %d: %q", size.Height, got)
		}
	}
}

func TestEffortLocalNavigationInvalidatesOnlyFeedback(t *testing.T) {
	for _, key := range []tea.KeyType{tea.KeyCtrlO, tea.KeyCtrlR, tea.KeyCtrlD, tea.KeyPgUp, tea.KeyPgDown, tea.KeyCtrlC, tea.KeyEsc} {
		m := NewModel(context.Background(), "test", &effortFixtureSession{})
		m.composer.replace(0, 0, "/effort status")
		m, _ = updateModelWithCommand(t, m, tea.KeyMsg{Type: tea.KeyEnter})
		id := m.effortFeedback.requestID
		m = effortAck(t, m)
		m.effortNotice = []string{"previous notice"}
		before := m.composer
		m = updateModel(t, m, tea.KeyMsg{Type: key})
		if m.effortFeedback != nil || len(m.effortNotice) != 0 {
			t.Fatal("local navigation retained feedback", key)
		}
		p := testEffortSnapshot("idle")
		p.Selected = protocol.EffortValuePayload{State: "explicit", Value: "low"}
		p.Source = "runtime"
		p.SelectionRevision = 1
		m = effortEvent(t, m, id, p)
		if !reflect.DeepEqual(before, m.composer) || len(m.effortNotice) != 0 || m.effortSelection.selected.Value != "low" {
			t.Fatal("late navigation feedback changed draft or lost selection", key)
		}
	}
}

func TestEffortActiveCancellationInvalidatesPendingFeedback(t *testing.T) {
	m := NewModel(context.Background(), "test", &effortFixtureSession{})
	m.activeTurnID = "01900000-0000-7000-8000-000000000003"
	m.state = stateStreaming
	m.composer.replace(0, 0, "/effort status")
	m, _ = updateModelWithCommand(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	id := m.effortFeedback.requestID
	m = effortAck(t, m)
	before := m.composer
	m, _ = updateModelWithCommand(t, m, tea.KeyMsg{Type: tea.KeyCtrlC})
	if !m.cancelling || m.effortFeedback != nil {
		t.Fatal("active cancellation retained local feedback")
	}
	p := testEffortSnapshot("turn")
	p.Selected = protocol.EffortValuePayload{State: "explicit", Value: "low"}
	p.Source = "runtime"
	p.SelectionRevision = 1
	p.Pending = true
	m = effortEvent(t, m, id, p)
	if !reflect.DeepEqual(before, m.composer) || len(m.effortNotice) != 0 || m.effortSelection.selected.Value != "low" {
		t.Fatal("cancelled feedback changed draft or reverted accepted selection")
	}
}

func TestEffortReviewBusyNoticeAndQueueStayWithinViewport(t *testing.T) {
	m := NewModel(context.Background(), "test", &effortFixtureSession{})
	m = updateModel(t, m, tea.WindowSizeMsg{Width: 28, Height: 10})
	m.activeTurnID = "01900000-0000-7000-8000-000000000003"
	m.state = stateStreaming
	m.markdown.lines = []string{"streamed text"}
	m.reasoning.notice = "Thinking text unavailable"
	m.inputs = []inputPreview{{id: "01900000-0000-7000-8000-000000000004", text: "queued task"}}
	m.effortNotice = []string{"Next effort: low", "Captured: high"}
	m.composer.replace(0, 0, "new draft")
	got := m.View()
	if len(strings.Split(strings.TrimSuffix(got, "\n"), "\n")) > 10 {
		t.Fatalf("notice displaced composer beyond viewport: %q", got)
	}
	for _, line := range strings.Split(got, "\n") {
		if ansi.StringWidth(line) > 28 {
			t.Fatalf("notice/queue exceeded width: %q", line)
		}
	}
	if !strings.Contains(got, "new draft") {
		t.Fatal("notice hid composer")
	}
	checkEffortGolden(t, "effort_busy_queue_28x10.golden", got)
}

func TestEffortHelpClearsRejectedCommandError(t *testing.T) {
	m := NewModel(context.Background(), "test", &effortFixtureSession{})
	m.composer.replace(0, 0, "/effort invalid")
	m, _ = updateModelWithCommand(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.errorSummary == "" {
		t.Fatal("invalid argument did not report an error")
	}
	m.composer.replace(0, len(m.composer.text), "/effort help")
	m, cmd := updateModelWithCommand(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil || m.errorSummary != "" || len(m.effortNotice) != 6 || m.composer.text != "" {
		t.Fatal("successful local help retained rejected-command state")
	}
}

func TestEffortDuplicateAndConflictingRevisionCannotChangeDraft(t *testing.T) {
	m := NewModel(context.Background(), "test", &effortFixtureSession{})
	m.composer.replace(0, 0, "/effort status")
	m, _ = updateModelWithCommand(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	id := m.effortFeedback.requestID
	m = effortAck(t, m)
	m = effortAck(t, m)
	p := testEffortSnapshot("idle")
	m = effortEvent(t, m, id, p)
	if len(m.composer.history.entries) != 1 || len(m.effortNotice) != 2 {
		t.Fatal("duplicate receipt consumed feedback twice")
	}
	m.composer.replace(0, 0, "new draft")
	draft := m.composer
	m = effortEvent(t, m, id, p)
	if !reflect.DeepEqual(draft, m.composer) || len(m.composer.history.entries) != 1 {
		t.Fatal("duplicate snapshot cleared a later draft")
	}
	selected := *m.effortSelection
	p.Selected = protocol.EffortValuePayload{State: "explicit", Value: "low"}
	p.Source = "runtime"
	if m.observeEffort(p, true) || *m.effortSelection != selected {
		t.Fatal("contradictory same-revision selection replaced authority")
	}
	p.SelectionRevision++
	if m.observeEffort(p, false) || m.effortSelection.selected.Value != "low" || !reflect.DeepEqual(draft, m.composer) {
		t.Fatal("newer non-local selection restored feedback or lost authority")
	}
}

func TestEffortStackedStatesAndFullQueueKeepPhysicalBounds(t *testing.T) {
	for _, size := range []tea.WindowSizeMsg{{Width: 80, Height: 24}, {Width: 28, Height: 10}, {Width: 20, Height: 5}, {Width: 1, Height: 1}} {
		m := NewModel(context.Background(), "test", &effortFixtureSession{})
		m = updateModel(t, m, size)
		m.activeTurnID = "01900000-0000-7000-8000-000000000003"
		m.state, m.cancelling = stateStreaming, true
		m.stopStatus, m.reasoning.notice = "Stop check is running", "Thinking text unavailable"
		m.reading.notice = "Reading history"
		m.compactionStatus = "Compacting captured high operation with a long status"
		m.errorSummary = "endpoint_failure: request failed"
		m.effortNotice = []string{"Next effort: low", "Captured: high"}
		for i := 0; i < 64; i++ {
			m.inputs = append(m.inputs, inputPreview{id: domain.InputID(fmt.Sprintf("01900000-0000-7000-8000-%012x", i)), text: strings.Repeat("queued 中文 task ", 6)})
		}
		m.composer.replace(0, 0, "retained draft")
		before := m.composer
		got := m.View()
		if renderedChatRows(got) > size.Height || len(m.inputs) != 64 || !reflect.DeepEqual(before, m.composer) {
			t.Fatalf("view exceeded height or mutated input state at %v: %q", size, got)
		}
		for _, line := range strings.Split(got, "\n") {
			if ansi.StringWidth(line) > size.Width {
				t.Fatalf("view exceeded width at %v: %q", size, line)
			}
		}
		if size.Width >= 20 && !strings.Contains(got, "retained draft") {
			t.Fatal("stacked statuses hid the editable draft", got)
		}
	}
}

func TestEffortQueueClippingKeepsNextEditableInputVisible(t *testing.T) {
	m := NewModel(context.Background(), "test", &effortFixtureSession{})
	m = updateModel(t, m, tea.WindowSizeMsg{Width: 28, Height: 10})
	for i := 0; i < 64; i++ {
		m.inputs = append(m.inputs, inputPreview{text: fmt.Sprintf("task-%02d", i)})
	}
	view := m.renderInputs()
	if !strings.Contains(view, "task-00") || strings.Contains(view, "task-63") || !strings.Contains(view, "more queued or") || len(m.inputs) != 64 {
		t.Fatal("clipped presentation lost FIFO head or changed queue", view)
	}
}
