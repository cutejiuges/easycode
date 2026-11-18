//go:build darwin

package builtin

import "testing"

func sandboxTestPlan(t *testing.T, root string) SandboxPlan {
	t.Helper()
	candidate, err := NewExecPlanner().Prepare(processTestCandidate(t).Invocation())
	if err != nil {
		t.Fatal(err)
	}
	plan, err := NewSandboxPlanner().Plan(candidate, root)
	if err != nil {
		t.Fatal(err)
	}
	return plan
}
