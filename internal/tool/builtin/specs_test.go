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
	writePreparer, _ := NewWritePreparer(workspace)
	writeExecutor, _ := NewWriteExecutor(workspace)
	policy := tool.NewApprovalPolicy()
	execPreparer := NewExecPlanner()
	execExecutor, err := NewExecExecutor(workspace, OpenCommandEnvironment(), tool.RenderExecOutput)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := NewCatalog(readExecutor, globExecutor, grepExecutor, patchPreparer, patchExecutor, writePreparer, writeExecutor, execPreparer, execExecutor, tool.RenderExecOutput, policy)
	if err != nil {
		t.Fatal(err)
	}
	text := string(catalog.CanonicalJSON())
	for _, present := range []string{"Read", "Glob", "Grep", "Write", "Edit", "apply_patch", "custom_freeform", "Bash", "exec_command"} {
		if !strings.Contains(text, present) {
			t.Fatalf("catalog 缺少 %s", present)
		}
	}
	for _, absent := range []string{"write_stdin", "MCP", "Skill", "Agent"} {
		if strings.Contains(text, absent) {
			t.Fatalf("catalog 暴露未实现能力 %s", absent)
		}
	}
	if _, err := NewCatalog(readExecutor, nil, grepExecutor, patchPreparer, patchExecutor, writePreparer, writeExecutor, execPreparer, execExecutor, tool.RenderExecOutput, policy); err == nil {
		t.Fatal("缺少组件的 catalog 未失败关闭")
	}
	if _, err := NewCatalog(readExecutor, globExecutor, grepExecutor, nil, patchExecutor, writePreparer, writeExecutor, execPreparer, execExecutor, tool.RenderExecOutput, policy); err == nil {
		t.Fatal("缺少 Patch preparer 的 catalog 未失败关闭")
	}
	if _, err := NewCatalog(readExecutor, globExecutor, grepExecutor, patchPreparer, patchExecutor, writePreparer, writeExecutor, nil, execExecutor, tool.RenderExecOutput, policy); err == nil {
		t.Fatal("missing Exec planner accepted")
	}
	if _, err := NewCatalog(readExecutor, globExecutor, grepExecutor, patchPreparer, patchExecutor, writePreparer, writeExecutor, execPreparer, nil, tool.RenderExecOutput, policy); err == nil {
		t.Fatal("missing Exec executor accepted")
	}
	if _, err := NewCatalog(readExecutor, globExecutor, grepExecutor, patchPreparer, patchExecutor, writePreparer, writeExecutor, execPreparer, execExecutor, nil, policy); err == nil {
		t.Fatal("missing Exec renderer accepted")
	}
	if _, err := NewExecExecutor(workspace, OpenCommandEnvironment(), nil); err == nil {
		t.Fatal("executor without renderer accepted")
	}
}
