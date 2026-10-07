package anthropic

import (
	"os"
	"strings"
	"testing"

	contextplan "easycode/internal/context"
	"easycode/internal/domain"
)

func TestCompileMessagesRequestMatchesGolden(t *testing.T) {
	request, err := compileMessagesRequest("claude-test", DefaultMaxOutputTokens, nil, nil, newUserMessage("hello"))
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
	for _, deferred := range []string{`"tools"`, `"system"`, `"thinking"`, `"temperature"`, `"beta"`, `"cache_control"`} {
		if strings.Contains(string(encoded), deferred) {
			t.Fatalf("request contains deferred field %s: %s", deferred, encoded)
		}
	}
}

func TestCompileMessagesRequestWithProjectInstructionsMatchesGolden(t *testing.T) {
	snapshot := testAnthropicProjectInstructions(t, "AGENTS.md", "Use make verify.")
	request, err := compileMessagesRequest(
		"claude-test", DefaultMaxOutputTokens, nil, &snapshot, newUserMessage("hello"),
	)
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
	for _, forbidden := range []string{`"system"`, `"tools"`, `"thinking"`, `"cache_control"`, "/Users/"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("request contains forbidden field %s: %s", forbidden, encoded)
		}
	}
}

func TestCompileMessagesRequestKeepsNativeMessageOrder(t *testing.T) {
	history := []nativeTurn{{
		User: newUserMessage("first"),
		Assistant: nativeMessage{Role: roleAssistant, Content: []NativeItem{
			{Type: blockTypeThinking, Thinking: "reason", Signature: "opaque-signature"},
			{Type: blockTypeRedactedThinking, RedactedData: "opaque-redacted"},
			{Type: blockTypeText, Text: "answer"},
		}},
	}}
	request := buildMessagesRequest("claude-test", 8192, history, nil, newUserMessage("second"))
	if request.MaxTokens != 8192 || len(request.Messages) != 3 {
		t.Fatalf("request shape: %#v", request)
	}
	if request.Messages[0].Role != roleUser || request.Messages[1].Role != roleAssistant || request.Messages[2].Role != roleUser {
		t.Fatalf("message roles: %#v", request.Messages)
	}
	blocks := request.Messages[1].Content
	if len(blocks) != 3 || blocks[0].Signature != "opaque-signature" || blocks[1].RedactedData != "opaque-redacted" || blocks[2].Text != "answer" {
		t.Fatalf("assistant blocks: %#v", blocks)
	}
}

func TestMessagesRequestFingerprintIsStableAndSecretFree(t *testing.T) {
	request, err := compileMessagesRequest("claude-test", DefaultMaxOutputTokens, nil, nil, newUserMessage("hello"))
	if err != nil {
		t.Fatalf("compile request: %v", err)
	}
	first, err := contextplan.NewSegment("anthropic-messages-request", contextplan.StabilityTurnStable, "v1", request)
	if err != nil {
		t.Fatalf("create first fingerprint: %v", err)
	}
	second, err := contextplan.NewSegment("anthropic-messages-request", contextplan.StabilityTurnStable, "v1", request)
	if err != nil {
		t.Fatalf("create second fingerprint: %v", err)
	}
	if first.Fingerprint() != second.Fingerprint() || !stringSlicesEqual(first.CanonicalJSON(), second.CanonicalJSON()) {
		t.Fatalf("request fingerprint is unstable: %#v %#v", first, second)
	}
	canonical := string(first.CanonicalJSON())
	for _, dynamic := range []string{"top-secret", "x-api-key", "timestamp", "random_id", "working_directory"} {
		if strings.Contains(canonical, dynamic) {
			t.Fatalf("dynamic or secret input entered fingerprint: %s", canonical)
		}
	}
}

func stringSlicesEqual(first []byte, second []byte) bool {
	return string(first) == string(second)
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
