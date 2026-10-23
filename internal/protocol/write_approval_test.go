package protocol

import (
	"easycode/internal/codec"
	"strings"
	"testing"
)

func TestWritePresentationStrictUnionAndFrozenDirectoryPlan(t *testing.T) {
	dirs := []string{"新建", "新建/pkg"}
	create, err := NewWriteCreateApprovalPresentation("(empty file)\n", 0, 0, dirs)
	if err != nil {
		t.Fatal(err)
	}
	dirs[0] = "changed"
	if create.WriteCreate.Directories[0] != "新建" || create.Capability() != "fs.write" || !strings.Contains(create.Text(), "0 bytes, 0 lines") {
		t.Fatal("创建展示未冻结")
	}
	overwrite, err := NewWriteOverwriteApprovalPresentation("-old\n+new\n", 3, 1)
	if err != nil {
		t.Fatal(err)
	}
	patch, err := NewPatchApprovalPresentation("-old\n+new\n")
	if err != nil {
		t.Fatal(err)
	}
	for _, original := range []ApprovalPresentation{create, overwrite, patch} {
		encoded, err := codec.MarshalStable(original)
		if err != nil {
			t.Fatal(err)
		}
		var restored ApprovalPresentation
		if err := codec.UnmarshalStrict(encoded, &restored); err != nil || restored.Text() != original.Text() {
			t.Fatalf("展示 roundtrip: %v", err)
		}
	}
	for _, raw := range []string{
		`{}`, `{"kind":"write-create"}`, `{"kind":"write-create","write_create":null}`,
		`{"kind":"write-create","write_create":{"preview":"empty","bytes":0,"directories":[]}}`,
		`{"kind":"write-create","write_create":{"preview":"empty","bytes":0,"lines":0,"directories":null}}`,
		`{"kind":"write-create","write_create":{"preview":"empty","bytes":0,"bytes":0,"lines":0,"directories":[]}}`,
		`{"kind":"write-create","write_create":{"preview":"empty","bytes":0,"lines":0,"directories":[],"extra":1}}`,
		`{"kind":"write-create","write_create":{"preview":"empty","bytes":0,"lines":0,"directories":[]},"patch":null}`,
		`{"kind":"write-overwrite","write_overwrite":{"diff":"x","bytes":-1,"lines":0}}`,
		`{"kind":"write-overwrite","write_overwrite":{"diff":"\u001b[31m","bytes":1,"lines":1}}`,
		`{"kind":"write-overwrite","write_overwrite":{"diff":"\u202e","bytes":1,"lines":1}}`,
		`{"kind":"write-overwrite","write_overwrite":{"diff":"x","bytes":1,"lines":2}}`,
		`{"kind":"patch","patch":{"diff":"x"},"write_create":{}}`,
		`{"kind":"write-create","kind":"patch","patch":{"diff":"x"}}`,
	} {
		var p ApprovalPresentation
		if codec.UnmarshalStrict([]byte(raw), &p) == nil {
			t.Fatalf("接受非法展示 %s", raw)
		}
	}
	if _, err := NewWriteCreateApprovalPresentation("x", 1, 1, []string{"a", "b"}); err == nil {
		t.Fatal("接受断裂目录计划")
	}
	if got := VisibleApprovalText("中文\x1b\r\u202e\\"); got != `中文\u{001B}\u{000D}\u{202E}\\` {
		t.Fatal(got)
	}
}

func TestWriteApprovalRequiresMatchingCapabilityAndDecisions(t *testing.T) {
	p, _ := NewWriteCreateApprovalPresentation("empty file\n", 0, 0, nil)
	request := ToolApprovalRequestedPayload{
		RequestID: "018f1d8a-7b5c-7def-8123-456789abcdef", InvocationID: "018f1d8a-7b5c-7def-8abc-456789abcdef", ProviderCallID: "write-call", Capability: "fs.write", Reason: "Write creates a file.", RelativePath: "a", Fingerprint: strings.Repeat("a", 64), Presentation: p, Decisions: []ApprovalDecision{ApprovalAllowOnce, ApprovalAllowSession, ApprovalDeny},
	}
	if _, err := NewToolApprovalRequested(request); err != nil {
		t.Fatal(err)
	}
	request.Capability = "fs.patch"
	if _, err := NewToolApprovalRequested(request); err == nil {
		t.Fatal("capability 错配被接受")
	}
	request.Capability = "fs.write"
	request.Decisions = []ApprovalDecision{ApprovalAllowOnce}
	if _, err := NewToolApprovalRequested(request); err == nil {
		t.Fatal("缺失拒绝选项被接受")
	}
}
