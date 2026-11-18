package skill

import (
	"context"
	"easycode/internal/domain"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeSkill(t *testing.T, root, kind, name, body string) string {
	t.Helper()
	p := filepath.Join(root, "."+kind, "skills", name, "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestPriorityFrozenRevisionAndExplicitResourcePermission(t *testing.T) {
	ctx := context.Background()
	root, home := t.TempDir(), t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	high := writeSkill(t, root, "easycode", "demo", "---\ndescription: safe\ndisable-model-invocation: true\n---\nTask $ARGUMENTS")
	writeSkill(t, root, "claude", "demo", "---\ndescription: lower\n---\nLower")
	if err := os.WriteFile(filepath.Join(filepath.Dir(high), "reference.md"), []byte("reference"), 0600); err != nil {
		t.Fatal(err)
	}
	l := NewLocal()
	if err := l.Load(ctx, root, home, nil, nil); err != nil {
		t.Fatal(err)
	}
	if l.Catalog().HasEntries() || len(l.Entries()) != 2 || l.Entries()[1].Status != "shadowed" {
		t.Fatal(l.Entries())
	}
	a, ok, err := l.Prepare(ctx, " /demo exact args ")
	if err != nil || !ok || a.Arguments != "exact args" {
		t.Fatalf("prepare %v %v", ok, err)
	}
	for _, resource := range []string{"", "reference.md"} {
		if _, err := l.Read(ctx, "demo", "", resource, nil); err == nil {
			t.Fatal("model bypassed manual flag")
		}
	}
	if _, err := l.Read(ctx, "demo", "", "", []domain.SkillAttachment{a}); err == nil {
		t.Fatal("explicit selection allowed model main load")
	}
	if v, err := l.Read(ctx, "demo", "", "reference.md", []domain.SkillAttachment{a}); err != nil || !strings.Contains(v.Content, "reference") {
		t.Fatalf("explicit resource %v", err)
	}
	other := a
	other.Source.Kind = "agents"
	if _, err := l.Read(ctx, "demo", "", "reference.md", []domain.SkillAttachment{other}); err == nil {
		t.Fatal("cross source permission")
	}
	if err := os.WriteFile(high, []byte("---\ndescription: changed\n---\nnew"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := l.Prepare(ctx, "$demo"); ErrorCode(err) != "skill_source_changed" {
		t.Fatalf("revision: %v", err)
	}
	if _, err := l.Read(ctx, "demo", "", "reference.md", []domain.SkillAttachment{a}); err == nil {
		t.Fatal("changed revision permission")
	}
}
