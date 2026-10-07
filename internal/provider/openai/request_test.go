package openai

import (
	"os"
	"strings"
	"testing"

	contextplan "easycode/internal/context"
	"easycode/internal/domain"
)

func TestCompileResponsesRequestMatchesGolden(t *testing.T) {
	request, err := compileResponsesRequest("gpt-test", nil, nil, nativeItemPointer(NewUserItem("hello")), testOpenAIToolView(t))
	if err != nil {
		t.Fatalf("compile request: %v", err)
	}
	encoded := request.Bytes()
	want, err := os.ReadFile("testdata/responses_request.golden.json")
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	if string(encoded) != strings.TrimSpace(string(want)) {
		t.Fatalf("request golden mismatch:\n got: %s\nwant: %s", encoded, want)
	}
	if strings.Contains(string(encoded), "previous_response_id") || !strings.Contains(string(encoded), `"tools"`) || strings.Contains(string(encoded), "prompt_cache_key") {
		t.Fatalf("request contains deferred fields: %s", encoded)
	}
}

func TestCompileResponsesRequestWithProjectInstructionsMatchesGolden(t *testing.T) {
	snapshot := testOpenAIProjectInstructions(t, "AGENTS.md", "Use make verify.")
	request, err := compileResponsesRequest("gpt-test", nil, &snapshot, nativeItemPointer(NewUserItem("hello")), testOpenAIToolView(t))
	if err != nil {
		t.Fatalf("compile request: %v", err)
	}
	encoded := request.Bytes()
	want, err := os.ReadFile("testdata/responses_request_with_project_instructions.golden.json")
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	if string(encoded) != strings.TrimSpace(string(want)) {
		t.Fatalf("request golden mismatch:\n got: %s\nwant: %s", encoded, want)
	}
	if strings.Contains(string(encoded), "/Users/") || strings.Contains(string(encoded), "previous_response_id") {
		t.Fatalf("request contains an absolute path or deferred field: %s", encoded)
	}
}

func TestCompileResponsesRequestIncludesNativeHistoryAndStableFingerprint(t *testing.T) {
	assistant := NativeItem{
		Type: "message",
		ID:   "msg-1",
		Role: "assistant",
		Content: []ContentPart{{
			Type: "output_text",
			Text: "first answer",
		}},
	}
	history := []nativeHistoryEntry{testSampleEntry(NewUserItem("first"), []NativeItem{assistant})}
	request := buildResponsesRequest("gpt-test", history, nil, nativeItemPointer(NewUserItem("second")), testOpenAIToolView(t))
	if len(request.Input) != 3 || request.Input[1].ID != "msg-1" || request.Input[2].Content[0].Text != "second" {
		t.Fatalf("unexpected request input: %#v", request.Input)
	}
	compiled, err := compileResponsesRequest("gpt-test", history, nil, nativeItemPointer(NewUserItem("second")), testOpenAIToolView(t))
	if err != nil {
		t.Fatalf("compile request: %v", err)
	}

	first, err := contextplan.NewSegment("openai-responses-request", contextplan.StabilityTurnStable, "v1", compiled)
	if err != nil {
		t.Fatalf("create first fingerprint: %v", err)
	}
	second, err := contextplan.NewSegment("openai-responses-request", contextplan.StabilityTurnStable, "v1", compiled)
	if err != nil {
		t.Fatalf("create second fingerprint: %v", err)
	}
	if first.Fingerprint() != second.Fingerprint() || string(first.CanonicalJSON()) != string(second.CanonicalJSON()) {
		t.Fatalf("request fingerprint is unstable: %#v %#v", first, second)
	}
}

func testOpenAIProjectInstructions(t *testing.T, source string, content string) domain.ProjectInstructionsSnapshot {
	t.Helper()
	document, err := domain.NewProjectInstructionDocument(source, content)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := domain.NewProjectInstructionsSnapshot([]domain.ProjectInstructionDocument{document}, 32<<10)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}
