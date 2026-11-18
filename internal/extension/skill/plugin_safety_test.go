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
