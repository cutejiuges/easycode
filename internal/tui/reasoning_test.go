package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"easycode/internal/domain"
	"easycode/internal/protocol"
	tea "github.com/charmbracelet/bubbletea"
)

func reasoningModel(t *testing.T) Model {
	t.Helper()
	m, _, _ := browserModel(t)
	m.activeTurnID = domain.TurnID("018f1d8a-7b5c-7def-8123-456789abcdea")
	m.reasoning = activeReasoning{}
	return m
}
func reasoningUIEvent(t *testing.T, m Model, event protocol.Event) Model {
	t.Helper()
	event.TurnID = m.activeTurnID
	updated, cmd := m.projectRuntimeEvent(event)
	if cmd != nil {
		t.Fatal("reasoning delta created asynchronous work")
	}
	m = updated.(Model)
	m.prepareMarkdownView()
	m.prepareMarkdownDetail()
	if strings.Contains(m.errorSummary, "stream_protocol_error") {
		t.Fatal(m.errorSummary)
	}
	return m
}
func reasoningUIStart(t *testing.T, m Model, block uint32) Model {
	event, _ := protocol.NewReasoningStarted(block, domain.ReasoningOpenAISummary)
	return reasoningUIEvent(t, m, event)
}
func reasoningUIDelta(t *testing.T, m Model, block uint32, offset uint64, text string) Model {
	event, err := protocol.NewReasoningDelta(block, domain.ReasoningOpenAISummary, offset, text)
	if err != nil {
		t.Fatal(err)
	}
	return reasoningUIEvent(t, m, event)
}
func reasoningUIComplete(t *testing.T, m Model, block uint32, text string) Model {
	availability := domain.ReasoningReadable
	if text == "" {
		availability = domain.ReasoningUnavailable
	}
	event, err := protocol.NewReasoningCompleted(block, domain.ReasoningOpenAISummary, protocol.ReasoningCompletion{Availability: availability, TotalBytes: uint64(len(text)), Text: text})
	if err != nil {
		t.Fatal(err)
	}
	return reasoningUIEvent(t, m, event)
}

func TestReasoningDefaultStatusAndUnifiedBrowserPreserveDraft(t *testing.T) {
	m := reasoningModel(t)
	draft := m.composer.text
	if strings.Contains(m.View(), "Thinking") {
		t.Fatal("invented reasoning")
	}
	m = reasoningUIStart(t, m, 0)
	m = reasoningUIDelta(t, m, 0, 0, "# 中文🙂\n\n```go\nanswer()\n```\n\x1b]52;c;secret\a\u202eunsafe")
	if !strings.Contains(m.View(), "Thinking") || strings.Contains(m.View(), "answer()") {
		t.Fatal("default presentation incorrect")
	}
	var cmd tea.Cmd
	m, cmd = browserKey(t, m, tea.KeyCtrlO)
	if cmd != nil {
		m = updateModel(t, m, cmd())
	}
	if len(m.browser.items) < 1 || !m.browser.items[0].temporary.live {
		t.Fatal("live item not merged")
	}
	m.browser.selected = 0
	m, cmd = browserKey(t, m, tea.KeyEnter)
	if cmd != nil {
		t.Fatal("live read performed query")
	}
	view := m.View()
	if !strings.Contains(view, "answer()") || strings.Contains(view, "\x1b]52") || strings.Contains(view, "\u202e") {
		t.Fatal(view)
	}
	m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	if !strings.Contains(m.View(), "```go") {
		t.Fatal("safe raw toggle missing")
	}
	m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("leak")})
	m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("paste"), Paste: true})
	if m.composer.text != draft {
		t.Fatal("reader edited composer")
	}
	m, _ = browserKey(t, m, tea.KeyEsc)
	m, _ = browserKey(t, m, tea.KeyEsc)
	if m.detailOpen || m.composer.text != draft {
		t.Fatal("reader focus leaked")
	}
}

