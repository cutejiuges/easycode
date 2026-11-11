package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/provider/anthropic"
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
			fixture, err := os.ReadFile("../session/testdata/current/" + name + ".jsonl")
			if err != nil {
				t.Fatal(err)
			}
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
			assembled, err := service.resume(context.Background(), instance, "messages", "claude-test", thread)
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
				if err := os.WriteFile(path, invalid, 0600); err != nil {
					t.Fatal(err)
				}
				if _, err := service.resume(context.Background(), instance, "messages", "claude-test", thread); err == nil {
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

func TestThinkingUsageAggregatesWithoutAddingToOutput(t *testing.T) {
	known := domain.KnownUsageMetric
	unknown := domain.UnknownUsageMetric()
	one, _ := domain.NewSampleUsage(unknown, unknown, unknown, known(40), known(30))
	two, _ := domain.NewSampleUsage(unknown, unknown, unknown, known(20), known(10))
	missing, _ := domain.NewSampleUsage(unknown, unknown, unknown, known(10), unknown)
	aggregate, err := domain.AggregateSampleUsage([]domain.SampleUsage{one, two})
	if err != nil {
		t.Fatal(err)
	}
	output, _ := aggregate.Output().Value()
	thinking, _ := aggregate.ReasoningOutput().Value()
	if output != 60 || thinking != 40 {
		t.Fatal("thinking double counted or lost")
	}
	aggregate, err = domain.AggregateSampleUsage([]domain.SampleUsage{one, missing})
	if err != nil || aggregate.ReasoningOutput().State() != domain.UsageMetricUnknown {
		t.Fatal("missing observation did not propagate", err)
	}
}
