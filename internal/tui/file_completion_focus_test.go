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
)

func TestFileCompletionExclusiveFocusStreamingPasteAndNewline(t *testing.T) {
	setters := map[string]func(*Model){"question": func(m *Model) { m.question = &questionOverlay{} }, "trust": func(m *Model) { m.hookTrust = &hookTrustOverlay{} }, "hooks": func(m *Model) { m.hooks.open = true }, "skills": func(m *Model) { m.skillsOpen = true }, "plugins": func(m *Model) { m.plugins.open = true }, "approval": func(m *Model) { m.approval = &approvalOverlay{} }, "cache": func(m *Model) { m.cachePanel = &protocol.CacheSnapshotPayload{} }, "context": func(m *Model) { m.contextPanel = &protocol.ContextSnapshotPayload{} }, "transcript": func(m *Model) { m.detailOpen = true }, "pending": func(m *Model) { c := m.composer; m.pending = &c }, "retract": func(m *Model) { m.retracting = &inputRetraction{} }, "cancel": func(m *Model) { m.cancelling = true }}
	for name, set := range setters {
		t.Run(name, func(t *testing.T) {
			m, f := fileCompletionModel(t, "prefix @a")
			id := m.files.identity
			before := m.composer
			set(&m)
			m.prepareCompletion()
			if m.files.rows != 0 || len(f.cancels) != 1 || !reflect.DeepEqual(before, m.composer) {
				t.Fatal("exclusive focus failed to cancel names without editing")
			}
			m.question = nil
			m.hookTrust = nil
			m.hooks.open = false
			m.skillsOpen = false
			m.plugins.open = false
			m.approval = nil
			m.cachePanel = nil
			m.contextPanel = nil
			m.detailOpen = false
			m.pending = nil
			m.retracting = nil
			m.cancelling = false
			m = fileReply(t, m, id, 1, []string{"a"}, domain.FileSearchComplete, domain.FileDiscoveryReport{})
			if m.files.rows != 0 {
				t.Fatal("modal stale result reopened")
			}
			m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyTab})
			if m.files.rows == 0 || m.files.session == id.SessionGeneration || m.composer.text != before.text {
				t.Fatal("explicit reopen failed")
			}
		})
	}
	for _, key := range []tea.KeyMsg{{Type: tea.KeyCtrlJ}, {Type: tea.KeyEnter, Alt: true}} {
		m, f := fileCompletionModel(t, "@a")
		m = updateModel(t, m, key)
		if m.composer.text != "@a\n" || m.pending != nil || m.files.rows != 0 || len(f.cancels) != 1 {
			t.Fatal("newline accepted file or retained session")
		}
	}
	m, f := fileCompletionModel(t, "")
	m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("@pasted"), Paste: true})
	if len(f.queries) != 0 || m.files.rows != 0 {
		t.Fatal("paste started discovery")
	}
	m, f = fileCompletionModel(t, "@a")
	before := m.files.identity
	m.activeTurnID = "01900000-0000-7000-8000-000000000010"
	delta, _ := protocol.NewAssistantTextDelta("output")
	delta.TurnID = m.activeTurnID
	m = updateModel(t, m, RuntimeEventMsg{Event: delta})
	m = updateModel(t, m, tea.WindowSizeMsg{Width: 28, Height: 10})
	if len(f.queries) != 1 || m.files.identity != before {
		t.Fatal("stream or resize queried again")
	}
	m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.cancelling || m.files.rows != 0 {
		t.Fatal("first Escape stopped turn")
	}
	m, cmd := updateModelWithCommand(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if !m.cancelling || cmd == nil {
		t.Fatal("second Escape did not stop turn")
	}
}

func TestFileCompletionInvalidSubmitAndLateEffortDoNotTouchNewDraft(t *testing.T) {
	for _, text := range []string{"inspect @../outside", `inspect @"unterminated`, "@a @b @c @d @e @f @g @h @i"} {
		m, _ := fileCompletionModel(t, text)
		before := m.composer
		m, cmd := updateModelWithCommand(t, m, tea.KeyMsg{Type: tea.KeyEnter})
		if cmd != nil || m.pending != nil || !reflect.DeepEqual(before, m.composer) || len(m.session.(*fakeChatSession).submits) != 0 || m.errorSummary == "" {
			t.Fatal("invalid draft reached host or changed editor", text)
		}
	}
	s := &effortFixtureSession{}
	f := &fileQueryFixture{results: make(chan domain.FileSearchSnapshot)}
	m := NewModel(context.Background(), "test", s).WithFileQueries(f)
	m.composer.replace(0, 0, "/effort low")
	m, _ = updateModelWithCommand(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	id := m.effortFeedback.requestID
	m = effortAck(t, m)
	m.restoreComposer(composerWithText(t, "@a"))
	m.prepareCompletion()
	m = fileReply(t, m, m.files.identity, 1, []string{"a b"}, domain.FileSearchComplete, domain.FileDiscoveryReport{})
	m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyTab})
	before := m.composer
	p := testEffortSnapshot("idle")
	p.Selected = protocol.EffortValuePayload{State: "explicit", Value: "low"}
	p.Source = "runtime"
	p.SelectionRevision = 1
	m = effortEvent(t, m, id, p)
	m.prepareCompletion()
	if !reflect.DeepEqual(before, m.composer) || len(m.effortNotice) != 0 || m.files.rows != 0 || m.effortSelection.selected.Value != "low" {
		t.Fatal("late effort feedback cleared file draft or reopened focus")
	}
}

