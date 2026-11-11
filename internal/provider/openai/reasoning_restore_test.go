package openai

import (
	"bytes"
	"os"
	"testing"

	"easycode/internal/codec"
	contextplan "easycode/internal/context"
	"easycode/internal/domain"
	"easycode/internal/provider"
	"easycode/internal/secret"
)

func TestReasoningFixedNativeRestoreAndCacheBytesIgnoreReading(t *testing.T) {
	fixture, err := os.ReadFile("testdata/reasoning_restore.current.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []domain.OpenAICacheKeyMode{domain.OpenAICacheOmit, domain.OpenAICacheThread} {
		policy, _ := domain.NewCachePolicy(domain.AnthropicCacheDisabled, key, domain.OpenAIHTTPSSE)
		settings, _ := domain.NewOpenAIInference("high", "auto", 16384)
		instance, err := New(Config{Inference: settings, BaseURL: "https://example.invalid", Model: "gpt-test", APIKey: secret.New("fixture-key"), CachePolicy: policy})
		if err != nil {
			t.Fatal(err)
		}
		func() {
			defer instance.Close()
			live := testConversation(t, instance)
			var envelopes []provider.NativeCommitEnvelope
			for _, line := range bytes.Split(bytes.TrimSpace(fixture), []byte("\n")) {
				envelope, err := provider.NewNativeCommitEnvelope(domain.ProviderOpenAI, "responses", line)
				if err != nil {
					t.Fatal(err)
				}
				entry, err := decodeNativeCommit(envelope)
				if err != nil {
					t.Fatal(err)
				}
				entry.TurnID = "01900000-0000-7000-8000-000000000010"
				live.history.commit(entry)
				envelopes = append(envelopes, envelope)
			}
			restoredValue, err := testRestoreAfterPreview(t, instance, "01900000-0000-7000-8000-000000000001", testRestoreRecords(t, envelopes))
			if err != nil {
				t.Fatal(err)
			}
			restored := restoredValue.(*Conversation)
			input, err := provider.NewSamplingInput().WithToolCatalog(testOpenAIToolCatalog(t))
			if err != nil {
				t.Fatal(err)
			}
			beforeEvidence, err := live.requestEvidence(input)
			if err != nil {
				t.Fatal(err)
			}
			beforeSources := reasoningCacheSources(t, live)
			before, err := live.compileSamplingRequest(live.history.snapshot(), input)
			if err != nil {
				t.Fatal(err)
			}
			for _, conversation := range []*Conversation{live, restored} {
				view, err := conversation.ProjectTranscript()
				if err != nil {
					t.Fatal(err)
				}
				if len(view.Items) != 3 || view.Items[1].Reasoning == nil {
					t.Fatal("reasoning position lost")
				}
				body := view.Items[1].Reasoning
				page, err := body.ReadRange(0, 64<<10, false)
				if err != nil || page.Text != "  readable 中文🙂\n\n\nnext" {
					t.Fatal(page, err)
				}
				encoded, err := codec.MarshalStable(view)
				if err != nil {
					t.Fatal(err)
				}
				for _, opaque := range [][]byte{[]byte("opaque-sentinel"), []byte("raw-sentinel"), []byte("future-sentinel"), []byte("redacted-sentinel")} {
					if bytes.Contains(encoded, opaque) {
						t.Fatal("opaque data reached transcript")
					}
				}
				semantic, _ := codec.MarshalStable(conversation.ProjectHistory())
				if bytes.Contains(semantic, []byte("readable")) {
					t.Fatal("reasoning entered semantic history")
				}
				after, err := conversation.compileSamplingRequest(conversation.history.snapshot(), input)
				if err != nil {
					t.Fatal(err)
				}
				afterEvidence, err := conversation.requestEvidence(input)
				if err != nil || afterEvidence.nativeFingerprint != beforeEvidence.nativeFingerprint || !bytes.Equal(afterEvidence.properties.Bytes(), beforeEvidence.properties.Bytes()) {
					t.Fatal("restored request evidence differs", err)
				}
				if reasoningCacheSources(t, conversation) != beforeSources {
					t.Fatal("restored profile/source fingerprint differs")
				}
				if !bytes.Equal(before.Bytes(), after.Bytes()) {
					t.Fatal("reading or restore changed cached request")
				}
				a, _ := contextplan.NewSegment("reasoning-request", contextplan.StabilityTurnStable, "current", before)
				b, _ := contextplan.NewSegment("reasoning-request", contextplan.StabilityTurnStable, "current", after)
				if a.Fingerprint() != b.Fingerprint() {
					t.Fatal("display changed request fingerprint")
				}
			}
			left, _ := codec.MarshalStable(live.history.snapshot())
			right, _ := codec.MarshalStable(restored.history.snapshot())
			if !bytes.Equal(left, right) {
				t.Fatal("native item order or opaque snapshot changed on restore")
			}
			if !bytes.Contains(before.Bytes(), []byte("opaque-sentinel")) || !bytes.Contains(before.Bytes(), []byte("future-sentinel")) {
				t.Fatal("opaque native request lost")
			}
		}()
	}
}

func reasoningCacheSources(t *testing.T, c *Conversation) contextplan.CacheSources {
	t.Helper()
	return websocketCacheSources(t, c)
}
