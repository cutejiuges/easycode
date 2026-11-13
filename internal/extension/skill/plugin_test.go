package skill

import (
	"context"
	"easycode/internal/domain"
	"easycode/internal/extension/plugin"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func pluginFixture(t *testing.T, kind string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS(filepath.Join("..", "plugin", "testdata", kind))); err != nil {
		t.Fatal(err)
	}
	return root
}
func pluginFile(t *testing.T, root, relative, text string) {
	t.Helper()
	file := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
}
func loadPluginSkills(t *testing.T, project string, roots ...string) (*Local, *plugin.Sources) {
	t.Helper()
	if project == "" {
		project = t.TempDir()
	}
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(project, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	source := plugin.NewSources(plugin.Options{})
	t.Cleanup(func() {
		if err := source.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	views, contributions, err := source.Load(context.Background(), project, roots)
	if err != nil {
		t.Fatal(err)
	}
	l := NewLocal()
	if err := l.Load(context.Background(), project, home, contributions, views); err != nil {
		t.Fatal(err)
	}
	return l, source
}

func TestFixedPluginPackagesShareInvocationAndResourceRules(t *testing.T) {
	roots := []string{pluginFixture(t, "claude"), pluginFixture(t, "codex"), pluginFixture(t, "agentplugins"), pluginFixture(t, "easycode")}
	l, _ := loadPluginSkills(t, "", roots...)
	if len(l.Entries()) != 4 || len(l.Catalog().Entries()) != 3 {
		t.Fatalf("frozen catalog: %#v", l.Entries())
	}
	for _, id := range []string{"claudeqa", "codexqa", "agentqa", "easyqa"} {
		name := id + ":review"
		a, ok, err := l.Prepare(context.Background(), "/"+name+" $(touch forbidden)")
		if err != nil || !ok || a.Name != name || !strings.Contains(a.Content, "$(touch forbidden)") || !strings.Contains(a.Content, "${CLAUDE_PLUGIN_ROOT}") {
			t.Fatalf("prepare %s: %v", name, err)
		}
		b, ok, err := l.Prepare(context.Background(), "$"+name+" $(touch forbidden)")
		if err != nil || !ok || a != b {
			t.Fatalf("spelling %s: %v", name, err)
		}
		_, err = l.Read(context.Background(), name, "", "", nil)
		if (id == "codexqa") != (err != nil) {
			t.Fatalf("effective model policy %s: %v", name, err)
		}
		if id == "codexqa" {
			if _, err := l.Read(context.Background(), name, "", "references/checklist.md", nil); ErrorCode(err) != "skill_model_invocation_disabled" {
				t.Fatal(err)
			}
			if _, err := l.Read(context.Background(), name, "", "", []domain.SkillAttachment{a}); ErrorCode(err) != "skill_model_invocation_disabled" {
				t.Fatal("manual enabled main", err)
			}
		}
		resource, err := l.Read(context.Background(), name, "", "references/checklist.md", []domain.SkillAttachment{a})
		if err != nil || !strings.Contains(resource.Content, "correctness") || resource.Revision != a.Revision {
			t.Fatalf("resource %s: %v", name, err)
		}
		for _, bad := range []string{"../SKILL.md", "/private/file", "SKILL.md", "skill.md", "../../other/guide.md"} {
			if _, err := l.Read(context.Background(), name, "", bad, []domain.SkillAttachment{a}); err == nil {
				t.Fatal("cross-root/main resource accepted", bad)
			}
		}
	}
	for _, text := range []string{"/missing:review", "$missing:review"} {
		if _, _, err := l.Prepare(context.Background(), text); ErrorCode(err) != "skill_not_found" {
			t.Fatal("qualified call fell back", err)
		}
	}
	for _, text := range []string{"/tmp/file", "text $codexqa:review", "/plugins", "/hooks"} {
		if _, ok, err := l.Prepare(context.Background(), text); ok || err != nil {
			t.Fatal("ordinary input", text, err)
		}
	}
	for _, summary := range l.PluginSummaries() {
		if summary.Status != domain.PluginAvailable || summary.Available != 1 {
			t.Fatal(summary)
		}
	}
}

func TestImporterNamesLayoutsAndNoFallback(t *testing.T) {
	for _, kind := range []string{"claude", "codex"} {
		root := pluginFixture(t, kind)
		directory := "skills/review"
		if kind == "codex" {
			directory = "skills/directory-name"
		}
		pluginFile(t, root, directory+"/SKILL.md", "---\nname: other\ndescription: Valid name\n---\nbody")
		l, _ := loadPluginSkills(t, "", root)
		_, ok, err := l.Prepare(context.Background(), "$"+map[string]string{"claude": "claudeqa", "codex": "codexqa"}[kind]+":other")
		if kind == "codex" && (!ok || err != nil) || kind == "claude" && (ok || ErrorCode(err) != "skill_unavailable") {
			t.Fatalf("%s name: %v", kind, err)
		}
	}
	root := pluginFixture(t, "agentplugins-direct")
	l, _ := loadPluginSkills(t, "", root)
	if got := l.PluginSummaries()[0]; got.Status != domain.PluginInvalid || got.Diagnostics[0].Code != "plugin_unsupported_layout" {
		t.Fatal(got)
	}
	root = pluginFixture(t, "claude")
	pluginFile(t, root, "skills/SKILL.md", "broken")
	l, _ = loadPluginSkills(t, "", root)
	if len(l.Entries()) != 1 || l.Entries()[0].Status == "available" {
		t.Fatal("direct root bypassed by collection")
	}
}

func TestSidecarStrictSubsetAndFrozenPresence(t *testing.T) {
	for _, data := range []string{
		"policy: {allow_implicit_invocation: false, allow_implicit_invocation: true}",
		"policy: {allow_implicit_invocation: 'false'}", "policy: {allow_implicit_invocation: null}",
		"policy: {products: []}", "dependencies: {}", "unknown: true", "policy: &p {allow_implicit_invocation: false}",
		"interface: {display_name: !custom text}", "interface: {display_name: &n text, short_description: *n}",
		"interface: {default_prompt: [bad]}", "policy: [bad]", "{}\n---\n{}", "interface: {short_description: '" + strings.Repeat("a", 1025) + "'}",
		"x: " + strings.Repeat("[", 17) + "0" + strings.Repeat("]", 17), strings.Repeat("x", 65537),
	} {
		if _, err := parseSidecar([]byte(data)); err == nil {
			t.Fatalf("accepted sidecar %q", data[:min(100, len(data))])
		}
	}
	for _, mutation := range []string{"add", "remove", "change", "symlink"} {
		t.Run(mutation, func(t *testing.T) {
			root := pluginFixture(t, "codex")
			relative := "skills/directory-name/agents/openai.yaml"
			at := filepath.Join(root, filepath.FromSlash(relative))
			if mutation == "add" {
				if err := os.Remove(at); err != nil {
					t.Fatal(err)
				}
			}
			l, _ := loadPluginSkills(t, "", root)
			a, _, err := l.Prepare(context.Background(), "$codexqa:review before")
			if err != nil {
				t.Fatal(err)
			}
			switch mutation {
			case "add", "change":
				pluginFile(t, root, relative, "policy: {allow_implicit_invocation: true}\n")
			case "remove":
				if err := os.Remove(at); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Remove(at); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Join(root, ".codex-plugin", "plugin.json"), at); err != nil {
					t.Fatal(err)
				}
			}
			if _, _, err := l.Prepare(context.Background(), "$codexqa:review after"); ErrorCode(err) != "skill_source_changed" {
				t.Fatal(err)
			}
			if _, err := l.Read(context.Background(), "codexqa:review", "", "references/checklist.md", []domain.SkillAttachment{a}); ErrorCode(err) != "skill_source_changed" {
				t.Fatal(err)
			}
			if !strings.Contains(a.Content, "before") {
				t.Fatal("prepared input changed")
			}
		})
	}
	root := pluginFixture(t, "codex")
	pluginFile(t, root, "skills/directory-name/SKILL.md", "---\nname: review\ndescription: Manual\ndisable-model-invocation: true\n---\nbody")
	pluginFile(t, root, "skills/directory-name/agents/openai.yaml", "policy: {allow_implicit_invocation: true}\n")
	l, _ := loadPluginSkills(t, "", root)
	if l.Catalog().HasEntries() {
		t.Fatal("sidecar re-enabled model invocation")
	}
}

func TestPluginConflictsHardlinksAndOverlappingRoots(t *testing.T) {
	root := pluginFixture(t, "codex")
	pluginFile(t, root, ".codex-plugin/plugin.json", `{"name":"qa","skills":["./skills","./skills/directory-name"]}`)
	l, _ := loadPluginSkills(t, "", root)
	if len(l.Entries()) != 1 || l.PluginSummaries()[0].Available != 1 {
		t.Fatal("overlapping root not deduplicated", l.Entries())
	}
	pluginFile(t, root, ".codex-plugin/plugin.json", `{"name":"qa","skills":["./skills","./other"]}`)
	pluginFile(t, root, "other/SKILL.md", "temporary")
	if err := os.Remove(filepath.Join(root, "other", "SKILL.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(filepath.Join(root, "skills", "directory-name", "SKILL.md"), filepath.Join(root, "other", "SKILL.md")); err != nil {
		t.Fatal(err)
	}
	pluginFile(t, root, "skills/lint/SKILL.md", "---\nname: lint\ndescription: lint\n---\nLint body")
	l, _ = loadPluginSkills(t, "", root)
	if got := l.PluginSummaries()[0]; got.Status != domain.PluginPartial || got.Available != 1 {
		t.Fatal("hardlink resource roots merged", got)
	}
	if _, _, err := l.Prepare(context.Background(), "$qa:review"); ErrorCode(err) != "skill_unavailable" {
		t.Fatal(err)
	}
	bad := pluginFixture(t, "codex")
	pluginFile(t, bad, ".codex-plugin/plugin.json", `{"name":"qa","commands":{}}`)
	l, _ = loadPluginSkills(t, "", root, bad)
	if _, _, err := l.Prepare(context.Background(), "/qa:never-discovered"); ErrorCode(err) != "skill_unavailable" {
		t.Fatal("failed namespace forgotten", err)
	}
	good := pluginFixture(t, "easycode")
	pluginFile(t, good, ".easycode-plugin/plugin.json", `{"name":"empty","skills":"./missing"}`)
	l, _ = loadPluginSkills(t, "", good)
	if l.PluginSummaries()[0].Status != domain.PluginUnavailable {
		t.Fatal("explicit missing root accepted")
	}
	pluginFile(t, good, ".easycode-plugin/plugin.json", `{"name":"empty"}`)
	if err := os.RemoveAll(filepath.Join(good, "skills")); err != nil {
		t.Fatal(err)
	}
	l, _ = loadPluginSkills(t, "", good)
	if _, _, err := l.Prepare(context.Background(), "$empty:unknown"); ErrorCode(err) != "skill_not_found" {
		t.Fatal("empty namespace unavailable", err)
	}
}

func TestPluginChangesAndPrivateRevisionsDoNotPolluteCatalog(t *testing.T) {
	var content, revision, catalog string
	for i := 0; i < 2; i++ {
		root := pluginFixture(t, "codex")
		pluginFile(t, root, ".codex-plugin/plugin.json", fmt.Sprintf(`{"name":"qa","version":"0.%d"}`, i))
		pluginFile(t, root, "skills/directory-name/agents/openai.yaml", fmt.Sprintf("interface: {display_name: 'Review %d'}\npolicy: {allow_implicit_invocation: true}\n", i))
		l, _ := loadPluginSkills(t, "", root)
		a, _, err := l.Prepare(context.Background(), "$qa:review diff")
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			content, revision, catalog = a.Content, a.Revision, l.Catalog().Revision()
		} else if a.Content != content || a.Revision == revision || l.Catalog().Revision() != catalog {
			t.Fatal("private revision polluted visible content")
		}
		pluginFile(t, root, ".codex-plugin/plugin.json", `{"name":"qa","version":"changed"}`)
		if _, _, err := l.Prepare(context.Background(), "$qa:review diff"); ErrorCode(err) != "skill_source_changed" {
			t.Fatal("manifest not bound", err)
		}
	}
}

func TestPluginBudgetsPreserveLocalSkillsAndFinalCounts(t *testing.T) {
	project := t.TempDir()
	writeSkill(t, project, "easycode", "local", "---\ndescription: Local\n---\nbody")
	root := pluginFixture(t, "easycode")
	if err := os.RemoveAll(filepath.Join(root, "skills")); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 256; i++ {
		name := fmt.Sprintf("item-%03d", i)
		pluginFile(t, root, "skills/"+name+"/SKILL.md", "---\ndescription: Workflow\n---\nbody")
	}
	l, _ := loadPluginSkills(t, project, root)
	if len(l.Catalog().Entries()) != 1 || l.Catalog().Entries()[0].Name != "local" {
		t.Fatal("plugin displaced local skills")
	}
	if got := l.PluginSummaries()[0]; got.Available != 0 || got.Status != domain.PluginUnavailable || got.Diagnostics[0].Code != "plugin_catalog_limit" {
		t.Fatal(got)
	}
	pluginFile(t, root, "skills/item-extra/SKILL.md", "---\ndescription: Extra\n---\nbody")
	l, _ = loadPluginSkills(t, project, root)
	if l.PluginSummaries()[0].Diagnostics[0].Code != "plugin_root_limit" {
		t.Fatal("plugin candidate limit not enforced before catalog admission")
	}
	root = pluginFixture(t, "easycode")
	for i := 0; i < 1025; i++ {
		pluginFile(t, root, fmt.Sprintf("skills/unrelated-%04d", i), "")
	}
	l, _ = loadPluginSkills(t, project, root)
	if got := l.PluginSummaries()[0]; got.Status != domain.PluginUnavailable || got.Diagnostics[0].Code != "plugin_root_limit" {
		t.Fatal("unbounded raw entries", got)
	}
	if len(l.Catalog().Entries()) != 1 {
		t.Fatal("raw budget affected locals")
	}
	copy := l.PluginSummaries()
	copy[0].Diagnostics[0].Code = "modified"
	if l.PluginSummaries()[0].Diagnostics[0].Code == "modified" {
		t.Fatal("summary alias")
	}
}
