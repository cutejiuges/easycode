package plugin

import (
	"context"
	"easycode/internal/extension"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func fixture(t *testing.T, kind string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS(filepath.Join("testdata", kind))); err != nil {
		t.Fatal(err)
	}
	return root
}
func put(t *testing.T, root, relative, text string) {
	t.Helper()
	file := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
}
func load(t *testing.T, options Options, dirs ...string) (*Sources, []extension.PluginSource, []extension.Contribution) {
	t.Helper()
	sources := NewSources(options)
	t.Cleanup(func() {
		if err := sources.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	cwd, _ := os.Getwd()
	views, contributions, err := sources.Load(context.Background(), cwd, dirs)
	if err != nil {
		t.Fatal(err)
	}
	return sources, views, contributions
}

func TestManifestStrictSubsetAndSafeIdentity(t *testing.T) {
	valid := `{"name":"qa","version":"0.1","description":"review","author":{"name":"Team","email":"local@example.test","url":"https://invalid.example"},"homepage":"https://invalid.example","repository":"https://invalid.example/repo","license":"MIT","keywords":["review"],"skills":["./workflows","./skills/review","./workflows"]}`
	m, err := parseManifest([]byte(valid), "claude")
	if err != nil || m.ID != "qa" || m.Default || !reflect.DeepEqual(m.Roots, []string{"skills/review", "workflows"}) {
		t.Fatalf("manifest: %#v %v", m, err)
	}
	for _, tt := range []struct{ raw, code, id string }{
		{`{"name":"qa","commands":[]}`, "plugin_unsupported_contribution", "qa"},
		{`{"name":"qa","hooks":{}}`, "plugin_unsupported_contribution", "qa"},
		{`{"name":"qa","mcpServers":{}}`, "plugin_unsupported_contribution", "qa"},
		{`{"name":"qa","unknown":true}`, "plugin_unsupported_field", "qa"},
		{`{"name":"qa","name":"other"}`, "plugin_manifest_invalid", ""},
		{`{"name":"qa","author":{"name":"one","name":"two"}}`, "plugin_manifest_invalid", ""},
		{`{"name":"qa","author":{"token":"secret"}}`, "plugin_manifest_invalid", "qa"},
		{`{"name":"qa","author":{"name":null}}`, "plugin_manifest_invalid", "qa"},
		{`{"name":"qa","version":""}`, "plugin_manifest_invalid", "qa"},
		{`{"name":"Qa"}`, "plugin_manifest_invalid", ""},
		{`{"name":"qa"} {}`, "plugin_manifest_invalid", ""},
		{`{"name":"qa","description":"\ud800"}`, "plugin_manifest_invalid", ""},
		{`{"name":"qa","skills":[]}`, "plugin_root_limit", "qa"},
		{`{"name":"qa","skills":"../bad"}`, "plugin_manifest_invalid", "qa"},
		{`{"name":"qa","skills":"./a/../b"}`, "plugin_manifest_invalid", "qa"},
		{`{"name":"qa","skills":"./a//b"}`, "plugin_manifest_invalid", "qa"},
		{`{"name":"qa","skills":"./a\\b"}`, "plugin_manifest_invalid", "qa"},
		{`{"name":"qa","skills":"./C:/a"}`, "plugin_manifest_invalid", "qa"},
		{`{"name":"qa","skills":"./."}`, "plugin_manifest_invalid", "qa"},
		{`{"name":"qa","skills":"/absolute"}`, "plugin_manifest_invalid", "qa"},
		{`{"name":"qa","description":"` + strings.Repeat("x", 1025) + `"}`, "plugin_manifest_invalid", "qa"},
		{`{"name":"qa","keywords":["` + strings.Repeat("x", 65) + `"]}`, "plugin_manifest_invalid", "qa"},
		{`{"name":"qa","x":` + strings.Repeat("[", 16) + `0` + strings.Repeat("]", 16) + `}`, "plugin_manifest_invalid", ""},
		{strings.Repeat(" ", 64<<10) + `{"name":"qa"}`, "plugin_manifest_invalid", ""},
	} {
		got, err := parseManifest([]byte(tt.raw), "claude")
		if err == nil || err.Error() != tt.code || got.ID != tt.id {
			t.Fatalf("%s: id=%q err=%v", tt.code, got.ID, err)
		}
	}
	for _, field := range []string{"agents", "commands", "mcp_servers", "outputStyles", "lspServers", "apps"} {
		if _, err := parseManifest([]byte(`{"name":"qa","`+field+`":{}}`), "codex"); err == nil || err.Error() != "plugin_unsupported_contribution" {
			t.Fatalf("unsupported %s: %v", field, err)
		}
	}
}

func TestManifestPriorityAndNoFallback(t *testing.T) {
	root := fixture(t, "claude")
	put(t, root, ".easycode-plugin/plugin.json", `{"name":"preferred"}`)
	_, views, cs := load(t, Options{}, root, root+"/.")
	if len(views) != 1 || len(cs) != 1 || views[0].ID != "preferred" || cs[0].Source.Kind != "easycode" {
		t.Fatalf("priority/dedupe %#v %#v", views, cs)
	}
	for _, tt := range []struct{ path, raw, code string }{
		{".easycode-plugin/plugin.json", "{", "plugin_manifest_invalid"},
		{"plugin.json", `{"name":"other"}`, "plugin_manifest_unsupported"},
		{"plugin.json", `{"$schema":"https://unrelated.invalid","name":"other"}`, "plugin_manifest_unsupported"},
	} {
		dir := fixture(t, "claude")
		put(t, dir, tt.path, tt.raw)
		_, views, cs := load(t, Options{}, dir)
		if len(cs) != 0 || views[0].Code != tt.code {
			t.Fatalf("fallback %#v", views)
		}
	}
	dir := fixture(t, "claude")
	if err := os.Mkdir(filepath.Join(dir, ".easycode-plugin"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, ".claude-plugin", "plugin.json"), filepath.Join(dir, ".easycode-plugin", "plugin.json")); err != nil {
		t.Fatal(err)
	}
	_, views, cs = load(t, Options{}, dir)
	if len(cs) != 0 || views[0].Code != "plugin_path_unsafe" {
		t.Fatal("unsafe preferred fallback")
	}
	_, views, cs = load(t, Options{}, t.TempDir())
	if len(cs) != 0 || views[0].Code != "plugin_manifest_missing" {
		t.Fatal(views)
	}
}

func TestAllFixedFormatsAndConflictReservation(t *testing.T) {
	for _, kind := range []string{"easycode", "claude", "codex", "agentplugins"} {
		_, views, cs := load(t, Options{}, fixture(t, kind))
		if len(views) != 1 || len(cs) != 1 || cs[0].Source.Kind != kind || cs[0].Revision == "" {
			t.Fatalf("%s: %#v %#v", kind, views, cs)
		}
	}
	first, second := fixture(t, "claude"), fixture(t, "claude")
	put(t, second, ".claude-plugin/plugin.json", `{"name":"claudeqa","hooks":{}}`)
	for _, dirs := range [][]string{{first, second}, {second, first}} {
		_, views, cs := load(t, Options{}, dirs...)
		if len(cs) != 0 || len(views) != 2 || !views[0].Conflict || !views[1].Conflict {
			t.Fatal("implicit duplicate winner", views)
		}
	}
}

func TestExplicitLoadCancellationAndSingleLifecycle(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	root := fixture(t, "claude")
	s := NewSources(Options{BeforeOpen: func(value string) {
		if value == ".claude-plugin/plugin.json" {
			cancel()
		}
	}})
	cwd, _ := os.Getwd()
	views, cs, err := s.Load(ctx, cwd, []string{root})
	if err != context.Canceled || views != nil || cs != nil {
		t.Fatalf("cancelled load: %v", err)
	}
	if len(s.roots) != 0 || len(s.active) != 0 {
		t.Fatal("cancelled load retained resources")
	}
	if err := s.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.Load(context.Background(), cwd, []string{root}); err == nil {
		t.Fatal("reloaded closed source")
	}
	for _, dirs := range [][]string{{""}, {strings.Repeat("x", 4097)}, {string([]byte{0xff})}, make([]string, 33)} {
		if ValidateDirectories(dirs) == nil {
			t.Fatal("invalid arguments")
		}
	}
}
