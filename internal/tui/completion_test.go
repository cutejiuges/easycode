package tui

import (
	"context"
	"testing"

	"easycode/internal/domain"
)

func completionSkillEntry(t testing.TB, name, description string) domain.SkillListEntry {
	t.Helper()
	source, err := domain.NewSkillSource("project", ".", "easycode")
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := domain.NewSkillMetadata(source, name, description, "[args]", true, false)
	if err != nil {
		t.Fatal(err)
	}
	return domain.SkillListEntry{Name: name, Source: source, Metadata: metadata, Status: "available"}
}

func completionModel(t testing.TB, text string) Model {
	t.Helper()
	m := NewModel(context.Background(), "test", &fakeChatSession{}).WithSkills([]domain.SkillListEntry{completionSkillEntry(t, "review", "Review changes")})
	if err := m.composer.replace(0, 0, text); err != nil {
		t.Fatal(err)
	}
	m.prepareCompletion()
	return m
}
