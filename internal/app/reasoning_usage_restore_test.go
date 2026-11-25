package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"testing"

	"easycode/internal/fault"
	"easycode/internal/provider/anthropic"
	"easycode/internal/provider/openai"
	"easycode/internal/secret"
)

// corruptThinkingFact 只变更固定 bytes 的目标事实及 checksum，不调用被测 encoder。
func corruptThinkingFact(t *testing.T, data []byte, from, to string) []byte {
	t.Helper()
	lines := bytes.Split(bytes.TrimSuffix(data, []byte{'\n'}), []byte{'\n'})
	changed := false
	for i, line := range lines {
		if changed || !bytes.Contains(line, []byte(from)) {
			continue
		}
		line = bytes.Replace(line, []byte(from), []byte(to), 1)
		end := bytes.LastIndex(line, []byte(`,"checksum":`))
		if end < 0 {
			t.Fatal("fixed record missing checksum")
		}
		prefix := append([]byte(nil), line[:end]...)
		sum := sha256.Sum256(append(append([]byte(nil), prefix...), '}'))
		lines[i] = append(prefix, []byte(fmt.Sprintf(`,"checksum":"%x"}`, sum))...)
		changed = true
	}
	if !changed {
		t.Fatal("target fact not present in immutable fixture")
	}
	return append(bytes.Join(lines, []byte{'\n'}), '\n')
}

func TestThinkingUsageFixedCurrentRestoreAndRejectBeforeRepair(t *testing.T) {
	for _, name := range []string{"compaction_completed_anthropic", "reasoning_usage_positive_anthropic", "reasoning_usage_zero_anthropic"} {
		t.Run(name, func(t *testing.T) {
			fixture := readAppTestFile(t, "../session/testdata/current/"+name+".jsonl")
			root, path, thread := installPreviewFixture(t, fixture)
			view, cleanup, err := previewSession(context.Background(), root, thread, defaultPreviewDependencies())
			if err != nil || cleanup || len(view.Messages) == 0 {
				t.Fatal("fixed preview", err)
			}
			service, err := openSessionService(root)
			if err != nil {
				t.Fatal(err)
			}
			defer service.close()
			instance, err := anthropic.New(anthropic.Config{BaseURL: "https://example.invalid", Model: "claude-test", APIKey: secret.New("fixture-key")})
			if err != nil {
				t.Fatal(err)
			}
			defer instance.Close()
			assembled, err := service.resume(context.Background(), testSessionProvider(instance, "messages"), "claude-test", thread)
			if err != nil {
				t.Fatal(err)
			}
			if err := assembled.writer.Close(context.Background()); err != nil {
				t.Fatal(err)
			}
			after, _ := os.ReadFile(path)
			if !bytes.Equal(fixture, after) {
				t.Fatal("restore rewrote fixed current bytes")
			}
			if name != "reasoning_usage_positive_anthropic" {
				return
			}
			for _, tc := range []struct{ from, to string }{
				{`,"thinking_tokens":{"known":true,"value":30}`, ``},
				{`"reasoning_output":{"state":"known","value":30}`, `"reasoning_output":{"state":"known","value":0}`},
				{`"reasoning_output":{"state":"known","value":30}`, `"reasoning_output":{"state":"unknown"}`},
				{`"reasoning_output":{"state":"known","value":30}`, `"reasoning_output":{"state":"not_applicable"}`},
			} {
				invalid := append(corruptThinkingFact(t, fixture, tc.from, tc.to), []byte(`{"torn":`)...)
				writeAppTestFile(t, path, invalid, 0600)
				if _, err := service.resume(context.Background(), testSessionProvider(instance, "messages"), "claude-test", thread); err == nil {
					t.Fatal("inconsistent facts restored")
				}
				if _, _, err := previewSession(context.Background(), root, thread, defaultPreviewDependencies()); err == nil {
					t.Fatal("inconsistent facts previewed")
				}
				after, _ := os.ReadFile(path)
				if !bytes.Equal(invalid, after) {
					t.Fatal("invalid shape repaired or appended")
				}
			}
		})
	}
}

func TestOpenAISampleUsageRejectsKnownZeroForUnknownBeforeRepair(t *testing.T) {
	fixture := readAppTestFile(t, "../session/testdata/current/question_openai_complete.jsonl")
	invalid := append(corruptThinkingFact(t, fixture,
		`"reasoning_output":{"state":"unknown"}`, `"reasoning_output":{"state":"known","value":0}`), []byte(`{"torn":`)...)
	root, path, thread := installPreviewFixture(t, invalid)
	ctx := context.Background()
	service, err := openSessionService(root)
	if err != nil {
		t.Fatal(err)
	}
	defer service.close()
	instance, err := openai.New(openai.Config{BaseURL: "https://example.invalid", Model: "gpt-test", APIKey: secret.New("fixture-key")})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	assembled, err := service.resume(ctx, testSessionProvider(instance, "responses"), "gpt-test", thread)
	if err == nil {
		_ = assembled.writer.Close(ctx)
	}
	if summary := fault.Project(err); summary.Code != fault.CodeSessionCorruption || summary.Message != "sample usage is invalid" {
		t.Errorf("resume must reject the sample evidence: %v", err)
	}
	if _, _, err := previewSession(ctx, root, thread, defaultPreviewDependencies()); err == nil || err.Error() != "sample usage does not match native facts" {
		t.Errorf("preview must reject the sample evidence: %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(invalid, after) {
		t.Fatalf("rejected journal changed: %v", err)
	}
}
