package skill

import (
	"context"
	"easycode/internal/domain"
	"fmt"
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
func TestParserStrictAndLiteralArguments(t *testing.T) {
	source, _ := domain.NewSkillSource("project", ".", "easycode")
	for _, front := range []string{"description: ok\ndescription: duplicate", "description: [bad]", "description: &a ok", "description: !custom ok", "description: ok\nallowed-tools: Read", "description: ok\ncontext: fork", "description: ok\nmetadata: {a: one, a: two}", "description: ok\nuser-invocable: yes", "description: ok\nname: different", "description: ok\nmetadata: {a: 1}"} {
		t.Run(front, func(t *testing.T) {
			if _, err := parseDocument(source, "demo", "---\n"+front+"\n---\nbody"); err == nil {
				t.Fatal("accepted invalid frontmatter")
			}
		})
	}
	d, err := parseDocument(source, "demo", "---\r\ndescription: |\r\n  multiline\r\n  description\r\nmetadata: {owner: team}\r\n---\r\nRun $ARGUMENTS and !`secret`.")
	if err != nil {
		t.Fatal(err)
	}
	a, err := expand(d, domain.SkillDigest("source"), "$(touch forbidden) $ARGUMENTS")
	if err != nil || !strings.Contains(a.Content, "$(touch forbidden) $ARGUMENTS and !`secret`") {
		t.Fatalf("literal expansion: %v %#v", err, a)
	}
	if _, err := expand(d, domain.SkillDigest("source"), strings.Repeat("x", domain.MaxSkillArgumentBytes+1)); err == nil {
		t.Fatal("unbounded args")
	}
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
func TestInvalidWinnerAndFirstToken(t *testing.T) {
	root, home := t.TempDir(), t.TempDir()
	writeSkill(t, root, "easycode", "demo", "---\ndescription: broken\nhooks: nope\n---\nbody")
	writeSkill(t, home, "claude", "demo", "---\ndescription: lower\n---\nbody")
	l := NewLocal()
	if err := l.Load(context.Background(), root, home, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, _, err := l.Prepare(context.Background(), "/demo"); err == nil {
		t.Fatal("invalid winner fell back")
	}
	for _, text := range []string{"/unknown args", "/tmp/file", "text $demo", "```\n$demo\n```", "/skills", "/context"} {
		if _, ok, err := l.Prepare(context.Background(), text); ok || err != nil {
			t.Fatalf("ordinary %q: %v", text, err)
		}
	}
	if _, _, err := l.Prepare(context.Background(), "$unknown"); ErrorCode(err) != "skill_not_found" {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := l.Load(ctx, root, home, nil, nil); err != context.Canceled {
		t.Fatalf("cancellation %v", err)
	}
}
func TestCatalogStableAcrossAbsoluteRoots(t *testing.T) {
	var revision, text string
	for i := 0; i < 2; i++ {
		root, home := t.TempDir(), t.TempDir()
		writeSkill(t, root, "agents", "zeta", "---\ndescription: last\n---\nbody")
		writeSkill(t, root, "easycode", "alpha", "---\ndescription: first\n---\nbody")
		l := NewLocal()
		if err := l.Load(context.Background(), root, home, nil, nil); err != nil {
			t.Fatal(err)
		}
		c := l.Catalog()
		if i == 0 {
			revision, text = c.Revision(), c.RenderedText()
		} else if revision != c.Revision() || text != c.RenderedText() {
			t.Fatal("absolute root polluted catalog")
		}
		if strings.Contains(c.RenderedText(), root) || strings.Contains(c.RenderedText(), "body") {
			t.Fatal("catalog leaked body or root")
		}
	}
}

func TestDiscoveryLimitsKeepOtherRootsAndDisableMergedCatalog(t *testing.T) {
	root, home := t.TempDir(), t.TempDir()
	over := filepath.Join(root, ".easycode", "skills")
	if err := os.MkdirAll(over, 0700); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 1025; i++ {
		if err := os.WriteFile(filepath.Join(over, fmt.Sprintf("candidate-%04d", i)), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	writeSkill(t, root, "agents", "good", "---\ndescription: valid\n---\nbody")
	local := NewLocal()
	if err := local.Load(context.Background(), root, home, nil, nil); err != nil {
		t.Fatal(err)
	}
	if len(local.Catalog().Entries()) != 1 || local.Entries()[0].Code != "skill_catalog_limit" {
		t.Fatal("oversized root affected other roots")
	}
	root = t.TempDir()
	for i := 0; i < 257; i++ {
		writeSkill(t, root, "easycode", fmt.Sprintf("demo-%03d", i), "---\ndescription: valid\n---\nbody")
	}
	local = NewLocal()
	if err := local.Load(context.Background(), root, home, nil, nil); err != nil {
		t.Fatal(err)
	}
	if local.Catalog().HasEntries() {
		t.Fatal("oversized merged catalog executable")
	}
	if _, ok, err := local.Prepare(context.Background(), "ordinary chat"); err != nil || ok {
		t.Fatal("ordinary chat disabled")
	}
}
func TestModelOnlyAndResourceMainDocumentRejection(t *testing.T) {
	root, home := t.TempDir(), t.TempDir()
	writeSkill(t, root, "easycode", "model-only", "---\ndescription: valid\nuser-invocable: false\n---\nbody")
	writeSkill(t, root, "easycode", "manual", "---\ndescription: manual\ndisable-model-invocation: true\n---\nbody")
	local := NewLocal()
	if err := local.Load(context.Background(), root, home, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, _, err := local.Prepare(context.Background(), "$model-only"); ErrorCode(err) != "skill_not_user_invocable" {
		t.Fatal(err)
	}
	if _, err := local.Read(context.Background(), "model-only", "", "", nil); err != nil {
		t.Fatal(err)
	}
	a, _, err := local.Prepare(context.Background(), "/manual")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"SKILL.md", "skill.md", "../manual/SKILL.md"} {
		if _, err := local.Read(context.Background(), "manual", "", p, []domain.SkillAttachment{a}); err == nil {
			t.Fatal("resource bypassed main document restriction")
		}
	}
}
