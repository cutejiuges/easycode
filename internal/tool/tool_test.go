package tool

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"

	"easycode/internal/domain"
)

type inertReadExecutor struct{}

func (inertReadExecutor) Execute(context.Context, ReadInvocation) InvocationResult {
	return InvocationResult{}
}

func TestReadInputStrictDecodeAndBounds(t *testing.T) {
	t.Parallel()

	input, err := DecodeReadInput([]byte(`{"file_path":"src/main.go"}`))
	if err != nil {
		t.Fatalf("解码默认 Read 输入: %v", err)
	}
	if input.FilePath() != "src/main.go" || input.Offset() != 1 || input.Limit() != 2000 {
		t.Fatalf("默认值错误: %#v", input)
	}

	tests := []struct {
		name string
		data []byte
	}{
		{name: "重复字段", data: []byte(`{"file_path":"a","file_path":"b"}`)},
		{name: "未知字段", data: []byte(`{"file_path":"a","extra":true}`)},
		{name: "尾随对象", data: []byte(`{"file_path":"a"}{}`)},
		{name: "非法 UTF-8", data: []byte{'{', '"', 'f', 'i', 'l', 'e', '_', 'p', 'a', 't', 'h', '"', ':', '"', 0xff, '"', '}'}},
		{name: "缺少路径", data: []byte(`{"offset":1}`)},
		{name: "空路径", data: []byte(`{"file_path":""}`)},
		{name: "offset 零", data: []byte(`{"file_path":"a","offset":0}`)},
		{name: "offset 负数", data: []byte(`{"file_path":"a","offset":-1}`)},
		{name: "offset 小数", data: []byte(`{"file_path":"a","offset":1.5}`)},
		{name: "limit 零", data: []byte(`{"file_path":"a","limit":0}`)},
		{name: "limit 超限", data: []byte(`{"file_path":"a","limit":2001}`)},
		{name: "limit 小数", data: []byte(`{"file_path":"a","limit":1.0}`)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, decodeErr := DecodeReadInput(test.data); decodeErr == nil {
				t.Fatal("期望 strict decoder 拒绝输入")
			}
		})
	}
}

func TestCatalogIsDeterministicImmutableAndReadOnly(t *testing.T) {
	t.Parallel()

	first, err := NewReadCatalogSnapshot(inertReadExecutor{})
	if err != nil {
		t.Fatalf("创建目录: %v", err)
	}
	second, err := newCatalogSnapshot(CatalogRevision, reverseFacades(first.Facades()))
	if err != nil {
		t.Fatalf("以反向注册顺序创建目录: %v", err)
	}
	if first.Revision() == "" || first.Fingerprint() != second.Fingerprint() || string(first.CanonicalJSON()) != string(second.CanonicalJSON()) {
		t.Fatal("目录不随注册顺序保持稳定")
	}
	for _, family := range []domain.ProviderFamily{domain.ProviderAnthropic, domain.ProviderOpenAI} {
		view, viewErr := first.View(family)
		if viewErr != nil {
			t.Fatalf("读取 %s view: %v", family, viewErr)
		}
		if len(view.Facades()) != 1 || view.Facades()[0].Name() != "Read" {
			t.Fatalf("%s 未只暴露 Read", family)
		}
		bytes := view.CanonicalJSON()
		bytes[0] = '['
		if first.Validate() != nil {
			t.Fatal("调用方修改 view bytes 污染了目录")
		}
	}
	canonical := first.CanonicalJSON()
	canonical[0] = '['
	facades := first.Facades()
	schema := facades[0].InputSchema()
	schema[0] = '['
	if err := first.Validate(); err != nil {
		t.Fatalf("调用方修改 getter 污染目录: %v", err)
	}
	if _, err := NewReadCatalogSnapshot(nil); err == nil {
		t.Fatal("未绑定 executor 的目录应被拒绝")
	}
	changedFacades := first.Facades()
	changedFacades[0].description += " Changed."
	changedSchema, err := newCatalogSnapshot(CatalogRevision, changedFacades)
	if err != nil {
		t.Fatal(err)
	}
	changedRevision, err := newCatalogSnapshot("tool-catalog.v2", first.Facades())
	if err != nil {
		t.Fatal(err)
	}
	if changedSchema.Fingerprint() == first.Fingerprint() || changedRevision.Fingerprint() == first.Fingerprint() {
		t.Fatal("schema 或 catalog revision 变化未使 fingerprint 失效")
	}
	text := string(first.CanonicalJSON())
	for _, absent := range []string{"Glob", "Grep", "Edit", "Write", "Bash", "Skill", "MCP", "Agent", "/absolute/", "secret"} {
		if strings.Contains(text, absent) {
			t.Fatalf("未实现或动态内容进入目录: %q", absent)
		}
	}
}