func TestFileMainlineLiveAndRestoredUseSameBodyFreeSummary(t *testing.T) {
	data, err := os.ReadFile("../domain/testdata/file_attachment.json")
	if err != nil {
		t.Fatal(err)
	}
	a, err := domain.DecodeFileAttachment(data)
	if err != nil {
		t.Fatal(err)
	}
	files, _ := domain.NewFileAttachments([]domain.FileAttachment{a})
	input, _ := domain.NewUserInput("01900000-0000-7000-8000-000000000011", "inspect @src/a.go#L2-8")
	input, err = input.WithFileAttachments(files)
	if err != nil {
		t.Fatal(err)
	}
	turn := domain.TurnID("01900000-0000-7000-8000-000000000010")
	live := NewModel(context.Background(), "test", nil)
	live.activeTurnID = turn
	e, err := protocol.NewUserInputCommitted(input)
	if err != nil {
		t.Fatal(err)
	}
	e.TurnID = turn
	live = updateModel(t, live, RuntimeEventMsg{Event: e})
	summary, _ := domain.NewFileSummary(files)
	restored := NewModel(context.Background(), "test", nil)
	page, _ := protocol.NewTranscriptListPage(protocol.TranscriptListPage{Items: []protocol.TranscriptListItem{{Cursor: protocol.TranscriptCursor{Sequence: 5}, Kind: domain.TranscriptUser, TurnID: turn, InputID: input.ID(), Text: input.Text(), More: true, Files: summary}}})
	restored = updateModel(t, restored, mainTranscriptReply{page: page})
	if live.transcript[0].text != restored.main.items[0].text || strings.Contains(live.transcript[0].text, "second") || !strings.Contains(live.transcript[0].text, "lines 2-3/3") {
		t.Fatal("mainline file summary changed on restore or exposed body")
	}
}

func fileQueueSnapshotModel(t *testing.T, width, height int) Model {
	t.Helper()
	m, _ := fileCompletionModel(t, "line one\n参考 @sr")
	m.activeTurnID = "01900000-0000-7000-8000-000000000010"
	m.state = stateStreaming
	for i := 0; i < 64; i++ {
		m.inputs = append(m.inputs, inputPreview{id: domain.InputID(fmt.Sprintf("01900000-0000-7000-8000-%012d", i+100)), text: fmt.Sprintf("FIFO-%02d @raw", i)})
	}
	m.effortNotice = []string{"Selected effort: low · next operation", "/effort <level> · /effort reset", "/effort status"}
	m = fileReply(t, m, m.files.identity, 1, []string{"src/a b.go", "src/文.go", "src/c.go"}, domain.FileSearchComplete, domain.FileDiscoveryReport{})
	return updateModel(t, m, tea.WindowSizeMsg{Width: width, Height: height})
}
func TestFileCompletionQueueEffortAndSmallWindowSnapshots(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {28, 10}, {20, 5}, {1, 1}} {
		m := fileQueueSnapshotModel(t, size[0], size[1])
		view := m.View()
		name := fmt.Sprintf("file_queue_%dx%d.golden", size[0], size[1])
		want, err := os.ReadFile(filepath.Join("testdata", name))
		if err != nil || view != string(want) {
			t.Fatal("queue/effort snapshot differs", name, err)
		}
		if len(m.inputs) != 64 || m.inputs[0].text != "FIFO-00 @raw" || m.composer.cursor != len(m.composer.text) {
			t.Fatal("viewport deleted queue or moved cursor")
		}
		if renderedChatRows(view) > size[1] {
			t.Fatal("viewport overflow")
		}
		if size[1] >= 10 && !strings.Contains(view, "FIFO-00") {
			t.Fatal("FIFO head was not retained")
		}
		if m.files.visible == 0 {
			before := m.composer
			m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyTab})
			if !reflect.DeepEqual(before, m.composer) {
				t.Fatal("small viewport accepted hidden choice")
			}
		}
	}
}
