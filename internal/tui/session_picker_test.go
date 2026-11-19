package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"easycode/internal/domain"
)

type pickerTestHost struct {
	requests           []PickerRequest
	results            chan PickerResult
	cancelled, stopped bool
	accept             bool
}

func (h *pickerTestHost) Submit(r PickerRequest) bool {
	h.requests = append(h.requests, r)
	return true
}
func (h *pickerTestHost) Results() <-chan PickerResult { return h.results }
func (h *pickerTestHost) CancelPending()               { h.cancelled = true }
func (h *pickerTestHost) Stop()                        { h.stopped = true }
func (h *pickerTestHost) Accept(uint64) bool           { return h.accept }
func pickerTestModel(t *testing.T) (StartupModel, *pickerTestHost) {
	t.Helper()
	h := &pickerTestHost{results: make(chan PickerResult, 10)}
	m := NewStartupModel(h, PickerConfig{Family: domain.ProviderOpenAI, Wire: "responses", Model: "gpt", CWD: "/here"})
	m = pickerUpdate(m, pickerStart{})
	request := m.request
	m = pickerUpdate(m, PickerResult{Request: request, Entries: []SessionEntry{
		{ThreadID: "01900000-0000-7000-8000-000000000001", Family: domain.ProviderOpenAI, Wire: "responses", Model: "gpt", CWD: "/here", UpdatedAt: time.Unix(0, 0)},
		{ThreadID: "01900000-0000-7000-8000-000000000002", Family: domain.ProviderAnthropic, Wire: "messages", Model: "Claude", CWD: "/else"},
	}})
	return m, h
}
func pickerUpdate(m StartupModel, msg tea.Msg) StartupModel {
	next, _ := m.Update(msg)
	return next.(StartupModel)
}
func pickerKey(m StartupModel, key tea.KeyType) StartupModel {
	return pickerUpdate(m, tea.KeyMsg{Type: key})
}

func TestPickerResumingDoesNotSubmitBufferedInputToChat(t *testing.T) {
	m, h := pickerTestModel(t)
	m = pickerKey(m, tea.KeyEnter)
	m = pickerUpdate(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("do not submit")})
	m = pickerKey(m, tea.KeyEnter)
	session := &fakeChatSession{}
	chat := NewModel(context.Background(), "test", session)
	h.accept = true
	m = pickerUpdate(m, PickerResult{Request: m.request, Chat: &chat})
	if m.chat == nil || strings.Contains(m.View(), "do not submit") || len(session.submits) != 0 {
		t.Fatal("resuming input crossed into chat")
	}
	m = pickerUpdate(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("new draft")})
	if !strings.Contains(m.View(), "new draft") {
		t.Fatal("existing chat composer not delegated")
	}
}

func TestPickerTitlesSearchIndependentlyAndPreserveSelectionByID(t *testing.T) {
	m, host := pickerTestModel(t)
	first, second := m.entries[0], m.entries[1]
	first.Title, second.Title = "Review PARSER", "Review Parser"
	second.Family, second.Wire, second.Model, second.CWD = first.Family, first.Wire, first.Model, first.CWD
	m = pickerUpdate(m, PickerResult{Request: m.request, Entries: []SessionEntry{first, second}, HasMore: true})
	if view := m.View(); !strings.Contains(view, "Latest 200 sessions only") || !strings.Contains(view, string(first.Title)) {
		t.Fatalf("bounded named list missing from view: %s", view)
	}
	m = pickerUpdate(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("parser " + string(first.ThreadID))})
	if len(m.filtered()) != 0 {
		t.Fatal("title search combined adjacent metadata fields")
	}
	m = pickerKey(m, tea.KeyEsc)
	m = pickerUpdate(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("REVIEW PARSER")})
	m = pickerKey(m, tea.KeyDown)
	if len(m.filtered()) != 2 || m.selected != second.ThreadID {
		t.Fatalf("duplicate title selection = %s, matches=%d", m.selected, len(m.filtered()))
	}
	m = pickerKey(m, tea.KeyCtrlR)
	second.Title = "Review Parser updated"
	m = pickerUpdate(m, PickerResult{Request: m.request, Entries: []SessionEntry{second, first}})
	if entry, ok := m.selection(); !ok || entry.ThreadID != second.ThreadID || entry.Title != second.Title || m.query != "REVIEW PARSER" {
		t.Fatalf("refresh did not preserve selected identity: %+v, query=%q", entry, m.query)
	}
	m = pickerKey(m, tea.KeyCtrlV)
	if got := host.requests[len(host.requests)-1]; got.Operation != PickerPreview || got.ThreadID != second.ThreadID {
		t.Fatalf("preview did not use real thread id: %+v", got)
	}
}

func TestPickerLongTitleAndPreviewContentRemainReachable(t *testing.T) {
	m, _ := pickerTestModel(t)
	entry := m.entries[0]
	entry.Title = domain.SessionTitle("列表 👩‍💻 " + strings.Repeat("a", 80) + "末端")
	m = pickerUpdate(m, PickerResult{Request: m.request, Entries: []SessionEntry{entry}})
	m = pickerUpdate(m, tea.WindowSizeMsg{Width: 15, Height: 9})
	view := m.View()
	if !strings.Contains(view, "> 列表 👩‍💻") || !strings.Contains(view, "…") || strings.Contains(view, "末端") {
		t.Fatalf("narrow list did not preserve visible title graphemes: %s", view)
	}
	m = pickerKey(m, tea.KeyTab)
	if !strings.Contains(m.View(), "👩‍💻") || !strings.Contains(scrollPickerViews(t, m), "末端") || m.selected != entry.ThreadID {
		t.Fatalf("narrow details lost title or selection: %s", m.View())
	}
	m = pickerKey(m, tea.KeyEsc)
	m = pickerKey(m, tea.KeyCtrlV)
	m = pickerUpdate(m, PickerResult{Request: m.request, Preview: SessionPreview{
		Title:    domain.SessionTitle("预览 👩‍💻 " + strings.Repeat("b", 80) + "结尾"),
		Messages: []PreviewMessage{{Message: domain.SemanticMessage{Role: domain.SemanticAssistant, Text: "Visible body"}}},
	}})
	if view := m.View(); !strings.Contains(view, "👩‍💻") || strings.Contains(view, "列表") {
		t.Fatalf("preview showed a stale listing title: %s", view)
	}
	if scrolled := scrollPickerViews(t, m); !strings.Contains(scrolled, "结尾") || !strings.Contains(scrolled, "Visible body") {
		t.Fatalf("narrow preview did not expose title ending and body: %s", scrolled)
	}
	m = pickerKey(m, tea.KeyEsc)
	if m.selected != entry.ThreadID || !strings.Contains(m.View(), "> 列表 👩‍💻") {
		t.Fatal("preview changed the picker snapshot or selection")
	}
}

func scrollPickerViews(t *testing.T, m StartupModel) string {
	t.Helper()
	var output strings.Builder
	for range 12 {
		for _, line := range strings.Split(m.View(), "\n") {
			if ansi.StringWidth(line) > m.width {
				t.Fatalf("picker frame exceeded terminal width: %q", line)
			}
			output.WriteString(line)
			output.WriteByte('\n')
		}
		m = pickerKey(m, tea.KeyPgDown)
	}
	return output.String()
}
