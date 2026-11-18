package openai

import (
	"os"
	"strings"
	"testing"

	"easycode/internal/domain"
)

func TestCompileResponsesRequestMatchesGolden(t *testing.T) {
	request, err := compileResponsesRequest("gpt-test", append([]nativeHistoryEntry(nil), testInputEntry(t, NewUserItem("hello"))), nil, testOpenAIToolView(t))
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
	if !strings.Contains(string(encoded), `"type":"custom","name":"apply_patch"`) || strings.Contains(string(encoded), `"type":"function","name":"apply_patch"`) {
		t.Fatalf("apply_patch wire kind错误: %s", encoded)
	}
}

func TestCompileResponsesRequestWithProjectInstructionsMatchesGolden(t *testing.T) {
	snapshot := testOpenAIProjectInstructions(t, "AGENTS.md", "Use make verify.")
	request, err := compileResponsesRequest("gpt-test", append([]nativeHistoryEntry(nil), testInputEntry(t, NewUserItem("hello"))), &snapshot, testOpenAIToolView(t))
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
