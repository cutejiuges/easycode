package testsupport

import (
	"bytes"
	"testing"

	"easycode/internal/codec"
	contextplan "easycode/internal/context"
	"easycode/internal/provider"
)

// NativeConversation 读取实际原生状态与缓存证据，适配方法只存在于各家 _test.go。
type NativeConversation interface {
	RequestConversation
	provider.TranscriptProjector
	TestAppendFixture(*testing.T, provider.NativeCommitEnvelope)
	TestNativeJSON(*testing.T) []byte
	TestEvidence(provider.TurnInput) (provider.RequestEvidence, error)
	TestCacheSources(*testing.T) contextplan.CacheSources
}

// LoadNativeFixture 解码仓库内固定字节；不调用当前 encoder 生成期望值。
func LoadNativeFixture(t *testing.T, c NativeConversation, fixture []byte) []provider.NativeCommitEnvelope {
	t.Helper()
	var envelopes []provider.NativeCommitEnvelope
	for _, line := range bytes.Split(bytes.TrimSpace(fixture), []byte("\n")) {
		envelope, err := provider.NewNativeCommitEnvelope(c.Family(), c.Family().Wire(), line)
		if err != nil {
			t.Fatal(err)
		}
		c.TestAppendFixture(t, envelope)
		envelopes = append(envelopes, envelope)
	}
	return envelopes
}

// CheckReasoningRestore 验证读取展示不会改变两家原生事实、完整请求及缓存来源。
func CheckReasoningRestore(t *testing.T, live, restored NativeConversation, input provider.TurnInput, items int) {
	t.Helper()
	beforeEvidence, err := live.TestEvidence(input)
	if err != nil {
		t.Fatal(err)
	}
	beforeSources := live.TestCacheSources(t)
	before, err := live.TestRequest(input)
	if err != nil {
		t.Fatal(err)
	}
	for _, conversation := range []NativeConversation{live, restored} {
		view, _, err := conversation.ProjectTranscriptFrom(0)
		if err != nil {
			t.Fatal(err)
		}
		if len(view.Items) != items || view.Items[1].Reasoning == nil {
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
		after, err := conversation.TestRequest(input)
		if err != nil {
			t.Fatal(err)
		}
		afterEvidence, err := conversation.TestEvidence(input)
		if err != nil || afterEvidence.NativeFingerprint() != beforeEvidence.NativeFingerprint() || !bytes.Equal(afterEvidence.Properties().Bytes(), beforeEvidence.Properties().Bytes()) {
			t.Fatal("restored request evidence differs", err)
		}
		if conversation.TestCacheSources(t) != beforeSources {
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
	left := live.TestNativeJSON(t)
	right := restored.TestNativeJSON(t)
	if !bytes.Equal(left, right) {
		t.Fatal("native item order or opaque snapshot changed on restore")
	}
	if !bytes.Contains(before.Bytes(), []byte("opaque-sentinel")) || !bytes.Contains(before.Bytes(), []byte("future-sentinel")) {
		t.Fatal("opaque native request lost")
	}
}
