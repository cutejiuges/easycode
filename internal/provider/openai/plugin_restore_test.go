package openai

import (
	"bytes"
	"easycode/internal/codec"
	"easycode/internal/domain"
	"easycode/internal/provider"
	"easycode/internal/secret"
	"os"
	"reflect"
	"testing"
)

func TestPluginFixedNativeRestorePreservesOpaqueOrderCacheAndPrivateRevision(t *testing.T) {
	fixture, err := os.ReadFile("testdata/plugin_restore.current.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	settings, _ := domain.NewOpenAIInference("high", "auto", 16384)
	instance, err := New(Config{BaseURL: "https://example.invalid", Model: "fixture-model", APIKey: secret.New("fixture-key"), Inference: settings})
	if err != nil {
		t.Fatal(err)
	}
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
	value, err := testRestoreAfterPreview(t, instance, "01900000-0000-7000-8000-000000000001", testRestoreRecords(t, envelopes))
	if err != nil {
		t.Fatal(err)
	}
	restored := value.(*Conversation)
	source, _ := domain.NewSkillSource("plugin", "qa", "codex")
	metadata, _ := domain.NewSkillMetadata(source, "qa:demo", "on demand", "", true, false)
	catalog, _ := domain.NewSkillCatalog([]domain.SkillMetadata{metadata})
	sampling, _ := provider.NewSamplingInput().WithToolCatalog(testOpenAIToolCatalog(t))
	sampling, _ = sampling.WithSkillCatalog(catalog)
	before, err := live.compileSamplingRequest(live.history.snapshot(), sampling)
	if err != nil {
		t.Fatal(err)
	}
	after, err := restored.compileSamplingRequest(restored.history.snapshot(), sampling)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := codec.MarshalStable(live.history.snapshot())
	b, _ := codec.MarshalStable(restored.history.snapshot())
	fa, ea := live.HistoryFootprint()
	fb, eb := restored.HistoryFootprint()
	if !bytes.Equal(a, b) || !bytes.Equal(before.Bytes(), after.Bytes()) || ea != nil || eb != nil || !reflect.DeepEqual(fa, fb) || reasoningCacheSources(t, live) != reasoningCacheSources(t, restored) {
		t.Fatal("plugin restore changed request, native order, footprint or fingerprints")
	}
	for _, content := range []string{"Frozen skill instructions", "opaque-sentinel", "future-sentinel"} {
		if !bytes.Contains(before.Bytes(), []byte(content)) {
			t.Fatal("lost frozen or opaque content", content)
		}
	}
	changed := testConversation(t, instance)
	for i, entry := range live.history.snapshot() {
		if i == 0 {
			entry.InputMetadata.Skills[0].Attachment.Revision = domain.SkillDigest("different private binding")
		}
		changed.history.commit(entry)
	}
	private, _ := changed.compileSamplingRequest(changed.history.snapshot(), sampling)
	footprint, _ := changed.HistoryFootprint()
	if !bytes.Equal(before.Bytes(), private.Bytes()) || !reflect.DeepEqual(fa, footprint) || reasoningCacheSources(t, live) != reasoningCacheSources(t, changed) {
		t.Fatal("private attachment revision polluted model-visible bytes")
	}
	forged := bytes.Replace(bytes.Split(bytes.TrimSpace(fixture), []byte("\n"))[0], []byte(`"directory":"qa"`), []byte(`"directory":"docs"`), 1)
	envelope, err := provider.NewNativeCommitEnvelope(domain.ProviderOpenAI, "responses", forged)
	if err == nil {
		if _, err = decodeNativeCommit(envelope); err == nil {
			t.Fatal("forged plugin namespace accepted")
		}
	}
}
