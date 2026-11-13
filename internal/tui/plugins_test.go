package tui

import (
	"context"
	"easycode/internal/domain"
	"easycode/internal/protocol"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func pluginModel(t *testing.T, draft string) Model {
	t.Helper()
	source, _ := domain.NewSkillSource("plugin", "qa", "codex")
	m, _ := domain.NewSkillMetadata(source, "qa:review", "Review changes", "[scope]", true, true)
	skills := []domain.SkillListEntry{{Name: m.Name, Source: source, Metadata: m, Status: "available"}, {Name: "qa:broken", Source: source, Status: "invalid", Code: "skill_unsupported_field"}}
	entry, err := domain.NewPluginListEntry(domain.PluginListEntry{ID: "qa", Format: "codex", Version: "0.1.0", Description: "Review workflows", Argument: 1, Status: domain.PluginPartial, Available: 1, Diagnostics: []domain.PluginDiagnostic{{Skill: "qa:broken", Code: "skill_unsupported_field"}}})
	if err != nil {
		t.Fatal(err)
	}
	model := NewModel(context.Background(), "test", &fakeChatSession{}).WithSkills(skills).WithPlugins([]domain.PluginListEntry{entry})
	if err := model.composer.replace(0, 0, draft); err != nil {
		t.Fatal(err)
	}
	return model
}

func TestPluginsFrozenSnapshotsAndLocalNavigation(t *testing.T) {
	for _, tc := range []struct {
		name          string
		width, height int
		detail, empty bool
	}{
		{"plugins", 80, 24, false, false}, {"plugins_detail", 80, 24, true, false}, {"plugins_narrow", 28, 10, false, false}, {"plugins_detail_narrow", 28, 10, true, false}, {"plugins_empty", 80, 24, false, true}, {"plugins_tiny", 12, 2, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := pluginModel(t, "/plugins")
			if tc.empty {
				m = m.WithPlugins(nil)
			}
			m = updateModel(t, m, tea.WindowSizeMsg{Width: tc.width, Height: tc.height})
			m, cmd := updateModelWithCommand(t, m, tea.KeyMsg{Type: tea.KeyEnter})
			if cmd != nil || !m.plugins.open || len(m.session.(*fakeChatSession).submits) != 0 {
				t.Fatal("browser executed work")
			}
			if tc.detail {
				m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyEnter})
			}
			got := m.View()
			if tc.width == 28 && (!strings.Contains(got, "Esc:back") || !strings.Contains(got, "^C:stop")) {
				t.Fatal("narrow panel hid back or stop controls")
			}
			file := filepath.Join("testdata", tc.name+".golden")
			if os.Getenv("EASYCODE_UPDATE_PLUGIN_SNAPSHOTS") == "1" {
				if err := os.WriteFile(file, []byte(got), 0600); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			if got != string(want) {
				t.Fatalf("snapshot mismatch:\n%s", got)
			}
			for _, line := range strings.Split(got, "\n") {
				if ansi.StringWidth(line) > tc.width {
					t.Fatal("line exceeds window", line)
				}
			}
			if tc.detail {
				m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyEsc})
				if !m.plugins.open || m.plugins.detail {
					t.Fatal("Esc skipped list")
				}
			}
			m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyEsc})
			if m.plugins.open || m.composer.text != "/plugins" || m.completion.rows != 0 {
				t.Fatal("draft or focus lost")
			}
		})
	}
}

func TestPluginSelectionAndActualDraftSurviveResize(t *testing.T) {
	for _, draft := range []string{"/plug", " \n /plugins \n\t"} {
		m := pluginModel(t, draft)
		cursor := m.composer.cursor
		m = updateModel(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
		m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyEnter})
		if !m.plugins.open {
			t.Fatal("missing browser")
		}
		m = updateModel(t, m, tea.WindowSizeMsg{Width: 28, Height: 10})
		m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyEnter})
		m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyEsc})
		m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyEsc})
		if m.composer.text != draft || m.composer.cursor != cursor || m.composer.width != 28 || m.composer.height != 10 || m.completion.rows != 0 {
			t.Fatal("restored a different draft or old layout")
		}
		if err := m.composer.replace(0, len(m.composer.text), "/plugins"); err != nil {
			t.Fatal(err)
		}
		m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyEnter})
		m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyEnter})
		m, cmd := updateModelWithCommand(t, m, tea.KeyMsg{Type: tea.KeyEnter})
		if cmd != nil || m.plugins.open || m.composer.text != "/qa:review " || m.composer.width != 28 || len(m.session.(*fakeChatSession).submits) != 0 {
			t.Fatal("selection did not only fill invocation")
		}
	}
}

