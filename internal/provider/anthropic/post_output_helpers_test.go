package anthropic

import (
	"easycode/internal/tool"
	"testing"
)

func testModelOutputs(t *testing.T, results ...tool.InvocationResult) []tool.ModelOutput {
	t.Helper()
	out := make([]tool.ModelOutput, len(results))
	for i, result := range results {
		v, err := tool.NewModelOutput(result, nil)
		if err != nil {
			t.Fatal(err)
		}
		out[i] = v
	}
	return out
}

func testReadOutput(t *testing.T, call, path, text string) tool.ModelOutput {
	t.Helper()
	input, err := tool.NewReadInput(path, 1, 20)
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
	return testModelOutputs(t, result)[0]
}
func testRestoreFixtureOutputs(t *testing.T, prefix string, wire tool.ProviderWireKind) [][]tool.ModelOutput {
	t.Helper()
	id, _ := tool.ParseInvocationID("018f1d8a-7b5c-7def-8abc-456789abcdef")
	globInput, _ := tool.NewGlobInput("**/*.go", "", 10)
	globReady, _ := tool.NewGlobReadyCall(tool.ProviderCallID(prefix+"glob"), globInput)
	globInv, _ := tool.NewGlobInvocation(id, globReady)
	globMeta, _ := tool.NewGlobResultMetadata(nil, false, 0, 0, tool.SearchComplete, tool.SearchSkipCounts{})
	globPreview, _ := tool.NewModelPreview("frozen glob")
	glob, err := tool.NewGlobInvocationResult(globInv, tool.ResultSuccess, "ok", globPreview, globMeta)
	if err != nil {
		t.Fatal(err)
	}
	grepInput, _ := tool.NewGrepInput("TODO", "", "", tool.GrepOutputFilesWithMatches, false, 0, 0, 10)
	grepReady, _ := tool.NewGrepReadyCall(tool.ProviderCallID(prefix+"grep"), grepInput)
	grepInv, _ := tool.NewGrepInvocation(id, grepReady)
	grepMeta, _ := tool.NewGrepResultMetadata(tool.GrepOutputFilesWithMatches, nil, 0, false, 0, 0, 0, 0, tool.SearchComplete, tool.SearchSkipCounts{})
	grepPreview, _ := tool.NewModelPreview("frozen grep")
	grep, err := tool.NewGrepInvocationResult(grepInv, tool.ResultSuccess, "ok", grepPreview, grepMeta)
	if err != nil {
		t.Fatal(err)
	}
	replacement, _ := tool.NewPatchReplacement("old", "new", false)
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
	return [][]tool.ModelOutput{append(testModelOutputs(t, glob, grep), read), testModelOutputs(t, patch), testModelOutputs(t, write)}
}
