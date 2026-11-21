package anthropic

import (
	"bytes"
	"os"
	"testing"

	"easycode/internal/codec"
	"easycode/internal/domain"
	"easycode/internal/provider"
)

func assertPlanExecRestore(t *testing.T, live, restored *Conversation) {
	t.Helper()
	full := testAnthropicToolCatalog(t)
	filtered, err := full.ForPlanMode()
	if err != nil {
		t.Fatal(err)
	}
	base := testSamplingInput(t, live.provider.config.Inference)
	if _, err := base.WithPlanMode(full); err == nil {
		t.Fatal("Plan mode accepted the complete catalog")
	}
	if _, err := base.WithToolCatalog(filtered); err == nil {
		t.Fatal("Default mode accepted the restricted catalog")
	}
	input, err := base.WithPlanMode(filtered)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.NewCompactionInput(input, "", 4096); err == nil {
		t.Fatal("compaction accepted ordinary planning sources")
	}
	current, err := live.planSamplingRequest(live.history.Snapshot(), input)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := restored.planSamplingRequest(restored.history.Snapshot(), input)
	if err != nil {
		t.Fatal(err)
	}
	a, err := current.evidence()
	if err != nil {
		t.Fatal(err)
	}
	b, err := replayed.evidence()
	if err != nil || a.NativeFingerprint() != b.NativeFingerprint() {
		t.Fatal("restored planning native fingerprint differs", err)
	}
	for _, mode := range []domain.AnthropicCacheMode{domain.AnthropicCacheDisabled, domain.AnthropicCache5m} {
		compiled, err := current.compile(mode)
		if err != nil {
			t.Fatal(err)
		}
		var wire messagesRequest
		if err := codec.Unmarshal(compiled.body.Bytes(), &wire); err != nil || wire.System != domain.WorkModePlan.Instructions() || len(wire.Tools) != 7 {
			t.Fatalf("%s lost planning instructions or tool catalog: %v", mode, err)
		}
	}
	cached, err := current.compile(domain.AnthropicCache1h)
	if err != nil {
		t.Fatal(err)
	}
	restoredCache, err := replayed.compile(domain.AnthropicCache1h)
	if err != nil || !cached.body.Equal(restoredCache.body) || cached.body.SHA256() != restoredCache.body.SHA256() {
		t.Fatal("restored planning request bytes or fingerprint differ", err)
	}
	want, err := os.ReadFile("testdata/plan_mode_exec_continuation.golden.json")
	if err != nil || !bytes.Equal(cached.body.Bytes(), bytes.TrimSpace(want)) || cached.markers != 2 {
		t.Fatalf("cached planning request or marker count differs: %v\n%s", err, cached.body.Bytes())
	}
}
