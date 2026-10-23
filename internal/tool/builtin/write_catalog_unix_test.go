//go:build darwin || linux

package builtin

import (
	"bytes"
	"context"
	"easycode/internal/tool"
	"testing"
)

func TestCatalogWriteStateDoesNotChangeStableDefinition(t *testing.T) {
	root := t.TempDir()
	w, p, e, r := writeComponents(t, root)
	g, _ := NewGlobExecutor(w)
	s, _ := NewGrepExecutor(w)
	pp, _ := NewPatchPreparer(w)
	pe, _ := NewPatchExecutor(w)
	policy := tool.NewApprovalPolicy()
	execPreparer := NewExecPlanner()
	execExecutor, err := NewExecExecutor(w, OpenCommandEnvironment(), tool.RenderExecOutput)
	if err != nil {
		t.Fatal(err)
	}
	first, err := NewCatalog(r, g, s, pp, pe, p, e, execPreparer, execExecutor, tool.RenderExecOutput, policy)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewCatalog(r, g, s, pp, pe, nil, e, execPreparer, execExecutor, tool.RenderExecOutput, policy); err == nil {
		t.Fatal("nil Write preparer")
	}
	if _, err := NewCatalog(r, g, s, pp, pe, p, nil, execPreparer, execExecutor, tool.RenderExecOutput, policy); err == nil {
		t.Fatal("nil Write executor")
	}
	c, err := p.Prepare(context.Background(), writeCall(t, "new/a", "content"), tool.UnavailableReadObservation())
	if err != nil {
		t.Fatal(err)
	}
	if result := e.Execute(context.Background(), c); result.Status() != tool.ResultSuccess {
		t.Fatal(result.Code())
	}
	_ = readObservation(t, r, "new/a")
	second, err := NewCatalog(r, g, s, pp, pe, p, e, execPreparer, execExecutor, tool.RenderExecOutput, policy)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first.CanonicalJSON(), second.CanonicalJSON()) || first.Fingerprint() != second.Fingerprint() {
		t.Fatal("工作区状态污染 catalog")
	}
}
