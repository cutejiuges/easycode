package builtin

import (
	"strings"
	"testing"

	"easycode/internal/tool"
)

func TestCatalogRequiresAndExposesAllRealComponents(t *testing.T) {
	t.Parallel()
	workspace, err := OpenWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = workspace.Close() })
	readExecutor, _ := NewReadExecutor(workspace)
	globExecutor, _ := NewGlobExecutor(workspace)
	grepExecutor, _ := NewGrepExecutor(workspace)
	patchPreparer, _ := NewPatchPreparer(workspace)
	patchExecutor, _ := NewPatchExecutor(workspace)
	policy := tool.NewApprovalPolicy()
	catalog, err := NewCatalog(readExecutor, globExecutor, grepExecutor, patchPreparer, patchExecutor, policy)
	if err != nil {
		t.Fatal(err)
	}
	text := string(catalog.CanonicalJSON())
	for _, present := range []string{"Read", "Glob", "Grep", "Edit", "apply_patch", "custom_freeform"} {
		if !strings.Contains(text, present) {
			t.Fatalf("catalog 缺少 %s", present)
		}
	}
	for _, absent := range []string{"Write", "Bash", "MCP", "Skill", "Agent"} {
		if strings.Contains(text, absent) {
			t.Fatalf("catalog 暴露未实现能力 %s", absent)
		}
	}
	if _, err := NewCatalog(readExecutor, nil, grepExecutor, patchPreparer, patchExecutor, policy); err == nil {
		t.Fatal("缺少组件的 catalog 未失败关闭")
	}
	if _, err := NewCatalog(readExecutor, globExecutor, grepExecutor, nil, patchExecutor, policy); err == nil {
		t.Fatal("缺少 Patch preparer 的 catalog 未失败关闭")
	}
}
