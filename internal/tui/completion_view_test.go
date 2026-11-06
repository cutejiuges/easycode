package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unsafe"

	"easycode/internal/domain"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestCompletionFixedSnapshotsAndSafeLayout(t *testing.T) {
	for _, size := range []tea.WindowSizeMsg{{Width: 80, Height: 24}, {Width: 28, Height: 10}, {Width: 20, Height: 5}, {Width: 1, Height: 1}} {
		for _, color := range []bool{false, true} {
			name := fmt.Sprintf("completion_%dx%d_color%t", size.Width, size.Height, color)
			t.Run(name, func(t *testing.T) {
				m := completionModel(t, "/").WithMarkdownColor(color)
				m = updateModel(t, m, size)
				if m.completion.rows > 0 {
					for i := 0; i < 5; i++ {
						m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyDown})
					}
				}
				got := m.View()
				path := filepath.Join("testdata", name+".golden")
				if os.Getenv("EASYCODE_UPDATE_COMPLETION_SNAPSHOTS") == "1" {
					if err := os.WriteFile(path, []byte(got), 0600); err != nil {
						t.Fatal(err)
					}
				}
				want, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if got != string(want) {
					t.Fatalf("snapshot mismatch:\n%s", got)
				}
				for _, line := range strings.Split(got, "\n") {
					if ansi.StringWidth(line) > size.Width {
						t.Fatalf("line exceeds width %d: %q", size.Width, line)
					}
				}
				if m.completion.rows > 0 && !strings.Contains(m.completion.view, "> /review") {
					t.Fatal("selected candidate is invisible")
				}
				if size.Height <= 5 && m.completion.rows != 0 {
					t.Fatal("tiny terminal kept active choices")
				}
			})
		}
	}
	e := completionSkillEntry(t, "review", "unsafe\x1b[2J\n中文\u202e emoji 👩‍💻 "+strings.Repeat("x", 800))
	e.Metadata.ArgumentHint = "[\x1b]52;c;bad\a\targs]"
	m := completionModel(t, "$").WithSkills([]domain.SkillListEntry{e})
	m.prepareCompletion()
	if strings.ContainsAny(m.completion.view, "\x1b\a\u202e") || !strings.Contains(m.completion.view, `\u001b`) {
		t.Fatal("unsafe display metadata escaped", m.completion.view)
	}
	if m.composer.text != "$" {
		t.Fatal("display metadata entered draft")
	}
	m = updateModel(t, m, tea.WindowSizeMsg{Width: 28, Height: 10})
	if !strings.Contains(m.completion.view, "[skill project") {
		t.Fatal("long argument hint hid the Skill type or source", m.completion.view)
	}
	for _, empty := range []bool{false, true} {
		x := completionModel(t, "$")
		if empty {
			x = x.WithSkills(nil)
		}
		x.prepareCompletion()
		if (x.completion.rows == 0) != empty {
			t.Fatal("empty Skill directory visibility")
		}
	}
}

func TestCompletionResizeBudgetAndViewport(t *testing.T) {
	m := completionModel(t, "/")
	m.composer.history.add("old")
	before := m.composer
	identity := m.completion.catalog.entries[m.completion.matches[m.completion.selected].index].id
	with := m.transcriptHeight()
	rows := m.completion.rows
	x := m
	x.completion.rows = 0
	if x.transcriptHeight()-with != rows {
		t.Fatal("candidate rows not subtracted")
	}
	m = updateModel(t, m, tea.WindowSizeMsg{Width: 20, Height: 5})
	if m.composer.text != before.text || m.composer.cursor != before.cursor || m.completion.rows != 0 {
		t.Fatal("resize changed draft")
	}
	m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyTab})
	if m.composer.text != before.text {
		t.Fatal("invisible choice accepted")
	}
	m = updateModel(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	if m.completion.catalog.entries[m.completion.matches[m.completion.selected].index].id != identity {
		t.Fatal("resize changed identity")
	}
	// 超大列宽只改变当前受限布局，不按列宽填充或保留历史布局。
	m.width = 1 << 30
	m.prepareCompletion()
	if len(m.completion.view) > 6*maxCompletionLineBytes || m.completion.rows > 6 {
		t.Fatal("display exceeded budget")
	}
	cost := int(unsafe.Sizeof(m.completion)) + cap(m.completion.matches)*int(unsafe.Sizeof(completionMatch{})) + len(m.completion.view)
	catalog := m.completion.catalog
	cost += cap(catalog.entries) * int(unsafe.Sizeof(completionCandidate{}))
	for _, entry := range catalog.entries {
		cost += len(entry.description) + len(entry.searchDescription) + len(entry.origin) + len(entry.hint) + len(entry.id.name)
	}
	if cost > maxCompletionBytes || cost > catalog.bytes+6*maxCompletionLineBytes {
		t.Fatal("retained cost escaped conservative budget", cost)
	}
}

func TestCompletionTargetDoesNotRetainDraftBackingStorage(t *testing.T) {
	text := "/rev " + strings.Repeat("x", 1<<20)
	target, ok := findCompletionTarget(text, 4)
	if !ok {
		t.Fatal("valid target rejected")
	}
	start := uintptr(unsafe.Pointer(unsafe.StringData(text)))
	for _, value := range []string{target.token, target.query} {
		address := uintptr(unsafe.Pointer(unsafe.StringData(value)))
		if address >= start && address < start+uintptr(len(text)) {
			t.Fatal("short completion token retains the full draft")
		}
	}
	if text[:4] != "/rev" {
		t.Fatal("target extraction changed the draft")
	}
}

func BenchmarkCompletion(b *testing.B) {
	for _, count := range []int{0, 256} {
		var entries []domain.SkillListEntry
		for i := 0; i < count; i++ {
			entries = append(entries, completionSkillEntry(b, fmt.Sprintf("skill-%03d", i), "Review repository changes"))
		}
		catalog, err := newCompletionCatalog(entries)
		if err != nil {
			b.Fatal(err)
		}
		for _, query := range []string{"/", "$sk", "$" + strings.Repeat("a", 64)} {
			b.Run(fmt.Sprintf("match/%d/%d", count, len(query)), func(b *testing.B) {
				target, _ := findCompletionTarget(query, len(query))
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					catalog.match(target)
				}
			})
		}
	}
	for _, length := range []int{1024, 4 << 20} {
		b.Run(fmt.Sprintf("large-draft/%d", length), func(b *testing.B) {
			text := "/rev " + strings.Repeat("x", length-5)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				findCompletionTarget(text, 4)
			}
		})
	}
	b.Run("unchanged-stream-update", func(b *testing.B) {
		m := completionModel(b, "/con")
		m.prepareCompletion()
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			m.prepareCompletion()
		}
	})
}
