package domain_test

import (
	"bytes"
	"testing"
	"time"

	"easycode/internal/domain"
	"easycode/internal/tool"
)

func TestUUIDv7PreservesCanonicalSessionAndToolIdentity(t *testing.T) {
	stamp := time.UnixMilli(0x010203040506).UTC()
	generated, err := domain.GenerateUUIDv7(stamp, bytes.NewReader(bytes.Repeat([]byte{0xff}, 10)))
	if err != nil || generated != "01020304-0506-7fff-bfff-ffffffffffff" {
		t.Fatalf("固定时间及随机源的 UUIDv7 为 %q，错误 %v", generated, err)
	}

	thread, err := domain.ParseThreadID(generated)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := thread.Time()
	if err != nil || !parsed.Equal(stamp) {
		t.Fatalf("读取 thread 时间 %v，错误 %v", parsed, err)
	}
	if id, err := tool.ParseInvocationID(generated); err != nil || string(id) != generated {
		t.Fatalf("工具身份改变为 %q，错误 %v", id, err)
	}
	if _, err := tool.ParseApprovalRequestID("01020304-0506-7fff-Bfff-ffffffffffff"); err == nil {
		t.Fatal("批准请求不得接受非规范 UUIDv7")
	}
}