func TestReasoningBudgetRetainsSelectedAndBoundedItems(t *testing.T) {
	m := reasoningModel(t)
	m = reasoningUIStart(t, m, 0)
	m.detailOpen = true
	m.browser.items = []transcriptDetailItem{{temporary: reasoningKey{m.activeTurnID, 0, 0, true}, TranscriptListItem: protocol.TranscriptListItem{Kind: domain.TranscriptReasoning}}}
	m.browser.detail = true
	selected := m.selectedReasoning()
	for block := uint32(0); block < 80; block++ {
		if block > 0 {
			m = reasoningUIStart(t, m, block)
		}
		text := strings.Repeat("🙂", 4096)
		m = reasoningUIDelta(t, m, block, 0, text)
	}
	total := 0
	for _, item := range m.reasoning.items {
		total += len(item.text)
		if !utf8.ValidString(item.text) {
			t.Fatal("invalid excerpt")
		}
	}
	if total > 64<<10 || len(m.reasoning.items) > 50 || m.reasoning.find(selected) == nil || m.reasoning.find(selected).text == "" {
		t.Fatal("shared budget or selection protection failed", total, len(m.reasoning.items))
	}
	closed := make(map[uint32]bool)
	for _, preview := range append([]reasoningPreview(nil), m.reasoning.items...) {
		m = reasoningUIComplete(t, m, preview.key.block, strings.Repeat("🙂", 4096))
		closed[preview.key.block] = true
	}
	if !strings.Contains(m.reasoning.status, "Thinking") {
		t.Fatal("display eviction hid an active protocol block")
	}
	bindings := make([]domain.ReasoningBinding, 80)
	for block := uint32(0); block < 80; block++ {
		if !closed[block] {
			m = reasoningUIComplete(t, m, block, strings.Repeat("🙂", 4096))
		}
		bindings[block] = domain.ReasoningBinding{Block: block, Item: block, Source: domain.ReasoningOpenAISummary, Availability: domain.ReasoningReadable}
	}
	if err := m.reasoning.stream.Commit(protocol.SampleCommitPayload{ItemCount: 80, Bindings: bindings}); err != nil {
		t.Fatal("display eviction removed validator state", err)
	}
}
func TestReasoningCommitUpgradesExactCursorBeforeSlowQuery(t *testing.T) {
	m := reasoningModel(t)
	m = reasoningUIStart(t, m, 0)
	m = reasoningUIDelta(t, m, 0, 0, "same text")
	m = reasoningUIComplete(t, m, 0, "same text")
	m.detailOpen = true
	m.browser.items = []transcriptDetailItem{{temporary: m.reasoning.items[0].key, TranscriptListItem: protocol.TranscriptListItem{Kind: domain.TranscriptReasoning}}}
	m.browser.detail = true
	m.prepareLiveReasoningPage()
	oldGeneration := m.browser.generation
	p, _ := domain.NewSamplePresentation(8, []domain.ReasoningBinding{{Block: 0, Item: 7, Source: domain.ReasoningOpenAISummary, Availability: domain.ReasoningReadable}})
	event, _ := protocol.NewSampleTranscriptCommitted(999, 0, p)
	payload, _ := protocol.DecodeTranscriptCommitted(event)
	updated, cmd, err := m.commitReasoning(payload)
	if err != nil || cmd == nil {
		t.Fatal(err)
	}
	m = updated
	if m.browser.page == nil || m.browser.page.body.Text != "same text" {
		t.Fatal("slow durable query flashed the live body empty")
	}
	if m.browser.items[0].Cursor != (protocol.TranscriptCursor{Sequence: 999, Item: 7}) || m.browser.items[0].temporary.live || len(m.reasoning.items) != 0 || m.reasoning.status != "" {
		t.Fatal("not upgraded immediately")
	}
	// 旧列表回复不能按相同文字或较新列表替换当前精确位置。
	m = updateModel(t, m, transcriptReply{generation: oldGeneration, page: cachedTranscriptPage{key: transcriptPageKey{kind: transcriptListPage}}})
	if m.browser.items[0].Cursor.Sequence != 999 || !m.browser.detail {
		t.Fatal("stale query replaced selection")
	}
	reply := cmd()
	m, _ = browserKey(t, m, tea.KeyCtrlO)
	m = updateModel(t, m, reply)
	if m.detailOpen {
		t.Fatal("late body reopened closed reader")
	}
}

