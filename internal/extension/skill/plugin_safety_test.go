//go:build darwin || linux

package skill

import (
	"context"
	"easycode/internal/domain"
	"easycode/internal/extension/localfile"
	"easycode/internal/extension/plugin"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPluginReplacementAtEveryReadBoundary(t *testing.T) {
	for _, stage := range []string{"root", "manifest", "directory", "main", "resource"} {
		t.Run(stage, func(t *testing.T) {
			root := pluginFixture(t, "claude")
			target := pluginFixture(t, "claude")
			project, home := t.TempDir(), t.TempDir()
			pluginFile(t, target, "SKILL.md", "secret sentinel")
			pluginFile(t, target, "checklist.md", "secret sentinel")
			armed, fired := false, false
			source := plugin.NewSources(plugin.Options{BeforeOpen: func(value string) {
				if !armed || fired {
					return
				}
				match := map[string]string{"root": localfile.SystemPath(root), "manifest": ".claude-plugin/plugin.json", "directory": "skills/review", "main": "SKILL.md", "resource": "references/checklist.md"}[stage]
				if value != match {
					return
				}
				fired = true
				relative := map[string]string{"root": "", "manifest": ".claude-plugin/plugin.json", "directory": "skills/review", "main": "skills/review/SKILL.md", "resource": "skills/review/references/checklist.md"}[stage]
				at := root
				if relative != "" {
					at = filepath.Join(root, filepath.FromSlash(relative))
				}
				if err := os.Rename(at, at+"-old"); err != nil {
					t.Fatal(err)
				}
				destination := target
				if stage == "manifest" || stage == "main" {
					destination = filepath.Join(target, "SKILL.md")
				}
				if stage == "resource" {
					destination = filepath.Join(target, "checklist.md")
				}
				if err := os.Symlink(destination, at); err != nil {
					t.Fatal(err)
				}
			}})
			t.Cleanup(func() { _ = source.Close(context.Background()) })
			views, contributions, err := source.Load(context.Background(), project, []string{root})
			if err != nil {
				t.Fatal(err)
			}
			l := NewLocal()
			if err := l.Load(context.Background(), project, home, contributions, views); err != nil {
				t.Fatal(err)
			}
			armed = true
			var result domain.SkillAttachment
			if stage == "resource" {
				result, err = l.Read(context.Background(), "claudeqa:review", "", "references/checklist.md", nil)
			} else {
				result, _, err = l.Prepare(context.Background(), "$claudeqa:review diff")
			}
			if !fired || err == nil || strings.Contains(result.Content, "secret sentinel") {
				t.Fatal("unsafe replacement read", stage, err)
			}
			if stage != "resource" && ErrorCode(err) != "skill_source_changed" {
				t.Fatal("changed binding classification", err)
			}
		})
	}
}

func TestPluginSnapshotAndStartupReadBudgetRemainFrozen(t *testing.T) {
	root := pluginFixture(t, "easycode")
	l, source := loadPluginSkills(t, "", root)
	before := l.Catalog().Revision()
	pluginFile(t, root, "skills/added/SKILL.md", "---\ndescription: Added later\n---\nbody")
	if len(l.Entries()) != 1 || l.Catalog().Revision() != before {
		t.Fatal("getter refreshed frozen snapshot")
	}
	if _, _, err := l.Prepare(context.Background(), "$easyqa:added"); ErrorCode(err) != "skill_not_found" {
		t.Fatal(err)
	}
	_ = source
	root = pluginFixture(t, "easycode")
	owner := plugin.NewSources(plugin.Options{})
	defer owner.Close(context.Background())
	views, contributions, err := owner.Load(context.Background(), t.TempDir(), []string{root})
	if err != nil || len(views) != 1 {
		t.Fatal(err)
	}
	main, err := os.ReadFile(filepath.Join(root, "skills", "review", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	budget := &pluginScanBudget{bytes: (16 << 20) - len(main)}
	entries, code := scanPlugin(context.Background(), contributions[0], budget)
	if code != "" || len(entries) != 1 || entries[0].view.Status != "available" || budget.bytes != 16<<20 {
		t.Fatal("exact read budget rejected", code, budget.bytes)
	}
	if _, code := scanPlugin(context.Background(), contributions[0], budget); code != "plugin_root_limit" || budget.bytes != 16<<20 {
		t.Fatal("read budget exceeded", code, budget.bytes)
	}
}

func TestPluginStartupBudgetChargesRejectedMainAndSidecarReads(t *testing.T) {
	for _, kind := range []string{"easycode", "codex"} {
		t.Run(kind, func(t *testing.T) {
			root := pluginFixture(t, kind)
			directory := "review"
			if kind == "codex" {
				directory = "directory-name"
			}
			mainPath := filepath.Join(root, "skills", directory, "SKILL.md")
			main, err := os.ReadFile(mainPath)
			if err != nil {
				t.Fatal(err)
			}
			target, expectedReads, consumed := mainPath, 1, len(main)
			if kind == "codex" {
				target = filepath.Join(root, "skills", directory, "agents", "openai.yaml")
				sidecar, err := os.ReadFile(target)
				if err != nil {
					t.Fatal(err)
				}
				expectedReads, consumed = 2, consumed+len(sidecar)
			}
			reads := 0
			owner := plugin.NewSources(plugin.Options{AfterRead: func() {
				reads++
				if reads == expectedReads {
					file, err := os.OpenFile(target, os.O_APPEND|os.O_WRONLY, 0)
					if err != nil {
						t.Fatal(err)
					}
					_, writeErr := file.WriteString(" ")
					closeErr := file.Close()
					if writeErr != nil || closeErr != nil {
						t.Fatal(writeErr, closeErr)
					}
				}
			}})
			defer owner.Close(context.Background())
			_, contributions, err := owner.Load(context.Background(), t.TempDir(), []string{root})
			if err != nil || len(contributions) != 1 {
				t.Fatal(err)
			}
			budget := &pluginScanBudget{bytes: (16 << 20) - consumed}
			entries, code := scanPlugin(context.Background(), contributions[0], budget)
			if code != "" || len(entries) != 1 || entries[0].view.Status != "invalid" || budget.bytes != 16<<20 || reads != expectedReads {
				t.Fatal("rejected read escaped startup budget", code, budget.bytes, reads)
			}
			if _, code := scanPlugin(context.Background(), contributions[0], budget); code != "plugin_root_limit" || reads != expectedReads {
				t.Fatal("exhausted budget allowed another read", code, reads)
			}
		})
	}
}

func TestPluginMetadataAndDiagnosticsBudgetsAreAtomic(t *testing.T) {
	project := t.TempDir()
	writeSkill(t, project, "easycode", "local", "---\ndescription: Local\n---\nbody")
	root := pluginFixture(t, "easycode")
	for i := 0; i < 70; i++ {
		pluginFile(t, root, fmt.Sprintf("skills/workflow-%03d/SKILL.md", i), "---\ndescription: "+strings.Repeat("d", 1024)+"\n---\nbody")
	}
	l, _ := loadPluginSkills(t, project, root)
	if len(l.Catalog().Entries()) != 1 || l.PluginSummaries()[0].Diagnostics[0].Code != "plugin_catalog_limit" {
		t.Fatal("metadata budget published a partial plugin")
	}
	var roots []string
	for pluginIndex := 0; pluginIndex < 2; pluginIndex++ {
		dir := pluginFixture(t, "easycode")
		pluginFile(t, dir, ".easycode-plugin/plugin.json", fmt.Sprintf(`{"name":"invalid-%d"}`, pluginIndex))
		if err := os.RemoveAll(filepath.Join(dir, "skills")); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 256; i++ {
			pluginFile(t, dir, fmt.Sprintf("skills/workflow-%03d/SKILL.md", i), "---\ndescription: broken\ncontext: fork\n---\nbody")
		}
		roots = append(roots, dir)
	}
	l, _ = loadPluginSkills(t, project, roots...)
	count, omitted := 0, 0
	for _, summary := range l.PluginSummaries() {
		count += len(summary.Diagnostics)
		omitted += summary.Omitted
		if summary.Status != domain.PluginInvalid {
			t.Fatal(summary)
		}
	}
	if count != 256 || omitted != 256 || len(l.Catalog().Entries()) != 1 {
		t.Fatal("unbounded or misleading diagnostics", count, omitted)
	}
}

func TestPluginGlobalEnumerationAndCandidateLimits(t *testing.T) {
	root := pluginFixture(t, "easycode")
	owner := plugin.NewSources(plugin.Options{})
	defer owner.Close(context.Background())
	_, cs, err := owner.Load(context.Background(), t.TempDir(), []string{root})
	if err != nil {
		t.Fatal(err)
	}
	for _, budget := range []*pluginScanBudget{{enumerated: 8192}, {candidates: 1024}, {bytes: 16 << 20}} {
		entries, code := scanPlugin(context.Background(), cs[0], budget)
		if code != "plugin_root_limit" || len(entries) > 1 || budget.enumerated > 8193 || budget.candidates > 1025 || budget.bytes > 16<<20 {
			t.Fatal("unbounded global scan", code, *budget)
		}
	}
	for i := 0; i < 1023; i++ {
		pluginFile(t, root, fmt.Sprintf("skills/ordinary-%04d", i), "")
	}
	entries, code := scanPlugin(context.Background(), cs[0], &pluginScanBudget{})
	if code != "" || len(entries) != 1 {
		t.Fatal("exact raw enumeration limit rejected", code)
	}
	pluginFile(t, root, "skills/ordinary-extra", "")
	entries, code = scanPlugin(context.Background(), cs[0], &pluginScanBudget{})
	if code != "plugin_root_limit" || len(entries) != 0 {
		t.Fatal("raw enumeration overflow published partial results", code)
	}
}

func TestManualPluginResourceCannotAliasMainDocument(t *testing.T) {
	root := pluginFixture(t, "codex")
	dir := filepath.Join(root, "skills", "directory-name")
	if err := os.Link(filepath.Join(dir, "SKILL.md"), filepath.Join(dir, "reference-alias.md")); err != nil {
		t.Fatal(err)
	}
	l, _ := loadPluginSkills(t, "", root)
	a, _, err := l.Prepare(context.Background(), "$codexqa:review")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.Read(context.Background(), "codexqa:review", "", "reference-alias.md", []domain.SkillAttachment{a}); ErrorCode(err) != "skill_resource_invalid" {
		t.Fatal("main document reloaded through resource alias", err)
	}
}

func TestPluginPerSourceRawBudgetAndCancelledPublication(t *testing.T) {
	root := pluginFixture(t, "easycode")
	pluginFile(t, root, ".easycode-plugin/plugin.json", `{"name":"bounded","skills":["./first","./second","./third"]}`)
	for _, directory := range []string{"first", "second", "third"} {
		for i := 0; i < 1024; i++ {
			pluginFile(t, root, fmt.Sprintf("%s/ordinary-%04d", directory, i), "")
		}
	}
	l, _ := loadPluginSkills(t, "", root)
	if got := l.PluginSummaries()[0]; got.Status != domain.PluginUnavailable || got.Diagnostics[0].Code != "plugin_root_limit" {
		t.Fatal("per-plugin raw enumeration unbounded", got)
	}
	project, home := t.TempDir(), t.TempDir()
	writeSkill(t, project, "easycode", "local", "---\ndescription: Local\n---\nbody")
	l = NewLocal()
	if err := l.Load(context.Background(), project, home, nil, nil); err != nil {
		t.Fatal(err)
	}
	before := l.Catalog().Revision()
	ctx, cancel := context.WithCancel(context.Background())
	armed := false
	owner := plugin.NewSources(plugin.Options{BeforeOpen: func(value string) {
		if armed && value == "SKILL.md" {
			cancel()
		}
	}})
	defer owner.Close(context.Background())
	views, cs, err := owner.Load(ctx, project, []string{pluginFixture(t, "easycode")})
	if err != nil {
		t.Fatal(err)
	}
	armed = true
	if err := l.Load(ctx, project, home, cs, views); err != context.Canceled || l.Catalog().Revision() != before || len(l.Entries()) != 1 || len(l.PluginSummaries()) != 0 {
		t.Fatal("cancelled load published partial snapshot", err)
	}
}
