package builtin

import (
	"path/filepath"
	"reflect"
	"testing"

	"easycode/internal/tool"
)

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

func TestSandboxPlanFreezesCapabilitiesWithoutExternalResources(t *testing.T) {
	// 路径不必存在，证明plan不打开目录；真实启动只传入已打开描述符解析的路径。
	root := filepath.Join(t.TempDir(), "absent")
	plan := sandboxTestPlan(t, root)
	if plan.Workspace() != root || plan.Workdir() != "" || plan.NetworkAllowed() || plan.Revision() != "darwin-workspace-offline" || plan.CandidateFingerprint() == "" {
		t.Fatal("sandbox plan lost candidate constraints")
	}
	if plan.TemporaryDirectory() != root || plan.CacheDirectory() != filepath.Join(root, ".cache", "go-build") || !reflect.DeepEqual(plan.WriteRoots(), []string{root}) {
		t.Fatal("sandbox plan widened mutable paths")
	}
	reads, writes := plan.ReadRoots(), plan.WriteRoots()
	reads[0], writes[0] = "/private-secret", "/"
	if plan.ReadRoots()[0] != "/System" || plan.WriteRoots()[0] != root {
		t.Fatal("sandbox plan aliases caller slices")
	}
	candidate, _ := NewExecPlanner().Prepare(processTestCandidate(t).Invocation())
	for _, profile := range []struct {
		revision        string
		network, writes bool
	}{{"darwin-workspace-offline", true, true}, {"darwin-workspace-offline", false, false}, {"unreviewed-policy", false, true}} {
		sandbox, _ := tool.NewSandboxProfile(profile.revision, profile.network, profile.writes)
		changed, err := tool.NewExecCandidate(candidate.Invocation(), candidate.Shell(), candidate.Environment(), sandbox)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := NewSandboxPlanner().Plan(changed, root); err == nil {
			t.Fatal("unsupported candidate widened sandbox")
		}
	}
	for _, invalid := range []string{"", "relative", root + "/../other", root + "\x00secret"} {
		if _, err := NewSandboxPlanner().Plan(candidate, invalid); err == nil {
			t.Fatal("unbound workspace path accepted")
		}
	}
	if _, err := NewSandboxPlanner().Plan(tool.ExecCandidate{}, root); err == nil {
		t.Fatal("invalid candidate accepted")
	}
	input, _ := tool.NewExecInput("true", "src", 1000)
	call, _ := tool.NewExecReadyCall("call-plan", input)
	invocation, _ := tool.NewExecInvocation(candidate.Invocation().InvocationID(), call)
	changed, _ := NewExecPlanner().Prepare(invocation)
	other, err := NewSandboxPlanner().Plan(changed, root)
	if err != nil || other.Workdir() != "src" || other.CandidateFingerprint() == plan.CandidateFingerprint() {
		t.Fatal("workdir not candidate-bound")
	}
}
