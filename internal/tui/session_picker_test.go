package tui

import (
	"context"
	"easycode/internal/domain"
	tea "github.com/charmbracelet/bubbletea"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
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
func TestPickerSearchEditingAndIdentity(t *testing.T) {
	m, _ := pickerTestModel(t)
	m = pickerKey(m, tea.KeyDown)
	selected := m.selected
	m = pickerUpdate(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("CLAUDE")})
	if m.selected != selected || len(m.filtered()) != 1 {
		t.Fatal("case insensitive identity search failed")
	}
	m = pickerKey(m, tea.KeyEsc)
	if m.selected != selected || m.query != "" {
		t.Fatal("selection changed when clearing search")
	}
	m = pickerUpdate(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("👩‍💻")})
	m = pickerKey(m, tea.KeyBackspace)
	if m.query != "" {
		t.Fatal("backspace split grapheme")
	}
	m = pickerUpdate(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a\r\nb\tc"), Paste: true})
	if m.query != "a  b c" {
		t.Fatal("paste normalization failed")
	}
	m.query = strings.Repeat("中", 255)
	m = pickerUpdate(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("🙂a")})
	if utf8.RuneCountInString(m.query) != 255 {
		t.Fatal("oversized edit partially accepted")
	}
	m = pickerUpdate(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("🙂")})
	if utf8.RuneCountInString(m.query) != 256 {
		t.Fatal("boundary edit rejected")
	}
}
func TestPickerStaleScopePreviewAndResume(t *testing.T) {
	m, h := pickerTestModel(t)
	original := m.request
	m = pickerKey(m, tea.KeyCtrlA)
	if m.loaded || !m.all {
		t.Fatal("scope did not invalidate results")
	}
	pending := m.request
	m = pickerUpdate(m, PickerResult{Request: original, Error: "old failure"})
	if !m.busy || m.failure != "" {
		t.Fatal("stale result overrode loading")
	}
	m = pickerUpdate(m, PickerResult{Request: pending, Error: "new failure"})
	before := len(h.requests)
	m = pickerKey(m, tea.KeyEnter)
	if len(h.requests) != before {
		t.Fatal("stale scope resumed")
	}
	m = pickerKey(m, tea.KeyCtrlR)
	m = pickerUpdate(m, PickerResult{Request: m.request, Entries: m.entries})
	m = pickerKey(m, tea.KeyCtrlV)
	previewRequest := m.request
	m = pickerKey(m, tea.KeyEsc)
	m = pickerUpdate(m, PickerResult{Request: previewRequest, Preview: SessionPreview{Messages: []PreviewMessage{{Message: domain.SemanticMessage{Text: "secret"}}}}})
	if m.preview != nil || m.previewOpen || !h.cancelled {
		t.Fatal("late preview retained")
	}
	m = pickerKey(m, tea.KeyEnter)
	before = len(h.requests)
	m = pickerKey(m, tea.KeyEnter)
	if len(h.requests) != before || m.request.Operation != PickerResume {
		t.Fatal("duplicate resume")
	}
	m = pickerKey(m, tea.KeyEsc)
	if m.exitCode != 130 || !h.stopped {
		t.Fatal("resuming escape did not cancel startup")
	}
}
func TestPickerCompatibilityAndCrossDirectory(t *testing.T) {
	m, h := pickerTestModel(t)
	m = pickerKey(m, tea.KeyDown)
	n := len(h.requests)
	m = pickerKey(m, tea.KeyEnter)
	if len(h.requests) != n || m.failure == "" {
		t.Fatal("incompatible accepted")
	}
	m = pickerKey(m, tea.KeyUp)
	m.entries[0].CWD = "/other"
	m = pickerKey(m, tea.KeyEnter)
	if !m.confirm || m.confirmProceed {
		t.Fatal("confirmation must default to return")
	}
	m = pickerKey(m, tea.KeyEnter)
	if len(h.requests) != n {
		t.Fatal("default confirmation resumed")
	}
	m = pickerKey(m, tea.KeyEnter)
	m = pickerKey(m, tea.KeyRight)
	m = pickerKey(m, tea.KeyEnter)
	if m.request.Operation != PickerResume {
		t.Fatal("explicit confirmation failed")
	}
}
func TestPickerSafeRenderingAndBudget(t *testing.T) {
	m, _ := pickerTestModel(t)
	m.entries[0].Model = "model\x1b[2J\r\x00\u202e"
	m.hasMore = true
	for _, size := range []tea.WindowSizeMsg{{Width: 80, Height: 24}, {Width: 20, Height: 12}, {Width: 1, Height: 1}} {
		m = pickerUpdate(m, size)
		view := m.View()
		if strings.ContainsAny(view, "\x1b\r\x00\u202e") || len(strings.Split(view, "\n")) > size.Height {
			t.Fatal("unsafe or overflowing view")
		}
	}
	body := previewBody(SessionPreview{Messages: []PreviewMessage{{Message: domain.SemanticMessage{Text: strings.Repeat("\x00", 32768)}}}})
	if len(body) > 196608 || !utf8.ValidString(body) || strings.ContainsRune(body, 0) {
		t.Fatal("unsafe preview expansion")
	}
}

