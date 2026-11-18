//go:build darwin || linux

package builtin

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"easycode/internal/domain"
)

func discoverFixtureFiles(t *testing.T, workspace *Workspace) ([]string, domain.FileDiscoveryReport, error) {
	t.Helper()
	var paths []string
	report, err := workspace.DiscoverFileNames(context.Background(), func(batch []string, _ domain.FileDiscoveryReport) (bool, error) {
		if len(batch) > 256 {
			t.Error("unbounded publication")
		}
		paths = append(paths, batch...)
		return true, nil
	})
	sort.Strings(paths)
	return paths, report, err
}

func TestFileDiscoveryLocalRuleOrderPruningAndNoBodyReads(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "workspace")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(parent, ".gitignore"), []byte("*\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		".gitignore": "*.tmp\nbuild/\n!build/keep.go\n!keep.tmp\n",
		".ignore":    "keep.tmp\n", ".rgignore": "!keep.tmp\n",
		".hidden": "hidden", "keep.tmp": "keep", "drop.tmp": "drop", "build/keep.go": "pruned",
		"build/.gitignore": "!keep.go\n", "src/.gitignore": "*.go\n!keep.go\n",
		"src/keep.go": "keep", "src/drop.go": "drop", "z.go": "after nested scope",
		".git/config": "must-not-open", ".git/info/exclude": "*\n", "binary.dat": "\x00\xff",
	}
	for name, content := range files {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	workspace, err := OpenWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	defer workspace.Close()
	readBodies := 0
	workspace.hooks.afterFileRead = func(*os.File) { readBodies++ }
	workspace.hooks.beforeOpenComponent = func(path string) {
		if strings.HasPrefix(path, ".git/") {
			t.Error("read VCS state")
		}
	}
	paths, report, err := discoverFixtureFiles(t, workspace)
	want := []string{".gitignore", ".hidden", ".ignore", ".rgignore", "binary.dat", "keep.tmp", "src/.gitignore", "src/keep.go", "z.go"}
	if err != nil || strings.Join(paths, ",") != strings.Join(want, ",") || report.Issues != 0 {
		t.Fatalf("local discovery %v %+v %v", paths, report, err)
	}
	if readBodies != 4 {
		t.Fatalf("expected four rule bodies only, got %d", readBodies)
	}
	workspace.hooks = workspaceHooks{}
	if content, _, err := workspace.ReadWorkspaceFile(context.Background(), "drop.tmp", domain.MaxFileSourceBytes); err != nil || content != "drop" {
		t.Fatal("ignore became a read permission", err)
	}
	if _, _, err := workspace.ReadWorkspaceFile(context.Background(), "binary.dat", domain.MaxFileSourceBytes); err == nil {
		t.Fatal("filename promised valid body")
	}
}

func TestFileDiscoveryFailsClosedOnUnsafeAndOverBudgetRules(t *testing.T) {
	for _, scenario := range []string{"symlink", "oversized", "invalid_utf8", "invalid_pattern", "rule_count"} {
		t.Run(scenario, func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "visible"), []byte("v"), 0o600); err != nil {
				t.Fatal(err)
			}
			content := "[broken\n"
			switch scenario {
			case "symlink":
				if err := os.Symlink(filepath.Join(t.TempDir(), "missing"), filepath.Join(root, ".gitignore")); err != nil {
					t.Fatal(err)
				}
			case "oversized":
				content = strings.Repeat("a", (64<<10)+1)
			case "invalid_utf8":
				content = string([]byte{0xff})
			case "rule_count":
				content = strings.Repeat("a\n", 4097)
			}
			if scenario != "symlink" {
				if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			workspace, err := OpenWorkspace(root)
			if err != nil {
				t.Fatal(err)
			}
			defer workspace.Close()
			paths, report, err := discoverFixtureFiles(t, workspace)
			if err != nil || len(paths) != 0 || report.Issues&domain.FileDiscoveryIgnoreUnavailable == 0 || report.RuleBytes > 512<<10 || report.RuleCount > 4096 {
				t.Fatal("unsafe rules silently ignored", paths, report, err)
			}
		})
	}
}

func TestFileDiscoveryOmitsWholeOverfullDirectoryAndUnsupportedNames(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 4097; i++ {
		if err := os.WriteFile(filepath.Join(root, fmt.Sprintf("file-%04d", i)), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	workspace, err := OpenWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	paths, report, err := discoverFixtureFiles(t, workspace)
	_ = workspace.Close()
	if err != nil || len(paths) != 0 || report.Visited != 4097 || report.Issues&domain.FileDiscoveryDirectoryLimit == 0 {
		t.Fatal("published random partial directory", len(paths), report, err)
	}
	root = t.TempDir()
	for _, name := range []string{"good", "bad\\name"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("content"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(root, "good"), filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	workspace, err = OpenWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	defer workspace.Close()
	paths, report, err = discoverFixtureFiles(t, workspace)
	if err != nil || strings.Join(paths, ",") != "good" || report.Unsupported != 2 {
		t.Fatal("unsupported names or symlink indexed", paths, report, err)
	}
	// Darwin 文件系统拒绝创建非法 UTF-8 名称，使用名称枚举边界注入验证过滤本身。
	names := &discoveryNamesDirectory{batchedNamesFixture: &batchedNamesFixture{names: []string{"good", string([]byte{0xff})}}}
	fake := &Workspace{handle: &discoveryNamesHandle{directory: names}}
	paths, report, err = discoverFixtureFiles(t, fake)
	if err != nil || strings.Join(paths, ",") != "good" || report.Unsupported != 1 {
		t.Fatal("invalid UTF-8 name entered index", paths, report, err)
	}
}

type discoveryNamesHandle struct {
	inertWorkspaceHandle
	directory workspaceDirectory
}

func (h *discoveryNamesHandle) openDirectory(string, workspaceHooks) (workspaceDirectory, error) {
	return h.directory, nil
}

type discoveryNamesDirectory struct{ *batchedNamesFixture }

func (d *discoveryNamesDirectory) openChild(name string, _ workspaceHooks) (workspaceChild, error) {
	if name == ".gitignore" || name == ".ignore" || name == ".rgignore" {
		return nil, os.ErrNotExist
	}
	return discoveryRegularChild{}, nil
}

type discoveryRegularChild struct{}

func (discoveryRegularChild) kind() workspaceNodeKind       { return workspaceNodeRegular }
func (discoveryRegularChild) size() int64                   { return 0 }
func (discoveryRegularChild) file() *os.File                { return nil }
func (discoveryRegularChild) directory() workspaceDirectory { return nil }
func (discoveryRegularChild) close() error                  { return nil }

func TestFileDiscoveryCancellationAndIndexStopCloseEveryBorrow(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a"), []byte("body"), 0o600); err != nil {
		t.Fatal(err)
	}
	workspace, err := OpenWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	defer workspace.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, err = workspace.DiscoverFileNames(ctx, func(paths []string, _ domain.FileDiscoveryReport) (bool, error) {
		if len(paths) > 0 {
			cancel()
		}
		return true, nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatal("discovery ignored cancellation", err)
	}
	report, err := workspace.DiscoverFileNames(context.Background(), func([]string, domain.FileDiscoveryReport) (bool, error) { return false, nil })
	if err != nil || report.Issues&domain.FileDiscoveryIndexLimit == 0 {
		t.Fatal("index stop hidden", report, err)
	}
	if _, _, err = workspace.ReadWorkspaceFile(context.Background(), "a", domain.MaxFileSourceBytes); err != nil {
		t.Fatal("discovery released root capability", err)
	}
}