func TestPluginsRejectDisabledSelectionPasteAndUnsafeMetadata(t *testing.T) {
	m := pluginModel(t, "/plugins")
	m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	for _, key := range []tea.KeyType{tea.KeyEnter, tea.KeyEsc, tea.KeyCtrlC, tea.KeyTab} {
		m = updateModel(t, m, tea.KeyMsg{Type: key, Runes: []rune("\x1b\x03\n"), Paste: true})
		if !m.plugins.detail || !m.plugins.open || m.composer.text != "/plugins" {
			t.Fatal("paste handled as panel key")
		}
	}
	m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyDown})
	m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if !m.plugins.open || m.composer.text != "/plugins" {
		t.Fatal("disabled skill accepted")
	}
	m.plugins.skill = 0
	m.skills[0].Metadata.UserInvocable = false
	m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if !m.plugins.open {
		t.Fatal("model-only skill accepted")
	}
	m.plugins.entries[0].Description = "unsafe\x1b[2J\u202e\nline"
	view := m.View()
	if strings.ContainsAny(view, "\x1b\u202e") || !strings.Contains(view, `\u001b`) {
		t.Fatal("unsafe terminal metadata", view)
	}
}

func TestReservedBareSkillsUseDollarAndQualifiedCompletionBounds(t *testing.T) {
	for _, name := range []string{"cache", "compact", "context", "hooks", "skills", "plugins"} {
		entry := completionSkillEntry(t, name, "Reserved skill")
		m := NewModel(context.Background(), "test", &fakeChatSession{}).WithSkills([]domain.SkillListEntry{entry})
		m.skillsOpen = true
		m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyEnter})
		if m.composer.text != "$"+name+" " || m.plugins.open || m.skillsOpen {
			t.Fatal("reserved name dispatched builtin", name)
		}
	}
	for _, tc := range []struct {
		token string
		ok    bool
	}{{"/qa:", true}, {"$qa:re", true}, {"/" + strings.Repeat("a", 64) + ":" + strings.Repeat("b", 64), true}, {"/:review", false}, {"/qa:bad:leaf", false}, {"/qa:" + strings.Repeat("b", 65), false}, {"/qa:../file", false}} {
		if _, ok := findCompletionTarget(tc.token, len(tc.token)); ok != tc.ok {
			t.Fatalf("target %s: %v", tc.token, ok)
		}
	}
	m := pluginModel(t, "  /qa:re old\n尾部  ")
	m.composer.moveTo(len("  /qa:re"))
	m.prepareCompletion()
	m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyTab})
	if m.composer.text != "  /qa:review old\n尾部  " {
		t.Fatal("qualified completion lost arguments", fmt.Sprintf("%q", m.composer.text))
	}
}

func TestPluginPanelStopsActiveTurn(t *testing.T) {
	m := pluginModel(t, "/plugins")
	m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	m.activeTurnID = "01900000-0000-7000-8000-000000000001"
	m, cmd := updateModelWithCommand(t, m, tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil || !m.cancelling || m.plugins.open {
		t.Fatal("panel swallowed stop")
	}
	cmd()
	if m.session.(*fakeChatSession).interrupts != 1 {
		t.Fatal("stop not delivered exactly once")
	}
}

func TestPluginStatusSnapshots(t *testing.T) {
	for _, state := range []domain.PluginStatus{domain.PluginAvailable, domain.PluginInvalid, domain.PluginConflict, domain.PluginUnavailable} {
		m := pluginModel(t, "/plugins")
		entry := m.plugins.entries[0].Clone()
		entry.Status = state
		entry.Diagnostics = nil
		if state != domain.PluginAvailable {
			entry.Available = 0
			code := map[domain.PluginStatus]string{domain.PluginInvalid: "plugin_manifest_invalid", domain.PluginConflict: "plugin_name_conflict", domain.PluginUnavailable: "plugin_root_unavailable"}[state]
			entry.Diagnostics = []domain.PluginDiagnostic{{Code: code}}
		}
		m = m.WithPlugins([]domain.PluginListEntry{entry})
		m.plugins.open = true
		file := filepath.Join("testdata", "plugins_"+string(state)+".golden")
		if os.Getenv("EASYCODE_UPDATE_PLUGIN_SNAPSHOTS") == "1" {
			if err := os.WriteFile(file, []byte(m.View()), 0600); err != nil {
				t.Fatal(err)
			}
		}
		want, err := os.ReadFile(file)
		if err != nil || m.View() != string(want) {
			t.Fatalf("%s snapshot: %v\n%s", state, err, m.View())
		}
	}
}

func TestQualifiedCompletionRejectsOversizeAtomically(t *testing.T) {
	prefix := "/qa:r "
	m := pluginModel(t, prefix+strings.Repeat("x", protocol.MaxInputBytes-len(prefix)))
	m.composer.moveTo(len(prefix) - 1)
	m.prepareCompletion()
	before := m.composer
	m = updateModel(t, m, tea.KeyMsg{Type: tea.KeyTab})
	if !reflect.DeepEqual(before, m.composer) || m.pending != nil || !strings.Contains(m.errorSummary, "input exceeds") {
		t.Fatal("oversized qualified completion changed draft")
	}
}
