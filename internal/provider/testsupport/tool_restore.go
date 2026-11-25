package testsupport

import (
	"strings"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/tool"
)

// NamedResult 保留结果语义矩阵，原生 wire 断言留在各 Provider。
type NamedResult struct {
	Name   string
	Result tool.InvocationResult
}

func ExecRestoreResults(t *testing.T) []NamedResult {
	t.Helper()
	var results []NamedResult
	input, _ := tool.NewExecInput("printf '你好'", "src", 1000)
	call, _ := tool.NewExecReadyCall("call-exec", input)
	id, _ := tool.ParseInvocationID("018f1d8a-7b5c-7def-8abc-456789abcdef")
	invocation, _ := tool.NewExecInvocation(id, call)
	for _, test := range []struct {
		name                      string
		status                    tool.ResultStatus
		code                      string
		exit                      int
		known, timeout, cancelled bool
	}{
		{"zero", tool.ResultSuccess, "ok", 0, true, false, false},
		{"nonzero", tool.ResultSuccess, "ok", 2, true, false, false},
		{"denied", tool.ResultDenied, "approval_required", 0, false, false, false},
		{"hook-denied", tool.ResultDenied, "hook_denied", 0, false, false, false},
		{"hook-uncertain", tool.ResultCancelled, "hook_outcome_uncertain", 0, false, false, true},
		{"cancelled", tool.ResultCancelled, "cancelled", 143, true, false, true},
		{"timeout", tool.ResultError, "timed_out", 137, true, true, false},
		{"uncertain", tool.ResultOutcomeUncertain, "process_cleanup_uncertain", 0, false, false, false},
	} {

		text := "frozen " + test.name + "\n你好\n"
		if strings.HasPrefix(test.name, "hook-") {
			d, err := domain.NewHookDeclaration(domain.HookSourceProject, domain.HookPreToolUse, domain.HookFeedbackNone, strings.Repeat("a", 64), "check", 7, "true", 5)
			if err != nil {
				t.Fatal(err)
			}
			text = domain.HookSummary(d.Reference, domain.HookExec, domain.HookCode(test.code))
		}
		preview, _ := tool.NewModelPreview(text)
		metadata, err := tool.NewExecResultMetadata("src", test.exit, test.known, 0, tool.ExecStreamStats{}, tool.ExecStreamStats{}, test.timeout, test.cancelled, false)
		if err != nil {
			t.Fatal(err)
		}
		result, err := tool.NewExecInvocationResult(invocation, test.status, test.code, preview, metadata)
		if err != nil {
			t.Fatal(err)
		}
		results = append(results, NamedResult{Name: test.name, Result: result})
	}
	return results
}