func TestReasoningFailureStatesAndSnapshotRelease(t *testing.T) {
	for _, disposition := range []protocol.SampleDisposition{protocol.SampleNotCommitted, protocol.SampleUncertain} {
		t.Run(string(disposition), func(t *testing.T) {
			m := reasoningModel(t)
			m = reasoningUIStart(t, m, 0)
			m = reasoningUIDelta(t, m, 0, 0, "partial")
			resolution := protocol.NewUncommittedSampleResolution(0)
			if disposition == protocol.SampleUncertain {
				resolution = protocol.NewUncertainSampleResolution(0)
			}
			if err := m.resolveReasoning(protocol.TurnFailedPayload{Resolution: &resolution}); err != nil {
				t.Fatal(err)
			}
			if !m.reasoning.failed || m.reasoning.stream.Pending() || m.reasoning.items[0].text != "partial" {
				t.Fatal("failed sample not resolved")
			}
			started := protocol.NewTurnStarted()
			started.TurnID = m.activeTurnID
			next, _ := m.projectRuntimeEvent(started)
			m = next.(Model)
			if len(m.reasoning.items) != 0 || !strings.Contains(m.reasoning.notice, "released") {
				t.Fatal("old snapshot retained")
			}
		})
	}
}

func TestReasoningStatusGolden(t *testing.T) {
	for _, width := range []int{80, 28} {
		m := reasoningModel(t)
		m.width = width
		m.height = 14
		m = reasoningUIStart(t, m, 0)
		m = reasoningUIDelta(t, m, 0, 0, "中文🙂 **allowed**")
		text := m.View()
		path := filepath.Join("testdata", fmt.Sprintf("reasoning-status-%d.golden.txt", width))
		if os.Getenv("UPDATE_GOLDEN") == "1" {
			if err := os.WriteFile(path, []byte(text), 0600); err != nil {
				t.Fatal(err)
			}
		}
		expected, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if text != string(expected) {
			t.Fatalf("snapshot differs: %s", path)
		}
	}
}

func TestReasoningModalFocusAndCancellationPriority(t *testing.T) {
	for _, kind := range []string{"approval", "question", "hook"} {
		t.Run(kind, func(t *testing.T) {
			m := reasoningModel(t)
			m = reasoningUIStart(t, m, 0)
			m = reasoningUIDelta(t, m, 0, 0, "reader body")
			m.detailOpen = true
			m.browser.detail = true
			m.browser.items = []transcriptDetailItem{{temporary: m.reasoning.items[0].key, TranscriptListItem: protocol.TranscriptListItem{Kind: domain.TranscriptReasoning}}}
			m.prepareLiveReasoningPage()
			var event protocol.Event
			switch kind {
			case "approval":
				event = mustApprovalRequested(t)
			case "question":
				event = questionUIEvent(t, 1)
			case "hook":
				event = hookUIRequest(t, m)
			}
			event.TurnID = m.activeTurnID
			m = updateModel(t, m, RuntimeEventMsg{Event: event})
			before := m.composer.text
			m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
			m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("paste"), Paste: true})
			if m.composer.text != before || m.browser.markdown.raw {
				t.Fatal("modal keys leaked to reader or composer")
			}
			m = reasoningUIDelta(t, m, 0, uint64(len("reader body")), " continued")
			if strings.HasPrefix(m.View(), "Reasoning ·") {
				t.Fatal("reasoning stole modal focus")
			}
			path := filepath.Join("testdata", "reasoning-modal-"+kind+".golden.txt")
			if os.Getenv("UPDATE_GOLDEN") == "1" {
				if err := os.WriteFile(path, []byte(m.View()), 0600); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(want) != m.View() {
				t.Fatal("modal snapshot drift", kind)
			}
		})
	}
	m := reasoningModel(t)
	m.cancelling = true
	m = reasoningUIStart(t, m, 0)
	m = reasoningUIComplete(t, m, 0, "")
	if strings.Contains(m.View(), "Thinking") || !strings.Contains(m.View(), "Cancelling") {
		t.Fatal("boundary restarted activity after cancel")
	}
}

