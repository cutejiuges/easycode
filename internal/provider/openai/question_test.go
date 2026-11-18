package openai

import (
	"easycode/internal/codec"
	"easycode/internal/provider"
	"easycode/internal/tool"
	"testing"
)

// testQuestionRestoreResults 将测试原生 fixture 的成功答案绑定为独立共享事实。
func testQuestionRestoreResults(t *testing.T, following []provider.NativeCommitEnvelope, questions []tool.QuestionInvocation) []tool.InvocationResult {
	t.Helper()
	if len(questions) == 0 {
		return nil
	}
	for _, envelope := range following {
		entry, err := decodeNativeCommit(envelope)
		if err != nil {
			t.Fatal(err)
		}
		if entry.Kind != nativeHistoryToolOutputs {
			break
		}
		var results []tool.InvocationResult
		for _, q := range questions {
			for _, item := range entry.ToolOutputs {
				if !(item.CallID == string(q.ProviderCallID())) {
					continue
				}
				var wire functionOutputPayload
				if err := codec.UnmarshalRecord([]byte(item.Output), &wire); err != nil {
					t.Fatal(err)
				}
				if wire.Status != tool.ResultSuccess {
					t.Fatal("question fixture requires explicit failure facts")
				}
				preview := wire.Content
				a, err := tool.DecodeQuestionPreview(q.Input(), []byte(preview))
				if err != nil {
					t.Fatal(err)
				}
				r, err := tool.NewQuestionInvocationResult(q, a)
				if err != nil {
					t.Fatal(err)
				}
				results = append(results, r)
			}
		}
		return results
	}
	return nil
}