func TestPickerTinyDetailsExposeFullIdentity(t *testing.T) {
	m, _ := pickerTestModel(t)
	m = pickerUpdate(m, tea.WindowSizeMsg{Width: 1, Height: 1})
	m = pickerKey(m, tea.KeyTab)
	var visible strings.Builder
	for range len(string(m.selected)) {
		visible.WriteString(m.View())
		m = pickerKey(m, tea.KeyDown)
	}
	if visible.String() != string(m.selected) {
		t.Fatalf("identity inaccessible: %q", visible.String())
	}
	m = pickerKey(m, tea.KeyEsc)
	if m.details {
		t.Fatal("details not closed")
	}
}
func pickerSnapshots(t *testing.T) map[string]StartupModel {
	m, _ := pickerTestModel(t)
	m.config.Location = time.UTC
	m.entries[0].UpdatedAt = time.Date(2026, 11, 4, 12, 34, 56, 0, time.UTC)
	m.entries[1].UpdatedAt = m.entries[0].UpdatedAt
	m.entries[0].Model = "模型🙂"
	m.entries[0].CWD = "/工作区/\x1b[2J"
	m.hasMore = true
	m = pickerUpdate(m, tea.WindowSizeMsg{Width: 80, Height: 24})
	result := map[string]StartupModel{"picker_normal": m}
	result["picker_narrow"] = pickerUpdate(m, tea.WindowSizeMsg{Width: 20, Height: 12})
	result["picker_tiny"] = pickerUpdate(m, tea.WindowSizeMsg{Width: 1, Height: 1})
	m.previewOpen = true
	m.preview = &SessionPreview{Incomplete: true, Truncated: true, Messages: []PreviewMessage{{TurnID: "01900000-0000-7000-8000-000000000010", Message: domain.SemanticMessage{Role: domain.SemanticUser, InputID: "01900000-0000-7000-8000-000000000011", Text: "中文🙂\n\x1b[2J"}}}}
	result["picker_preview"] = m
	return result
}
func TestPickerSnapshots(t *testing.T) {
	// 时间格式固定，测试不依赖运行机器的时区。
	for name, m := range pickerSnapshots(t) {
		for i := range m.entries {
			m.entries[i].UpdatedAt = m.entries[i].UpdatedAt.In(time.Local)
		}
		got := m.View()
		want, err := os.ReadFile(filepath.Join("testdata", name+".golden"))
		if err != nil {
			t.Fatal(err)
		}
		if strings.TrimSuffix(string(want), "\n") != got {
			t.Fatalf("%s snapshot changed:\n%s", name, got)
		}
	}
}

