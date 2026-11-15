package openai

import (
	"easycode/internal/domain"
	"easycode/internal/tool"
	"testing"
)

func testDirectModelOutput(t *testing.T, invocation tool.Invocation, result tool.InvocationResult, admission tool.ExecutionAdmission) tool.ModelOutput {
	t.Helper()
	const id = "01900000-0000-7000-8000-000000000001"
	origin, err := tool.NewToolCallOrigin(domain.SessionID(id), domain.ThreadID(id), domain.TurnID(id), 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	selection, err := tool.NewUnhookedInputSelection(origin, invocation)
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := tool.NewInputResultEvidence(selection, admission, tool.InputResultExecution)
	if err != nil {
		t.Fatal(err)
	}
	output, err := tool.NewModelOutput(result, evidence)
	if err != nil {
		t.Fatal(err)
	}
	return output
}
func testNativeInvocation(t *testing.T, item NativeItem, id tool.InvocationID, callID tool.ProviderCallID) tool.Invocation {
	t.Helper()
	arguments := []byte(item.Arguments)
	if item.Type == "custom_tool_call" {
		arguments = []byte(item.Input)
	}
	ready, err := testOpenAIToolCatalog(t).DecodeReadyCall(domain.ProviderOpenAI, item.Name, callID, arguments)
	if err != nil {
		t.Fatal(err)
	}
	invocation, err := tool.NewInvocation(id, ready)
	if err != nil {
		t.Fatal(err)
	}
	return invocation
}
func testModelOutputs(t *testing.T, history []nativeHistoryEntry, results ...tool.InvocationResult) []tool.ModelOutput {
	t.Helper()
	calls, err := pendingFunctionCalls(history)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]tool.ModelOutput, len(results))
	for i, result := range results {
		if i >= len(calls) {
			t.Fatal("missing native fixture input")
		}
		invocation := testNativeInvocation(t, calls[i], result.InvocationID(), result.ProviderCallID())
		out[i] = testDirectModelOutput(t, invocation, result, tool.ExecutionAdmission{})
	}
	return out
}

func testReadOutput(t *testing.T, call, path, text string) tool.ModelOutput {
	t.Helper()
	input, err := tool.NewReadInput(path, 1, 2000)
	if err != nil {
		t.Fatal(err)
	}
	ready, err := tool.NewReadReadyCall(tool.ProviderCallID(call), input)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := tool.ParseInvocationID("018f1d8a-7b5c-7def-8abc-456789abcdef")
	inv, err := tool.NewReadInvocation(id, ready)
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := tool.NewReadResultMetadata(path, input, 1, 1, true, false, false)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := tool.NewModelPreview(text)
	if err != nil {
		t.Fatal(err)
	}
	result, err := tool.NewReadInvocationResult(inv, tool.ResultSuccess, "ok", preview, metadata)
	if err != nil {
		t.Fatal(err)
	}
	invocation, err := tool.NewInvocation(id, ready)
	if err != nil {
		t.Fatal(err)
	}
	return testDirectModelOutput(t, invocation, result, tool.ExecutionAdmission{})
}
func testRestoreFixtureOutputs(t *testing.T, prefix string, wire tool.ProviderWireKind) [][]tool.ModelOutput {
	t.Helper()
	id, _ := tool.ParseInvocationID("018f1d8a-7b5c-7def-8abc-456789abcdef")
	globInput, _ := tool.DecodeGlobInput([]byte(`{"pattern":"**/*.go"}`))
	globReady, _ := tool.NewGlobReadyCall(tool.ProviderCallID(prefix+"glob"), globInput)
	globInv, _ := tool.NewGlobInvocation(id, globReady)
	globMeta, _ := tool.NewGlobResultMetadata(nil, false, 0, 0, tool.SearchComplete, tool.SearchSkipCounts{})
	globPreview, _ := tool.NewModelPreview("frozen glob")
	glob, err := tool.NewGlobInvocationResult(globInv, tool.ResultSuccess, "ok", globPreview, globMeta)
	if err != nil {
		t.Fatal(err)
	}
	grepInput, _ := tool.DecodeGrepInput([]byte(`{"pattern":"TODO"}`))
	grepReady, _ := tool.NewGrepReadyCall(tool.ProviderCallID(prefix+"grep"), grepInput)
	grepInv, _ := tool.NewGrepInvocation(id, grepReady)
	grepMeta, _ := tool.NewGrepResultMetadata(tool.GrepOutputFilesWithMatches, nil, 0, false, 0, 0, 0, 0, tool.SearchComplete, tool.SearchSkipCounts{})
	grepPreview, _ := tool.NewModelPreview("frozen grep")
	grep, err := tool.NewGrepInvocationResult(grepInv, tool.ResultSuccess, "ok", grepPreview, grepMeta)
	if err != nil {
		t.Fatal(err)
	}
	replacement, _ := tool.NewPatchReplacement("old", "new", false)
	if wire == tool.WireCustomFreeform {
		replacement, _ = tool.NewPatchLineReplacement([]string{"old"}, []string{"new"})
	}
	patchInput, _ := tool.NewPatchInput("main.go", []tool.PatchReplacement{replacement})
	patchReady, _ := tool.NewPatchReadyCall(tool.ProviderCallID(prefix+"edit"), wire, patchInput)
	patchInv, _ := tool.NewPatchInvocation(id, patchReady)
	patchMeta, _ := tool.NewPatchResultMetadata("main.go", 1, 1, 1)
	patchPreview, _ := tool.NewModelPreview("frozen patch")
	patch, err := tool.NewPatchInvocationResult(patchInv, tool.ResultSuccess, "ok", patchPreview, patchMeta)
	if err != nil {
		t.Fatal(err)
	}
	writeInput, _ := tool.NewWriteInput("new/a", "中文\r\n")
	writeReady, _ := tool.NewWriteReadyCall(tool.ProviderCallID(prefix+"write"), writeInput)
	writeInv, _ := tool.NewWriteInvocation(id, writeReady)
	writeMeta, _ := tool.NewWriteResultMetadata("new/a", tool.WriteCreate, 8, 1, 1)
	writePreview, _ := tool.NewModelPreview("Created new/a (8 bytes, 1 lines, 1 directories created)")
	write, err := tool.NewWriteInvocationResult(writeInv, tool.ResultSuccess, "ok", writePreview, writeMeta)
	if err != nil {
		t.Fatal(err)
	}
	read := testReadOutput(t, prefix+"read", "README.md", "frozen read")
	globInvocation, _ := tool.NewInvocation(id, globReady)
	grepInvocation, _ := tool.NewInvocation(id, grepReady)
	patchInvocation, _ := tool.NewInvocation(id, patchReady)
	writeInvocation, _ := tool.NewInvocation(id, writeReady)
	return [][]tool.ModelOutput{{testDirectModelOutput(t, globInvocation, glob, tool.ExecutionAdmission{}), testDirectModelOutput(t, grepInvocation, grep, tool.ExecutionAdmission{}), read}, {testDirectModelOutput(t, patchInvocation, patch, tool.ExecutionAdmission{})}, {testDirectModelOutput(t, writeInvocation, write, tool.ExecutionAdmission{})}}
}
