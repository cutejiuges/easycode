package anthropic

import (
	"os"
	"strings"
	"testing"

	"easycode/internal/domain"
)

func TestCompileMessagesRequestMatchesGolden(t *testing.T) {
	request, err := compileMessagesRequest("claude-test", DefaultMaxOutputTokens, append([]nativeHistoryEntry(nil), testInputEntry(t, newUserMessage("hello"))), nil, testAnthropicToolView(t))
	if err != nil {
		t.Fatalf("compile request: %v", err)
	}
	encoded := request.Bytes()
	want, err := os.ReadFile("testdata/messages_request.golden.json")
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	if string(encoded) != strings.TrimSpace(string(want)) {
		t.Fatalf("request golden mismatch:\n got: %s\nwant: %s", encoded, want)
	}
	for _, deferred := range []string{`"system"`, `"thinking"`, `"temperature"`, `"beta"`, `"cache_control"`} {
		if strings.Contains(string(encoded), deferred) {
			t.Fatalf("request contains deferred field %s: %s", deferred, encoded)
		}
	}
}

func TestCompileMessagesRequestWithProjectInstructionsMatchesGolden(t *testing.T) {
	snapshot := testAnthropicProjectInstructions(t, "AGENTS.md", "Use make verify.")
	request, err := compileMessagesRequest("claude-test", DefaultMaxOutputTokens, append([]nativeHistoryEntry(nil), testInputEntry(t, newUserMessage("hello"))), &snapshot, testAnthropicToolView(t))
	if err != nil {
		t.Fatalf("compile request: %v", err)
	}
	encoded := request.Bytes()
	want, err := os.ReadFile("testdata/messages_request_with_project_instructions.golden.json")
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	if string(encoded) != strings.TrimSpace(string(want)) {
		t.Fatalf("request golden mismatch:\n got: %s\nwant: %s", encoded, want)
	}
	for _, forbidden := range []string{`"system"`, `"thinking"`, `"cache_control"`, "/Users/"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("request contains forbidden field %s: %s", forbidden, encoded)
		}
	}
}

func testAnthropicProjectInstructions(t *testing.T, source string, content string) domain.ProjectInstructionsSnapshot {
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
