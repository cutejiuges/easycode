package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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
		sampling, _ := provider.NewSamplingInput().WithToolCatalog(testOpenAIToolCatalog(t))
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
			if e.nativeFingerprint != domain.ContentFingerprint(native.Bytes()) || receipt.NativeFingerprint != e.nativeFingerprint || receipt.RequestFingerprint != domain.ContentFingerprint(body.Bytes()) {
				t.Fatal("receipt evidence mismatch")
			}
			var properties responsesRequest
			if codec.Unmarshal(e.properties.Bytes(), &properties) != nil || properties.MaxOutputTokens != plan.request.MaxOutputTokens || properties.Input != nil {
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
				if preview.Source().Digest() == e.nativeFingerprint {
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

func TestFrozenPlanAndChangedCompactionSourceRejectsBeforeNetwork(t *testing.T) {
	instance, err := New(Config{BaseURL: "https://example.invalid", Model: "model", APIKey: secret.New("fixture-key")})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	c := testConversation(t, instance)
	entry := testInputEntry(t, NewUserItem("hello"))
	c.history.commit(entry)
	sampling, _ := provider.NewSamplingInput().WithToolCatalog(testOpenAIToolCatalog(t))
	compact, _ := provider.NewCompactionInput(sampling, "preserve failures", 4096)
	preview, err := c.PreflightCompaction(compact)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := c.planCompactionRequest(compact)
	if err != nil {
		t.Fatal(err)
	}
	before, err := plan.body()
	if err != nil {
		t.Fatal(err)
	}
	generation := nativeHistoryEntry{Kind: nativeHistorySample, Outputs: []NativeItem{{Type: "message", Role: "assistant", Content: []ContentPart{{Type: "output_text", Text: "answer"}}}}}
	generation.TurnID = entry.TurnID
	c.history.commit(generation)
	after, err := plan.body()
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