func TestReasoningRepeatedTextContinuousCommitsAndEvictedSelection(t *testing.T) {
	m := reasoningModel(t)
	for block := uint32(0); block < 2; block++ {
		m = reasoningUIStart(t, m, block)
		m = reasoningUIDelta(t, m, block, 0, "same")
		event, _ := protocol.NewReasoningCompleted(block, domain.ReasoningOpenAISummary, protocol.ReasoningCompletion{Availability: domain.ReasoningReadable, Offset: 100000, TotalBytes: 100004, Text: "same"})
		m = reasoningUIEvent(t, m, event)
	}
	m.detailOpen = true
	m.mergeLiveReasoning()
	m.browser.selected = 1
	m.browser.detail = true
	m.prepareLiveReasoningPage()
	selected := m.selectedReasoning()
	if selected.block != 0 {
		t.Fatal("fixture selection wrong")
	}
	presentation, _ := domain.NewSamplePresentation(10, []domain.ReasoningBinding{{Block: 0, Item: 7, Source: domain.ReasoningOpenAISummary, Availability: domain.ReasoningReadable}, {Block: 1, Item: 9, Source: domain.ReasoningOpenAISummary, Availability: domain.ReasoningReadable}})
	commit, _ := protocol.NewSampleTranscriptCommitted(999, 0, presentation)
	payload, _ := protocol.DecodeTranscriptCommitted(commit)
	m, cmd, err := m.commitReasoning(payload)
	if err != nil || cmd == nil {
		t.Fatal(err)
	}
	reply := cmd().(transcriptReply)
	if reply.page.key.cursor != (protocol.TranscriptCursor{Sequence: 999, Item: 7}) || reply.page.key.offset != 100000 {
		t.Fatal("handoff searched latest list or lost source offset", reply.page.key)
	}
	for _, event := range []protocol.Event{mustReasoningStart(t), mustReasoningEnd(t, "same")} {
		bound, err := protocol.BindReasoningSample(event, 1)
		if err != nil {
			t.Fatal(err)
		}
		m = reasoningUIEvent(t, m, bound)
	}
	nextPresentation, _ := domain.NewSamplePresentation(1, []domain.ReasoningBinding{{Block: 0, Item: 0, Source: domain.ReasoningOpenAISummary, Availability: domain.ReasoningReadable}})
	nextCommit, _ := protocol.NewSampleTranscriptCommitted(1000, 1, nextPresentation)
	payload, _ = protocol.DecodeTranscriptCommitted(nextCommit)
	m, cmd, err = m.commitReasoning(payload)
	if err != nil || cmd != nil || m.browser.items[m.browser.selected].Cursor != (protocol.TranscriptCursor{Sequence: 999, Item: 7}) {
		t.Fatal("continuous commit replaced identical selected text", err)
	}
}
func mustReasoningStart(t *testing.T) protocol.Event {
	t.Helper()
	event, err := protocol.NewReasoningStarted(0, domain.ReasoningOpenAISummary)
	if err != nil {
		t.Fatal(err)
	}
	return event
}
func mustReasoningEnd(t *testing.T, text string) protocol.Event {
	t.Helper()
	event, err := protocol.NewReasoningCompleted(0, domain.ReasoningOpenAISummary, protocol.ReasoningCompletion{Availability: domain.ReasoningReadable, TotalBytes: uint64(len(text)), Text: text})
	if err != nil {
		t.Fatal(err)
	}
	return event
}

func TestReasoningDetailGolden(t *testing.T) {
	for _, width := range []int{80, 28} {
		for _, state := range []string{"readable", "unavailable", "excerpt", "long-fence"} {
			t.Run(fmt.Sprintf("%d/%s", width, state), func(t *testing.T) {
				m := reasoningModel(t)
				m.width = width
				m.height = 14
				m = reasoningUIStart(t, m, 0)
				text := "## Reasoning\n\n中文🙂 **allowed**\n\n```go\nreason()\n```\n\x1b]52;c;opaque\a\u202eend"
				if state == "unavailable" {
					text = ""
				}
				if state == "long-fence" {
					text = "```go\n" + strings.Repeat("var 中文 = 42\n", 3000)
				}
				if state == "excerpt" {
					event, _ := protocol.NewReasoningCompleted(0, domain.ReasoningOpenAISummary, protocol.ReasoningCompletion{Availability: domain.ReasoningReadable, Offset: 100000, TotalBytes: 100000 + uint64(len(text)), Text: text})
					m = reasoningUIEvent(t, m, event)
				} else {
					m = reasoningUIComplete(t, m, 0, text)
				}
				m.detailOpen = true
				m.mergeLiveReasoning()
				m.browser.selected = 0
				m.browser.detail = true
				m.prepareLiveReasoningPage()
				m.prepareMarkdownDetail()
				path := filepath.Join("testdata", fmt.Sprintf("reasoning-detail-%d-%s.golden.txt", width, state))
				if os.Getenv("UPDATE_GOLDEN") == "1" {
					if err := os.WriteFile(path, []byte(m.View()), 0600); err != nil {
						t.Fatal(err)
					}
				}
				want, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if string(want) != m.View() {
					t.Fatal("detail snapshot drift", path)
				}
				if strings.Contains(m.View(), "\x1b]52") || strings.Contains(m.View(), "\u202e") {
					t.Fatal("unsafe terminal text")
				}
			})
		}
	}
}

