package openai

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

func TestReasoningRequestsSummaryAndWebsocketGolden(t *testing.T) {
	var actual bytes.Buffer
	for _, tc := range []struct {
		name    string
		effort  domain.OpenAIEffort
		summary domain.ReasoningSummary
		cap     uint64
	}{
		{"default", "", "", 0}, {"effort-only", "high", "", 0}, {"summary-only", "", "auto", 0},
		{"both", "high", "detailed", 32768}, {"none", "none", "concise", 8192}, {"cap-only", "", "", 1},
	} {
		settings, err := domain.NewOpenAIInference(tc.effort, tc.summary, tc.cap)
		if err != nil {
			t.Fatal(err)
		}
		instance, err := New(Config{Inference: settings, BaseURL: "https://example.invalid", Model: "gpt-test", APIKey: secret.New("fixture-key")})
		if err != nil {
			t.Fatal(err)
		}
		c := testConversation(t, instance)
		c.history.commit(testInputEntry(t, NewUserItem("hello")))
		sampling, _ := testSamplingInput(t, instance.config.Inference).WithToolCatalog(testOpenAIToolCatalog(t))
		ordinary, err := c.planSamplingRequest(c.history.snapshot(), sampling)
		if err != nil {
			t.Fatal(err)
		}
		compact, _ := provider.NewCompactionInput(sampling, "preserve test failures", 4096)
		summary, err := c.planCompactionRequest(compact)
		if err != nil {
			t.Fatal(err)
		}
		for _, plan := range []samplingRequestPlan{ordinary, summary} {
			body, err := plan.body()
			if err != nil {
				t.Fatal(err)
			}
			e, err := plan.evidence()
			if err != nil {
				t.Fatal(err)
			}
			receipt, err := c.describeRequest(body, e, plan.purpose)
			if err != nil {
				t.Fatal(err)
			}
			native, _ := codec.MarshalCanonical(plan.request.Input, provider.MaxNativeCommitBytes)
			if e.NativeFingerprint() != domain.ContentFingerprint(native.Bytes()) || receipt.NativeFingerprint != e.NativeFingerprint() || receipt.RequestFingerprint != domain.ContentFingerprint(body.Bytes()) {
				t.Fatal("receipt evidence mismatch")
			}
			var properties responsesRequest
			if codec.Unmarshal(e.Properties().Bytes(), &properties) != nil || properties.MaxOutputTokens != plan.request.MaxOutputTokens || properties.Input != nil {
				t.Fatal("incorrect properties")
			}
			if plan.purpose == domain.RequestSummary {
				if properties.ToolChoice != "none" || properties.MaxOutputTokens != 4096 || !bytes.Contains(native.Bytes(), []byte("preserve test failures")) {
					t.Fatal("ordinary evidence substituted for summary")
				}
				preview, err := c.PreflightCompaction(compact)
				if err != nil {
					t.Fatal(err)
				}
				if preview.Source().Digest() == e.NativeFingerprint() {
					t.Fatal("source digest confused with guided input")
				}
				preflight, _ := c.compileCompactionRequest(preview.Input())
				if !bytes.Equal(preflight.Bytes(), body.Bytes()) {
					t.Fatal("preflight differs from execution")
				}
			}
			for _, transport := range []string{"http", "ws-full", "ws-incremental"} {
				wire := body
				if transport != "http" {
					id, offset := "", 0
					if transport == "ws-incremental" {
						id, offset = "response-fixture", 1
					}
					wire, err = websocketBody(body, id, offset)
					if err != nil {
						t.Fatal(err)
					}
				}
				record, err := codec.MarshalStable(struct {
					Name    string          `json:"name"`
					Request json.RawMessage `json:"request"`
				}{fmt.Sprintf("%s/%s/%s", tc.name, plan.purpose, transport), wire.Bytes()})
				if err != nil {
					t.Fatal(err)
				}
				actual.Write(record)
				actual.WriteByte('\n')
			}
		}
		if instance.Capabilities().ReasoningSummary != (tc.summary != "") || instance.Capabilities().RawReasoning {
			t.Fatal("capability does not match enabled compiler")
		}
		if err := instance.Close(); err != nil {
			t.Fatal(err)
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
