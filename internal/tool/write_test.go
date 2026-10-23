package tool

import (
	"strings"
	"testing"
)

func writeInvocationFor(t *testing.T, path, content string) WriteInvocation {
	t.Helper()
	input, err := NewWriteInput(path, content)
	if err != nil {
		t.Fatal(err)
	}
	call, err := NewWriteReadyCall("write-call", input)
	if err != nil {
		t.Fatal(err)
	}
	id, err := GenerateInvocationID()
	if err != nil {
		t.Fatal(err)
	}
	v, err := NewWriteInvocation(id, call)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestWriteStrictInput(t *testing.T) {
	for _, data := range []string{
		`{}`, `null`, `[]`, `{"file_path":"a"}`, `{"content":"x"}`, `{"file_path":"a","content":null}`,
		`{"file_path":null,"content":""}`, `{"file_path":"a","content":"x","content":"y"}`,
		`{"file_path":"a","content":"","extra":""}`, `{"file_path":"a","content":""}{}`,
		`{"file_path":"a","content":"\ud800"}`, `{"file_path":"a","content":"\udc00"}`,
		`{"file_path":"a","content":"\ud800\u1234"}`, `{"file_path":"a","content":"\u0000"}`,
		"{\"file_path\":\"a\",\"content\":\"\xff\"}",
	} {
		if _, err := DecodeWriteInput([]byte(data)); err == nil {
			t.Fatalf("接受了非法输入 %q", data)
		}
	}
	for _, content := range []string{"", "\ufeff中文\r\n尾行", "\n", "😀", `\ud800`} {
		input, err := NewWriteInput("src/a.go", content)
		if err != nil || input.Content() != content {
			t.Fatalf("内容被修改或拒绝: %v", err)
		}
	}
	value, err := DecodeWriteInput([]byte(`{"file_path":"a","content":"\ud83d\ude00"}`))
	if err != nil || value.Content() != "😀" {
		t.Fatal("合法代理项未无损解码", err)
	}
	for _, path := range []string{"", "/a", "a//b", "a/./b", "a/../b", "a\\b", "a\n", strings.Repeat("a/", 64) + "a", strings.Repeat("a", 4097)} {
		if _, err := NewWriteInput(path, ""); err == nil {
			t.Fatalf("接受非法路径 %q", path)
		}
	}
	for _, path := range []string{strings.Repeat("a", 4096), strings.Repeat("a/", 63) + "a"} {
		if _, err := NewWriteInput(path, ""); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := NewWriteInput("a", strings.Repeat("x", MaxWriteInputBytes-len(`{"file_path":"a","content":""}`))); err != nil {
		t.Fatal(err)
	}
	if _, err := NewWriteInput("a", strings.Repeat("x", MaxWriteInputBytes)); err == nil {
		t.Fatal("未限制规范化参数")
	}
	prefix, suffix := `{"file_path":"a","content":"`, `"}`
	data := prefix + strings.Repeat("x", MaxWriteInputBytes-len(prefix)-len(suffix)) + suffix
	if _, err := DecodeWriteInput([]byte(data)); err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeWriteInput([]byte(data + " ")); err == nil {
		t.Fatal("未限制 JSON bytes")
	}
}

func TestWriteCandidateBindingAndFrozenResults(t *testing.T) {
	v := writeInvocationFor(t, "new/empty", "")
	root, _ := NewDirectoryEvidence(".", 1, 2, 0o700)
	ancestors := []DirectoryEvidence{root}
	dirs := []string{"new"}
	c, err := NewWriteCandidate(v, WriteCreate, FileBaseEvidence{}, ancestors, dirs, "Empty file (0 bytes, 0 lines)")
	if err != nil {
		t.Fatal(err)
	}
	ancestors[0] = DirectoryEvidence{}
	dirs[0] = "other"
	c.Ancestors()[0] = DirectoryEvidence{}
	c.Directories()[0] = "other"
	if c.Validate() != nil || c.Directories()[0] != "new" {
		t.Fatal("候选未深拷贝")
	}
	r := NewWriteCandidateResult(c, ResultSuccess, "ok")
	if r.Validate() != nil || r.WriteMetadata().Operation() != WriteCreate || r.WriteMetadata().Bytes() != 0 || r.WriteMetadata().Lines() != 0 || r.WriteMetadata().CreatedDirectories() != 1 {
		t.Fatal("空文件摘要错误", r)
	}
	broken := c
	broken.operation = WriteOverwrite
	if broken.Validate() == nil {
		t.Fatal("接受混合 operation")
	}
	broken = c
	broken.fingerprint = NewCandidateFingerprint([]byte("other"))
	if broken.Validate() == nil {
		t.Fatal("接受伪造 fingerprint")
	}
	if (WriteCandidate{}).Validate() == nil || (WriteResultMetadata{}).Validate() == nil || (ReadObservation{}).Validate() == nil {
		t.Fatal("接受零值")
	}
	unknown, _ := NewWriteResultMetadata(v.Input().FilePath(), WriteOperationUnavailable, 0, 0, 0)
	preview, _ := NewModelPreview("summary")
	if _, err := NewWriteInvocationResult(v, ResultSuccess, "ok", preview, unknown); err == nil {
		t.Fatal("成功操作不可未知")
	}
	for text, lines := range map[string]int{"": 0, "a": 1, "a\n": 1, "a\r\n": 1, "\n\n": 2, "a\nb": 2} {
		if WriteLineCount(text) != lines {
			t.Fatalf("行数错误 %q", text)
		}
	}
	secret := "synthetic-body-never-in-result"
	v = writeInvocationFor(t, "a", secret)
	c, err = NewWriteCandidate(v, WriteCreate, FileBaseEvidence{}, []DirectoryEvidence{root}, nil, "+"+secret)
	if err != nil {
		t.Fatal(err)
	}
	bytes := c.FinalBytes()
	bytes[0] = 'X'
	if string(c.FinalBytes()) != secret {
		t.Fatal("final bytes 未复制")
	}
	r = NewWriteCandidateResult(c, ResultSuccess, "ok")
	if strings.Contains(r.Preview().Text(), secret) || r.WriteMetadata().Bytes() != len(secret) {
		t.Fatal("成功结果泄漏正文或计数错误")
	}
}

func TestWriteReadyAndInvocationRejectMixedBranches(t *testing.T) {
	v := writeInvocationFor(t, "a", "x")
	call, _ := NewWriteReadyCall(v.ProviderCallID(), v.Input())
	call.readInput, _ = NewReadInput("a", 1, 1)
	if call.Validate() == nil {
		t.Fatal("接受混合输入")
	}
	call, _ = NewWriteReadyCall(v.ProviderCallID(), v.Input())
	i, err := NewInvocation(v.InvocationID(), call)
	if err != nil || i.Validate() != nil || i.WireKind() != WireStructured {
		t.Fatal(err)
	}
	i.capability = CapabilityRead
	if i.Validate() == nil {
		t.Fatal("接受混合调用")
	}
	if NewApprovalPolicy().Decide(call) != PolicyAsk {
		t.Fatal("Write 必须询问")
	}
}