func TestReadyCallInvocationAndPolicyRejectZeroValues(t *testing.T) {
	t.Parallel()

	if (ReadyCall{}).Validate() == nil || (ReadInvocation{}).Validate() == nil || (InvocationResult{}).Validate() == nil {
		t.Fatal("零值契约必须失败关闭")
	}
	input, _ := NewReadInput("README.md", 1, 20)
	providerID, _ := ParseProviderCallID("call-1")
	call, err := NewReadyCall(providerID, input)
	if err != nil {
		t.Fatalf("创建 ready call: %v", err)
	}
	invocationID, err := ParseInvocationID("018f1d8a-7b5c-7def-8123-456789abcdef")
	if err != nil {
		t.Fatalf("测试 invocation ID 无效: %v", err)
	}
	invocation, err := NewReadInvocation(invocationID, call)
	if err != nil {
		t.Fatalf("创建 invocation: %v", err)
	}
	if invocation.Input() != input || call.Clone() != call {
		t.Fatal("typed 值 clone 后不等价")
	}
	policy := NewReadOnlyPolicy()
	if policy.Decide(call) != PolicyAllow || policy.Decide(ReadyCall{}) != PolicyDeny {
		t.Fatal("ReadOnlyPolicy 决策错误")
	}
}

func TestReadRendererIsDeterministicBoundedAndUTF8Safe(t *testing.T) {
	t.Parallel()

	invocation := testInvocation(t, "src/main.go", 3, 2)
	first := RenderReadSuccess(invocation, "src/main.go", []string{"alpha", "beta"}, 8)
	second := RenderReadSuccess(invocation, "src/main.go", []string{"alpha", "beta"}, 8)
	if err := first.Validate(); err != nil {
		t.Fatalf("验证成功结果: %v", err)
	}
	if first.Preview().Text() != "3\talpha\n4\tbeta\n" || first.Preview().Text() != second.Preview().Text() {
		t.Fatalf("行号或确定性错误: %q", first.Preview().Text())
	}
	if first.Metadata().ReachedEOF() || first.Metadata().StartLine() != 3 || first.Metadata().EndLine() != 4 {
		t.Fatalf("范围元数据错误: %#v", first.Metadata())
	}

	longInvocation := testInvocation(t, "unicode.txt", 1, 2000)
	long := RenderReadSuccess(longInvocation, "unicode.txt", []string{strings.Repeat("界", 2001)}, 1)
	if !long.Metadata().LongLineTruncated() || !utf8.ValidString(long.Preview().Text()) || !strings.Contains(long.Preview().Text(), "… [line truncated]") {
		t.Fatal("长行未在 Unicode code point 边界稳定截断")
	}
	lines := make([]string, 2000)
	for index := range lines {
		lines[index] = strings.Repeat("界", 2000)
	}
	bounded := RenderReadSuccess(longInvocation, "unicode.txt", lines, 4000)
	if len(bounded.Preview().Text()) > MaxModelPreviewBytes || !utf8.ValidString(bounded.Preview().Text()) || !bounded.Metadata().OutputTruncated() {
		t.Fatal("总 preview 预算未生效")
	}
}

func reverseFacades(source []Facade) []Facade {
	result := make([]Facade, len(source))
	for index := range source {
		result[len(source)-1-index] = source[index]
	}
	return result
}

func testInvocation(t *testing.T, path string, offset int, limit int) ReadInvocation {
	t.Helper()
	input, err := NewReadInput(path, offset, limit)
	if err != nil {
		t.Fatal(err)
	}
	callID, _ := ParseProviderCallID("call-read")
	call, _ := NewReadyCall(callID, input)
	invocationID, err := ParseInvocationID("018f1d8a-7b5c-7def-8123-456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	invocation, err := NewReadInvocation(invocationID, call)
	if err != nil {
		t.Fatal(err)
	}
	return invocation
}
