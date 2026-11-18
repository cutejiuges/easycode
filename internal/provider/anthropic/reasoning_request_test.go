package anthropic

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
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
			sampling, _ := testSamplingInput(t, instance.config.Inference).WithToolCatalog(testAnthropicToolCatalog(t))
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