func TestReasoningDurableFailureOpensExactSavedSnapshotAndExitReleasesIt(t *testing.T) {
	m := reasoningModel(t)
	m = reasoningUIStart(t, m, 0)
	m = reasoningUIDelta(t, m, 0, 0, "saved excerpt")
	m = reasoningUIComplete(t, m, 0, "saved excerpt")
	presentation, _ := domain.NewSamplePresentation(4, []domain.ReasoningBinding{{Block: 0, Item: 3, Source: domain.ReasoningOpenAISummary, Availability: domain.ReasoningReadable}})
	resolution, err := protocol.NewDurableSampleResolution(0, 700, presentation)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.resolveReasoning(protocol.TurnFailedPayload{Resolution: &resolution}); err != nil {
		t.Fatal(err)
	}
	m, cmd := browserKey(t, m, tea.KeyCtrlO)
	if cmd != nil {
		m = updateModel(t, m, cmd())
	}
	if len(m.browser.items) == 0 || m.browser.items[0].Cursor != (protocol.TranscriptCursor{Sequence: 700, Item: 3}) || m.browser.items[0].temporary.live {
		t.Fatal("durable failure retained temporary identity")
	}
	m.browser.selected = 0
	m, cmd = browserKey(t, m, tea.KeyEnter)
	if cmd != nil || !strings.Contains(m.View(), "saved excerpt") || !strings.Contains(m.View(), "Saved · display refresh failed") {
		t.Fatal("saved snapshot lost or mislabeled", m.View())
	}
	next, _ := m.quitTranscript()
	m = next.(Model)
	if len(m.reasoning.items) != 0 || m.browser.page != nil || m.reasoning.stream.Pending() {
		t.Fatal("exit retained failed sample")
	}
}

func TestReasoningRejectsUnknownSampleAndDoesNotInventFailureActivity(t *testing.T) {
	m := reasoningModel(t)
	resolution := protocol.NewUncommittedSampleResolution(0)
	if err := m.resolveReasoning(protocol.TurnFailedPayload{Resolution: &resolution}); err != nil {
		t.Fatal(err)
	}
	if m.reasoning.status != "" || m.reasoning.failed {
		t.Fatal("failure without reasoning invented activity")
	}
	event, err := protocol.BindReasoningSample(mustReasoningStart(t), 2)
	if err != nil {
		t.Fatal(err)
	}
	event.TurnID = m.activeTurnID
	if err := m.acceptReasoning(event); err == nil {
		t.Fatal("unknown sample accepted")
	}
	m = reasoningUIStart(t, m, 0)
	m = reasoningUIComplete(t, m, 0, "")
	presentation, _ := domain.NewSamplePresentation(1, []domain.ReasoningBinding{{Block: 0, Item: 0, Source: domain.ReasoningOpenAISummary, Availability: domain.ReasoningUnavailable}})
	commit, _ := protocol.NewSampleTranscriptCommitted(1, 0, presentation)
	payload, _ := protocol.DecodeTranscriptCommitted(commit)
	m, _, err = m.commitReasoning(payload)
	if err != nil {
		t.Fatal(err)
	}
	event = mustReasoningStart(t)
	event.TurnID = m.activeTurnID
	if err := m.acceptReasoning(event); err == nil {
		t.Fatal("stale sample accepted after commit")
	}
}

func TestReasoningReaderCancellationRemainsAboveLateBoundaries(t *testing.T) {
	m := reasoningModel(t)
	m = reasoningUIStart(t, m, 0)
	m = reasoningUIDelta(t, m, 0, 0, "body")
	m.detailOpen = true
	m.mergeLiveReasoning()
	m.browser.selected = 0
	m.browser.detail = true
	m.prepareLiveReasoningPage()
	m, _ = browserKey(t, m, tea.KeyCtrlC)
	if !strings.Contains(m.View(), "Cancelling") || strings.Contains(m.View(), "Thinking") {
		t.Fatal("reader concealed cancellation", m.View())
	}
	m = reasoningUIComplete(t, m, 0, "body")
	if !strings.Contains(m.View(), "Cancelling") || strings.Contains(m.View(), "waiting to save") {
		t.Fatal("late boundary covered cancellation", m.View())
	}
}
