package tui

import (
	"context"
	"easycode/internal/domain"
	"easycode/internal/protocol"
	tea "github.com/charmbracelet/bubbletea"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func skillEntries(t *testing.T) []domain.SkillListEntry {
	t.Helper()
	source, _ := domain.NewSkillSource("project", ".", "easycode")
	metadata, _ := domain.NewSkillMetadata(source, "demo", "Review changes", "", true, false)
	return []domain.SkillListEntry{{Metadata: metadata, Name: "demo", Source: source, Status: "available"}, {Name: "broken", Source: source, Status: "invalid", Code: "skill_unsupported_field"}, {Metadata: metadata, Name: "demo", Source: source, Status: "shadowed"}}
}
func TestSkillsPickerSnapshotsAndNoSubmission(t *testing.T) {
	for _, tc := range []struct {
		name  string
		width int
		empty bool
	}{{"skills", 80, false}, {"skills_narrow", 28, false}, {"skills_empty", 80, true}} {
		t.Run(tc.name, func(t *testing.T) {
			session := &fakeChatSession{}
			model := NewModel(context.Background(), "test", session)
			if !tc.empty {
				model = model.WithSkills(skillEntries(t))
			}
			model = updateModel(t, model, tea.WindowSizeMsg{Width: tc.width, Height: 24})
			if err := model.composer.replace(0, 0, "/skills"); err != nil {
				t.Fatal(err)
			}
			updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
			model = updated.(Model)
			if cmd != nil || !model.skillsOpen || len(session.submits) != 0 {
				t.Fatal("picker submitted or started I/O")
			}
			want, err := os.ReadFile(filepath.Join("testdata", tc.name+".golden"))
			if err != nil {
				t.Fatal(err)
			}
			if model.View()+"\n" != string(want) {
				t.Fatalf("snapshot mismatch:\n%s", model.View())
			}
			model = updateModel(t, model, tea.KeyMsg{Type: tea.KeyEsc})
			if model.composer.text != "/skills" || model.skillsOpen {
				t.Fatal("escape lost draft")
			}
			if !tc.empty {
				model.skillsOpen = true
				updated, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
				model = updated.(Model)
				if cmd != nil || model.composer.text != "/demo " || model.skillsOpen || len(session.submits) != 0 {
					t.Fatal("selection did not only fill composer")
				}
			}
		})
	}
}

// cancellingSkillSession 模拟准备阶段收到取消，取消后不能进入 admission。
type cancellingSkillSession struct {
	fakeChatSession
	admitted bool
}

func (s *cancellingSkillSession) Append(ctx context.Context, text string) (protocol.CommandResult, error) {
	if err := ctx.Err(); err != nil {
		return protocol.CommandResult{}, err
	}
	s.admitted = true
	return s.fakeChatSession.Append(ctx, text)
}
func TestSkillPreparationCancellationAndStaleResponse(t *testing.T) {
	session := &cancellingSkillSession{}
	model := NewModel(context.Background(), "test", session)
	model.composer.replace(0, 0, "/demo args")
	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(Model)
	model = updateModel(t, model, tea.KeyMsg{Type: tea.KeyEsc})
	message := cmd()
	model = updateModel(t, model, message)
	if session.admitted || model.composer.text != "/demo args" || model.pending != nil {
		t.Fatal("cancelled preparation admitted or lost draft")
	}
	model.submissionGeneration++
	model.composer.replace(0, len(model.composer.text), "new draft")
	model = updateModel(t, model, message)
	if model.composer.text != "new draft" {
		t.Fatal("stale failure overwrote draft")
	}
}

func TestSkillsDisplayEscapesMetadata(t *testing.T) {
	entries := skillEntries(t)
	entries[0].Metadata.Description = "unsafe\x1b[2J\nline"
	entries[0].Metadata.ArgumentHint = "\x1b]52;c;data\a"
	model := NewModel(context.Background(), "test", &fakeChatSession{}).WithSkills(entries)
	model.skillsOpen = true
	view := model.View()
	if strings.ContainsAny(view, "\x1b\a") {
		t.Fatal("terminal controls escaped metadata display boundary")
	}
	if !strings.Contains(view, `\u001b`) {
		t.Fatal("metadata controls were not visibly escaped")
	}
}
