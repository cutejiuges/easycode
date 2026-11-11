package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"easycode/internal/codec"
	"easycode/internal/domain"
	"easycode/internal/provider"
	"easycode/internal/secret"
)

func TestReasoningRequestsAndSummaryEvidenceGolden(t *testing.T) {
	var actual bytes.Buffer
	for _, mode := range []domain.AnthropicCacheMode{domain.AnthropicCacheDisabled, domain.AnthropicCache5m, domain.AnthropicCache1h} {
		for _, tc := range []struct {
			name    string
			mode    domain.ThinkingMode
			budget  uint64
			display domain.ThinkingDisplay
			effort  domain.AnthropicEffort
			output  uint64
		}{
			{"default", domain.ThinkingOmit, 0, "", "", 0},
			{"disabled", domain.ThinkingDisabled, 0, "", "low", 0},
			{"adaptive", domain.ThinkingAdaptive, 0, "", "high", 16384},
			{"adaptive-summarized", domain.ThinkingAdaptive, 0, domain.DisplaySummarized, "high", 16384},
			{"adaptive-omitted", domain.ThinkingAdaptive, 0, domain.DisplayOmitted, "max", 16384},
			{"manual", domain.ThinkingEnabled, 2048, "", "", 8192},
			{"manual-summarized", domain.ThinkingEnabled, 2048, domain.DisplaySummarized, "high", 8192},
			{"manual-omitted", domain.ThinkingEnabled, 2048, domain.DisplayOmitted, "high", 8192},
			{"effort-only", domain.ThinkingOmit, 0, "", "xhigh", 0},
		} {
			thinking, err := domain.NewThinkingSettings(tc.mode, tc.budget, tc.display)
			if err != nil {
				t.Fatal(err)
			}
			settings, err := domain.NewAnthropicInference(thinking, tc.effort, tc.output)
			if err != nil {
				t.Fatal(err)
			}
			policy, _ := domain.NewCachePolicy(mode, domain.OpenAICacheOmit, domain.OpenAIHTTPSSE)
			instance, err := New(Config{Inference: settings, CachePolicy: policy, BaseURL: "https://example.invalid", Model: "claude-test", APIKey: secret.New("fixture-key")})
			if err != nil {
				t.Fatal(err)
			}
			conversation := testConversation(t, instance)
			conversation.history.commit(testInputEntry(t, newUserMessage("hello")))
			sampling, _ := provider.NewSamplingInput().WithToolCatalog(testAnthropicToolCatalog(t))
			ordinary, err := conversation.planSamplingRequest(conversation.history.snapshot(), sampling)
			if err != nil {
				t.Fatal(err)
			}
			compact, _ := provider.NewCompactionInput(sampling, "preserve test failures", 4096)
			summary, err := conversation.planCompactionRequest(compact)
			if err != nil {
				t.Fatal(err)
			}
			before, _ := codec.MarshalStable(conversation.history.snapshot())
			for _, plan := range []samplingRequestPlan{ordinary, summary} {
				body, err := plan.body(mode)
				if err != nil {
					t.Fatal(err)
				}
				evidence, err := plan.evidence()
				if err != nil {
					t.Fatal(err)
				}
				receipt, err := conversation.describeRequest(body, evidence, plan.purpose)
				if err != nil {
					t.Fatal(err)
				}
				logical, _ := codec.MarshalCanonical(plan.request.Messages, provider.MaxNativeCommitBytes)
				if evidence.nativeFingerprint != domain.ContentFingerprint(logical.Bytes()) || receipt.NativeFingerprint != evidence.nativeFingerprint || receipt.RequestFingerprint != domain.ContentFingerprint(body.Bytes()) {
					t.Fatal("request and evidence disagree")
				}
				var wire messagesRequest
				if codec.Unmarshal(body.Bytes(), &wire) != nil || wire.MaxTokens != plan.request.MaxTokens {
					t.Fatal("request fields lost")
				}
				var props messagesRequest
				if codec.Unmarshal(evidence.properties.Bytes(), &props) != nil || props.MaxTokens != wire.MaxTokens || props.Messages != nil {
					t.Fatal("properties describe a different request")
				}
				if plan.purpose == domain.RequestSummary {
					if props.ToolChoice == nil || props.ToolChoice.Type != "none" || props.MaxTokens != 4096 || !bytes.Contains(logical.Bytes(), []byte("preserve test failures")) {
						t.Fatal("ordinary evidence substituted for summary")
					}
					source, err := conversation.compactionSource()
					if err != nil || source.Digest() == evidence.nativeFingerprint {
						t.Fatal("source digest confused with guided input", err)
					}
					preview, err := conversation.PreflightCompaction(compact)
					if err != nil || preview.Source() != source {
						t.Fatal("preflight source", err)
					}
					second, _ := conversation.compileCompactionRequest(preview.Input())
					if !bytes.Equal(second.Bytes(), body.Bytes()) {
						t.Fatal("preflight differs from execution plan")
					}
				}
				record, err := codec.MarshalStable(struct {
					Name    string          `json:"name"`
					Request json.RawMessage `json:"request"`
				}{fmt.Sprintf("%s/%s/%s", mode, tc.name, plan.purpose), body.Bytes()})
				if err != nil {
					t.Fatal(err)
				}
				actual.Write(record)
				actual.WriteByte('\n')
			}
			after, _ := codec.MarshalStable(conversation.history.snapshot())
			if !bytes.Equal(before, after) {
				t.Fatal("cache planning mutated native history")
			}
			if instance.Capabilities().ReasoningSummary || instance.Capabilities().RawReasoning {
				t.Fatal("unsupported capability")
			}
			if err := instance.Close(); err != nil {
				t.Fatal(err)
			}
		}
	}
	want, err := os.ReadFile("testdata/reasoning_requests.golden.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(actual.Bytes(), want) {
		t.Fatal("reasoning request golden mismatch")
	}
}

