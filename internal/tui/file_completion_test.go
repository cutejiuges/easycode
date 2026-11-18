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
	"github.com/rivo/uniseg"
)

type fileQueryFixture struct {
	queries []domain.FileSearchQuery
	cancels []domain.FileSearchIdentity
	results chan domain.FileSearchSnapshot
}

func (f *fileQueryFixture) QueryFiles(q domain.FileSearchQuery) error {
	f.queries = append(f.queries, q)
	return nil
}
func (f *fileQueryFixture) CancelFiles(id domain.FileSearchIdentity) {
	f.cancels = append(f.cancels, id)
}
func (f *fileQueryFixture) FileResults() <-chan domain.FileSearchSnapshot { return f.results }
func fileCompletionModel(t *testing.T, text string) (Model, *fileQueryFixture) {
	t.Helper()
	f := &fileQueryFixture{results: make(chan domain.FileSearchSnapshot, 8)}
	m := NewModel(context.Background(), "test", &fakeChatSession{}).WithFileQueries(f)
	if err := m.composer.replace(0, 0, text); err != nil {
		t.Fatal(err)
	}
	m.prepareCompletion()
	return m, f
}
func fileReply(t *testing.T, m Model, id domain.FileSearchIdentity, revision uint64, paths []string, state domain.FileSearchState, report domain.FileDiscoveryReport) Model {
	t.Helper()
	snapshot, err := domain.NewFileSearchSnapshot(id, revision, paths, state, report)
	if err != nil {
		t.Fatal(err)
	}
	return updateModel(t, m, fileResultMsg{generation: m.files.workspace, snapshot: snapshot})
}

func TestFileCompletionAcceptOnlyFillsAndSecondEnterSubmits(t *testing.T) {
	for _, key := range []tea.KeyType{tea.KeyEnter, tea.KeyTab} {
		m, f := fileCompletionModel(t, "请看 @sr")
		m = fileReply(t, m, m.files.identity, 1, []string{"src/a b#c.go"}, domain.FileSearchComplete, domain.FileDiscoveryReport{})
		m, cmd := updateModelWithCommand(t, m, tea.KeyMsg{Type: key})
		want := "请看 @\"src/a b#c.go\" "
		if cmd != nil || m.pending != nil || m.composer.text != want || m.composer.cursor != len(want) || m.files.rows != 0 || len(f.cancels) != 1 || len(m.session.(*fakeChatSession).submits) != 0 {
			t.Fatal("acceptance executed or misplaced cursor", m.composer.text)
		}
		m, cmd = updateModelWithCommand(t, m, tea.KeyMsg{Type: tea.KeyEnter})
		if cmd == nil || m.pending == nil {
			t.Fatal("second Enter did not independently submit")
		}
		cmd()
		if !reflect.DeepEqual(m.session.(*fakeChatSession).submits, []string{want}) {
			t.Fatal("submission changed raw text")
		}
	}
}

func TestFileCompletionRangeSeparatorAndAtomicFailure(t *testing.T) {
	for _, path := range []string{"@start", "a b", "a#b", "a\"b", "a`b", "目录/文.go", strings.Repeat("a", 4096)} {
		for _, separator := range []string{"", " \u0301", "\u3000\ufe0f", "\t", "\n"} {
			text := "before\n@old#L2-9" + separator + "tail"
			if separator == "" {
				text = "before\n@old#L2-9"
			}
			c := composerWithText(t, text)
			c.moveTo(len("before\n@ol"))
			target, ok := domain.FindFileMentionTarget(c.text, c.cursor)
			if !ok {
				t.Fatal("missing target")
			}
			if err := c.completeFile(target, path); err != nil {
				t.Fatal(err)
			}
			rendered, _ := domain.RenderFilePath(path)
			prefix := "before\n@" + rendered + "#L2-9"
			want, cursor := prefix+separator+"tail", len(prefix)
			if separator == "" {
				want = prefix + " "
				cursor++
			} else {
				g := uniseg.NewGraphemes(separator + "tail")
				g.Next()
				cursor += len(g.Str())
			}
			if c.text != want || c.cursor != cursor {
				t.Fatalf("range/separator changed: %q %d", c.text, c.cursor)
			}
			refs, err := domain.ParseFileReferences(c.text)
			if err != nil || len(refs) != 1 || refs[0].Path() != path || refs[0].Selection().EndLine() != 9 {
				t.Fatal("accepted path does not round trip", err)
			}
		}
	}
	c := composerWithText(t, "@a "+strings.Repeat("x", protocol.MaxInputBytes-3))
	c.moveTo(2)
	target, _ := domain.FindFileMentionTarget(c.text, c.cursor)
	before := c
	if c.completeFile(target, "longer") == nil || !reflect.DeepEqual(before, c) {
		t.Fatal("over-limit completion was not atomic")
	}
}

