package testsupport

import (
	"bytes"
	"testing"

	"easycode/internal/codec"
	contextplan "easycode/internal/context"
	"easycode/internal/provider"
)

// RequestConversation 只在测试中补充真实请求字节入口，不改变生产 Provider 接口。
type RequestConversation interface {
	provider.Conversation
	TestRequest(provider.TurnInput) (codec.CanonicalJSON, error)
}

// RestoredRequest 对照实际请求、指纹与窗口；调用方继续核对各自的固定 native fixture。
func RestoredRequest(t *testing.T, live, restored RequestConversation, input provider.TurnInput) codec.CanonicalJSON {
	t.Helper()
	before, err := live.TestRequest(input)
	if err != nil {
		t.Fatal(err)
	}
	after, err := restored.TestRequest(input)
	if err != nil || !bytes.Equal(before.Bytes(), after.Bytes()) {
		t.Fatal("restore changed canonical request or fingerprint", err)
	}
	first, err := contextplan.NewSegment("request", contextplan.StabilityTurnStable, "current", before)
	if err != nil {
		t.Fatal(err)
	}
	second, err := contextplan.NewSegment("request", contextplan.StabilityTurnStable, "current", after)
	if err != nil || first.Fingerprint() != second.Fingerprint() {
		t.Fatal("restore changed cache segment fingerprint", err)
	}
	_, a, err := live.ProjectContextWindow()
	if err != nil {
		t.Fatal(err)
	}
	_, b, err := restored.ProjectContextWindow()
	if err != nil || a != b {
		t.Fatal("restore changed context footprint", err)
	}
	return before
}
