package openai

import (
	"bytes"
	"os"
	"testing"

	"easycode/internal/provider"
)

func testPlanModeInput(t *testing.T, input provider.TurnInput) provider.TurnInput {
	t.Helper()
	catalog, err := testOpenAIToolCatalog(t).ForPlanMode()
	if err != nil {
		t.Fatal(err)
	}
	planned, err := input.WithPlanMode(catalog)
	if err != nil {
		t.Fatal(err)
	}
	return planned
}

func assertPlanExecRestore(t *testing.T, live, restored *Conversation) {
	t.Helper()
	input := testPlanModeInput(t, testSamplingInput(t, live.provider.config.Inference))
	before, err := live.compileSamplingRequest(live.history.Entries(), input)
	if err != nil {
		t.Fatal(err)
	}
	after, err := restored.compileSamplingRequest(restored.history.Entries(), input)
	if err != nil || !before.Equal(after) || before.SHA256() != after.SHA256() {
		t.Fatal("restored planning request bytes or fingerprint differ", err)
	}
	opened, err := live.requestEvidence(input)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := restored.requestEvidence(input)
	if err != nil || opened.NativeFingerprint() != replayed.NativeFingerprint() || !opened.Properties().Equal(replayed.Properties()) {
		t.Fatal("restored planning request evidence differs", err)
	}
	want, err := os.ReadFile("testdata/plan_mode_exec_continuation.golden.json")
	if err != nil || !bytes.Equal(before.Bytes(), bytes.TrimSpace(want)) {
		t.Fatalf("planning request golden mismatch: %v\n%s", err, before.Bytes())
	}
}