func TestFileCompletionRejectsOldRowsABAAndOldIndexRevision(t *testing.T) {
	m, f := fileCompletionModel(t, "@a")
	a := m.files.identity
	waiter := m.files.waiter
	m = fileReply(t, m, a, 2, []string{"a-first", "a-next"}, domain.FileSearchProgress, domain.FileDiscoveryReport{})
	m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyDown})
	m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("b")})
	b := m.files.identity
	if m.files.ready || len(m.files.paths) != 0 || a.SessionGeneration != b.SessionGeneration || a.RequestGeneration == b.RequestGeneration {
		t.Fatal("pending query left stale rows or restarted session")
	}
	m = fileReply(t, m, a, 3, []string{"a-wrong"}, domain.FileSearchComplete, domain.FileDiscoveryReport{})
	m, cmd := updateModelWithCommand(t, m, tea.KeyMsg{Type: tea.KeyTab})
	if cmd != nil || m.composer.text != "@ab" || m.files.ready {
		t.Fatal("stale Tab wrote wrong path")
	}
	m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyBackspace})
	newest := m.files.identity
	m = fileReply(t, m, a, 4, []string{"a-wrong"}, domain.FileSearchComplete, domain.FileDiscoveryReport{})
	if m.files.ready {
		t.Fatal("ABA revived old request")
	}
	m = fileReply(t, m, newest, 3, []string{"a-first", "a-next"}, domain.FileSearchComplete, domain.FileDiscoveryReport{})
	if m.files.selected != 1 {
		t.Fatal("path selection was not preserved")
	}
	m = fileReply(t, m, newest, 2, []string{"a-old-index"}, domain.FileSearchComplete, domain.FileDiscoveryReport{})
	if m.files.paths[1] != "a-next" || len(f.queries) != 3 || waiter == nil {
		t.Fatal("old index replaced newer snapshot")
	}
	beforeWaiter := m.files.waiter
	m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyLeft})
	if m.files.waiter != beforeWaiter {
		t.Fatal("key started another result waiter")
	}
}

func TestFileCompletionSuppressionLoadingEnterAndBuiltinArguments(t *testing.T) {
	for _, text := range []string{"/cache @a", "/context @a", "/compact @a", "/effort @a", "/skills @a", "/plugins @a", "/hooks @a", "email@host", "`@code`"} {
		m, f := fileCompletionModel(t, text)
		if len(f.queries) != 0 || m.files.rows != 0 {
			t.Fatal("literal or local argument searched", text)
		}
	}
	m, f := fileCompletionModel(t, "$effort @a")
	if len(f.queries) != 1 {
		t.Fatal("explicit Skill could not contain file")
	}
	id := m.files.identity
	m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	m = fileReply(t, m, id, 1, []string{"a"}, domain.FileSearchComplete, domain.FileDiscoveryReport{})
	if m.files.rows != 0 {
		t.Fatal("dismissed search revived")
	}
	m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyTab})
	if m.files.session == id.SessionGeneration || m.composer.text != "$effort @a" {
		t.Fatal("reopen did not create fresh session")
	}
	m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(" pasted"), Paste: true})
	if m.files.rows != 0 || len(f.cancels) < 2 {
		t.Fatal("paste kept completion")
	}
	for _, state := range []domain.FileSearchState{domain.FileSearchProgress, domain.FileSearchComplete, domain.FileSearchFailed} {
		x, _ := fileCompletionModel(t, "@manual")
		report := domain.FileDiscoveryReport{}
		if state == domain.FileSearchFailed {
			report.Failure = domain.FileUnavailable
		}
		x = fileReply(t, x, x.files.identity, 0, nil, state, report)
		x, cmd := updateModelWithCommand(t, x, tea.KeyMsg{Type: tea.KeyEnter})
		if cmd == nil || x.pending == nil || x.composer.text != "@manual" {
			t.Fatal("empty/loading/error Enter did not submit raw draft", state)
		}
	}
}

