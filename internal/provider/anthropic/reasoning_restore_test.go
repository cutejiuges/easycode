package anthropic

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
	for _, mode := range []domain.AnthropicCacheMode{domain.AnthropicCacheDisabled, domain.AnthropicCache5m, domain.AnthropicCache1h} {
		policy, _ := domain.NewCachePolicy(mode, domain.OpenAICacheOmit, domain.OpenAIHTTPSSE)
		instance, err := New(Config{BaseURL: "https://example.invalid", Model: "claude-test", APIKey: secret.New("fixture-key"), CachePolicy: policy, MaxOutputTokens: 4096})
		if err != nil {
			t.Fatal(err)
		}
		func() {
			defer instance.Close()
			live := testConversation(t, instance)
			var envelopes []provider.NativeCommitEnvelope
			for _, line := range bytes.Split(bytes.TrimSpace(fixture), []byte("\n")) {
				envelope, err := provider.NewNativeCommitEnvelope(domain.ProviderAnthropic, "messages", line)
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
			input, err := provider.NewSamplingInput().WithToolCatalog(testAnthropicToolCatalog(t))
			if err != nil {
				t.Fatal(err)
			}
			before, err := live.compileSamplingRequest(live.history.snapshot(), input)
			if err != nil {
				t.Fatal(err)
			}
			for _, conversation := range []*Conversation{live, restored} {
				view, err := conversation.ProjectTranscript()
				if err != nil {
					t.Fatal(err)
				}
				if len(view.Items) != 4 || view.Items[1].Reasoning == nil {
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