func TestManualSummaryBudgetRejectedBeforeNetwork(t *testing.T) {
	thinking, _ := domain.NewThinkingSettings(domain.ThinkingEnabled, 8192, domain.DisplayOmitted)
	settings, err := domain.NewAnthropicInference(thinking, "high", 16384)
	if err != nil {
		t.Fatal(err)
	}
	instance, err := New(Config{Inference: settings, BaseURL: "https://example.invalid", Model: "claude-test", APIKey: secret.New("fixture-key")})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	c := testConversation(t, instance)
	c.history.commit(testInputEntry(t, newUserMessage(strings.Repeat("history ", 50))))
	sampling, _ := provider.NewSamplingInput().WithToolCatalog(testAnthropicToolCatalog(t))
	before, _ := codec.MarshalStable(c.history.snapshot())
	for _, output := range []uint64{4096, 8192, 8193} {
		compact, _ := provider.NewCompactionInput(sampling, "", output)
		_, err := c.PreflightCompaction(compact)
		if output <= 8192 {
			var failure *provider.CompactionError
			if !errors.As(err, &failure) || failure.Code() != provider.CompactionBudgetExceeded {
				t.Fatal(output, err)
			}
		} else if err != nil {
			t.Fatal(output, err)
		}
	}
	after, _ := codec.MarshalStable(c.history.snapshot())
	if !bytes.Equal(before, after) || c.pending.Load() || c.active.Load() {
		t.Fatal("preflight changed history or ownership")
	}
}

func TestFrozenPlanAndChangedCompactionSourceRejectsBeforeNetwork(t *testing.T) {
	instance, err := New(Config{BaseURL: "https://example.invalid", Model: "model", APIKey: secret.New("fixture-key")})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	c := testConversation(t, instance)
	entry := testInputEntry(t, newUserMessage("hello"))
	c.history.commit(entry)
	sampling, _ := provider.NewSamplingInput().WithToolCatalog(testAnthropicToolCatalog(t))
	compact, _ := provider.NewCompactionInput(sampling, "preserve failures", 4096)
	preview, err := c.PreflightCompaction(compact)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := c.planCompactionRequest(compact)
	if err != nil {
		t.Fatal(err)
	}
	before, err := plan.body(domain.AnthropicCacheDisabled)
	if err != nil {
		t.Fatal(err)
	}
	generation := compactionTestGeneration("answer", "opaque")
	generation.TurnID = entry.TurnID
	c.history.commit(generation)
	after, err := plan.body(domain.AnthropicCacheDisabled)
	if err != nil || !bytes.Equal(before.Bytes(), after.Bytes()) {
		t.Fatal("native mutation changed frozen plan", err)
	}
	prepared, err := c.RunCompaction(context.Background(), "01900000-0000-7000-8000-000000000009", preview)
	var failure *provider.CompactionError
	if prepared != nil || !errors.As(err, &failure) || failure.Code() != provider.CompactionSourceChanged {
		t.Fatal("stale preflight reached network", err)
	}
	if c.active.Load() || c.pending.Load() {
		t.Fatal("stale preflight retained owner")
	}
}