func TestPickerNormalExitSurvivesOwnerClosedRace(t *testing.T) {
	m, h := pickerTestModel(t)
	m = pickerKey(m, tea.KeyEsc)
	if !h.stopped || m.exitCode != 0 {
		t.Fatal("normal escape failed")
	}
	m = pickerUpdate(m, pickerClosed{})
	if m.exitCode != 0 {
		t.Fatal("owner completion changed successful cancellation to failure")
	}
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

func TestPickerReviewPreviewDisplayBoundaries(t *testing.T) {
	for _, gap := range []int{0, 1, 18} {
		// 第一条安全展示恰好用尽预算或留下不足一个省略标记的空间。
		first := PreviewMessage{Message: domain.SemanticMessage{Role: domain.SemanticUser}}
		headerBytes := len(previewBody(SessionPreview{Messages: []PreviewMessage{first}}))
		size := 196608 - headerBytes - gap
		first.Message.Text = strings.Repeat("\x00", size/6) + strings.Repeat("a", size%6)
		body := previewBody(SessionPreview{Messages: []PreviewMessage{first, {Message: domain.SemanticMessage{Role: domain.SemanticAssistant, Text: "tail"}}}})
		if len(body) > 196608 || !utf8.ValidString(body) || !strings.HasSuffix(body, "[Preview truncated]") {
			t.Fatalf("missing bounded omission marker, gap=%d bytes=%d", gap, len(body))
		}
	}
	body := previewBody(SessionPreview{Messages: []PreviewMessage{{Truncated: true, Message: domain.SemanticMessage{Text: "line\n\tcell\x1b\u2028\u2029"}}}})
	if !strings.Contains(body, "line\n\tcell\\u001b\\u2028\\u2029") || !strings.Contains(body, "[Message truncated]") {
		t.Fatalf("layout or per-message marker lost: %q", body)
	}
	if got := pickerSafe("\n\t\u2028\u2029", 256); got != "\\u000a\\u0009\\u2028\\u2029" {
		t.Fatalf("unsafe metadata: %q", got)
	}
}

func TestPickerReviewLoadingAndFailureViews(t *testing.T) {
	m := NewStartupModel(&pickerTestHost{}, PickerConfig{})
	m = pickerUpdate(m, pickerStart{})
	if strings.Contains(m.View(), "No sessions") {
		t.Fatal("loading pretends to be empty")
	}
	m = pickerUpdate(m, PickerResult{Request: m.request, Error: "Session discovery failed"})
	if strings.Contains(m.View(), "No sessions") {
		t.Fatal("failure pretends to be empty")
	}
	m = pickerUpdate(m, tea.WindowSizeMsg{Width: 40, Height: 3})
	if !strings.Contains(m.View(), "Session discovery failed") {
		t.Fatal("short viewport hides error")
	}
	m, _ = pickerTestModel(t)
	m = pickerKey(m, tea.KeyCtrlA)
	if !strings.Contains(m.View(), "Previous results: Current directory") {
		t.Fatal("stale scope not identified")
	}
}

func TestPickerConfirmationScrollKeepsPathsAndChoiceAccessible(t *testing.T) {
	m, _ := pickerTestModel(t)
	m.entries[0].CWD = "/origin/unique-directory"
	m.config.CWD = "/current/another-directory"
	m = pickerUpdate(m, tea.WindowSizeMsg{Width: 30, Height: 3})
	m = pickerKey(m, tea.KeyEnter)
	var seen strings.Builder
	for range 20 {
		view := m.View()
		if !strings.HasSuffix(view, "> Return     Continue") || len(strings.Split(view, "\n")) > 3 {
			t.Fatal("choice hidden or viewport overflow")
		}
		seen.WriteString(view)
		m = pickerKey(m, tea.KeyDown)
	}
	if !strings.Contains(seen.String(), m.config.CWD) || !strings.Contains(seen.String(), m.entries[0].CWD) {
		t.Fatal("full paths inaccessible")
	}
	m = pickerKey(m, tea.KeyTab)
	if !strings.HasSuffix(m.View(), "> Continue   Return") {
		t.Fatal("selected continuation hidden")
	}
}

func TestPickerExcessiveSearchShowsLimitAndPreservesSelection(t *testing.T) {
	m, _ := pickerTestModel(t)
	m.query = strings.Repeat("x", 256)
	before := m.selected
	m = pickerUpdate(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("overflow"), Paste: true})
	if m.query != strings.Repeat("x", 256) || m.selected != before || !strings.Contains(m.View(), "limited to 256") {
		t.Fatal("limit rejection not explicit or state changed")
	}
	m = pickerKey(m, tea.KeyBackspace)
	if strings.Contains(m.View(), "limited to 256") {
		t.Fatal("corrected input retains stale limit error")
	}
}

func TestPickerTinyPreviewCanScrollPastHeaders(t *testing.T) {
	m, _ := pickerTestModel(t)
	m = pickerKey(m, tea.KeyCtrlV)
	m = pickerUpdate(m, PickerResult{Request: m.request, Preview: SessionPreview{Incomplete: true, Truncated: true, Messages: []PreviewMessage{{Message: domain.SemanticMessage{Text: "visible-body"}}}}})
	m = pickerUpdate(m, tea.WindowSizeMsg{Width: 30, Height: 1})
	seen := ""
	for range 15 {
		seen += m.View() + "\n"
		m = pickerKey(m, tea.KeyDown)
	}
	if !strings.Contains(seen, "visible-body") || !strings.Contains(seen, "Preview truncated") {
		t.Fatal("short preview cannot reach body or status")
	}
}