func TestPastedFileRangeSuffixKeepsSearchSuppressedUntilExplicitTab(t *testing.T) {
	m, f := fileCompletionModel(t, "")
	for _, part := range []string{"请看 @sr", "#L2-3 tail"} {
		m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(part), Paste: true})
	}
	m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyCtrlA})
	for i := 0; i < 6; i++ {
		m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyRight})
	}
	if len(f.queries) != 0 || m.files.rows != 0 || m.composer.cursor != len("请看 @sr") {
		t.Fatal("pasted target searched before explicit reopening")
	}
	m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyTab})
	if len(f.queries) != 1 || m.files.identity.Target.Query != "sr" || m.composer.text != "请看 @sr#L2-3 tail" {
		t.Fatal("explicit reopening changed the pasted range or started duplicate searches")
	}
}

func TestFileCompletionRebindWaiterAndPendingActiveCancellation(t *testing.T) {
	m, f := fileCompletionModel(t, "@a")
	old := m.files.waiter
	oldWait := m.initialFileWait()
	fresh := &fileQueryFixture{results: make(chan domain.FileSearchSnapshot, 1)}
	m = m.WithFileQueries(fresh)
	if m.files.waiter != old || !old.stopped || len(f.cancels) != 1 {
		t.Fatal("rebind abandoned old waiter")
	}
	msg := oldWait().(fileResultMsg)
	m, cmd := updateModelWithCommand(t, m, msg)
	if cmd == nil || m.files.waiter == old || m.files.waiter.generation != m.files.workspace {
		t.Fatal("new waiter did not follow old completion")
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.pendingCancel = cancel
	c := m.composer
	m.pending = &c
	m.activeTurnID = "01900000-0000-7000-8000-000000000001"
	m, cmd = updateModelWithCommand(t, m, tea.KeyMsg{Type: tea.KeyCtrlC})
	if ctx.Err() == nil || cmd == nil || !m.cancelling {
		t.Fatal("active stop did not cancel pending preparation")
	}
	m.closeFileQueries()
	if !m.files.waiter.stopped {
		t.Fatal("quit retained waiter")
	}
}

func TestFileCompletionSnapshots(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {28, 10}, {20, 5}, {1, 1}} {
		for _, state := range []string{"complete", "loading", "incomplete", "error"} {
			m, _ := fileCompletionModel(t, "参考 @sr")
			report := domain.FileDiscoveryReport{}
			s := domain.FileSearchState(state)
			paths := []string{"src/a b.go", "src/文.go", "src/c.go", "src/d.go", "src/e.go", "src/f.go"}
			if state == "loading" {
				s = domain.FileSearchProgress
				paths = nil
			}
			if state == "incomplete" {
				report.Issues = domain.FileDiscoveryIgnoreUnavailable
			}
			if state == "error" {
				s = domain.FileSearchFailed
				report.Failure = domain.FileUnavailable
				paths = nil
			}
			m = fileReply(t, m, m.files.identity, 1, paths, s, report)
			m = updateModel(t, m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			got := m.View()
			name := fmt.Sprintf("file_completion_%dx%d_%s.golden", size[0], size[1], state)
			want, err := os.ReadFile(filepath.Join("testdata", name))
			if err != nil {
				t.Fatal(err)
			}
			if got != string(want) {
				t.Fatalf("%s mismatch:\n%s", name, got)
			}
			for _, line := range strings.Split(strings.TrimSuffix(got, "\n"), "\n") {
				if ansi.StringWidth(line) > size[0] {
					t.Fatal("overflow", line)
				}
			}
			if m.files.visible > 5 || strings.Contains(got, "\x1b") {
				t.Fatal("too many rows or unexpected color")
			}
			if m.files.visible == 0 {
				before := m.composer.text
				m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyTab})
				if m.composer.text != before {
					t.Fatal("hidden row accepted")
				}
			}
		}
	}
}
