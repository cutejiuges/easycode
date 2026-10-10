package builtin

import (
	"strings"
	"testing"
)

func TestCatalogRequiresAndExposesThreeRealExecutors(t *testing.T) {
	t.Parallel()
	workspace, err := OpenWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = workspace.Close() })
	readExecutor, _ := NewReadExecutor(workspace)
	globExecutor, _ := NewGlobExecutor(workspace)
	grepExecutor, _ := NewGrepExecutor(workspace)
	catalog, err := NewCatalog(readExecutor, globExecutor, grepExecutor)
	if err != nil {
		t.Fatal(err)
	}
	text := string(catalog.CanonicalJSON())
	for _, present := range []string{"Read", "Glob", "Grep"} {
		if !strings.Contains(text, present) {
			t.Fatalf("catalog 缺少 %s", present)
		}
	}
	for _, absent := range []string{"Edit", "Write", "Bash", "MCP", "Skill", "Agent"} {
		if strings.Contains(text, absent) {
			t.Fatalf("catalog 暴露未实现能力 %s", absent)
		}
	}
	if _, err := NewCatalog(readExecutor, nil, grepExecutor); err == nil {
		t.Fatal("缺少 executor 的 catalog 未失败关闭")
	}
}
