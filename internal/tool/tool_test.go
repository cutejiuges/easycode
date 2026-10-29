package tool

import (
	"context"
	"os"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"easycode/internal/domain"
)

type inertReadExecutor struct{}

func (inertReadExecutor) Execute(context.Context, ReadInvocation) ReadExecution {
	return ReadExecution{}
}

type inertGlobExecutor struct{}

func (inertGlobExecutor) Execute(context.Context, GlobInvocation) InvocationResult {
	return InvocationResult{}
}

type inertGrepExecutor struct{}

func (inertGrepExecutor) Execute(context.Context, GrepInvocation) InvocationResult {
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

	first, err := NewCatalogSnapshot(inertReadExecutor{}, inertGlobExecutor{}, inertGrepExecutor{}, catalogPatchPreparer{}, catalogPatchExecutor{}, catalogWritePreparer{}, catalogWriteExecutor{}, catalogExecFixture{}, catalogExecFixture{}, RenderExecOutput, NewApprovalPolicy(), catalogArtifactExecutor{})
	if err != nil {
		t.Fatalf("创建目录: %v", err)
	}
	second, err := newCatalogSnapshot(reverseFacades(first.Facades()))
	if err != nil {
		t.Fatalf("以反向注册顺序创建目录: %v", err)
	}
	if first.Fingerprint() == "" || first.Fingerprint() != second.Fingerprint() || string(first.CanonicalJSON()) != string(second.CanonicalJSON()) {
		t.Fatal("目录不随注册顺序保持稳定")
	}
	golden, err := os.ReadFile("testdata/exec_catalog.golden.json")
	if err != nil || string(golden) != string(first.CanonicalJSON())+"\n" {
		t.Fatalf("catalog golden mismatch: %v; actual=%q", err, string(first.CanonicalJSON())+"\n")
	}
	for _, family := range []domain.ProviderFamily{domain.ProviderAnthropic, domain.ProviderOpenAI} {
		view, viewErr := first.View(family)
		if viewErr != nil {
			t.Fatalf("读取 %s view: %v", family, viewErr)
		}
		if len(view.Facades()) != 7 {
			t.Fatalf("%s 未完整暴露七工具", family)
		}
		names := map[string]bool{}
		readOnly := 0
		for _, facade := range view.Facades() {
			names[facade.Name()] = true
			if !facade.Capability().Exclusive() {
				readOnly++
			}
		}
		if !names["ReadArtifact"] || !names["Read"] || !names["Glob"] || !names["Grep"] || !names["Write"] || (family == domain.ProviderOpenAI && !names["apply_patch"]) || (family == domain.ProviderAnthropic && !names["Edit"]) {
			t.Fatalf("%s facade 不完整: %#v", family, names)
		}
		if readOnly != 4 {
			t.Fatal("read-only projection changed")
		}
		if family == domain.ProviderAnthropic && (!names["Edit"] || !names["Bash"]) {
			t.Fatal("Anthropic mutation facades missing")
		}
		if family == domain.ProviderOpenAI && (!names["apply_patch"] || !names["exec_command"]) {
			t.Fatal("OpenAI mutation facades missing")
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
	if _, err := NewCatalogSnapshot(nil, inertGlobExecutor{}, inertGrepExecutor{}, catalogPatchPreparer{}, catalogPatchExecutor{}, catalogWritePreparer{}, catalogWriteExecutor{}, catalogExecFixture{}, catalogExecFixture{}, RenderExecOutput, NewApprovalPolicy(), catalogArtifactExecutor{}); err == nil {
		t.Fatal("缺少 Read executor 的目录应被拒绝")
	}
	if _, err := NewCatalogSnapshot(inertReadExecutor{}, nil, inertGrepExecutor{}, catalogPatchPreparer{}, catalogPatchExecutor{}, catalogWritePreparer{}, catalogWriteExecutor{}, catalogExecFixture{}, catalogExecFixture{}, RenderExecOutput, NewApprovalPolicy(), catalogArtifactExecutor{}); err == nil {
		t.Fatal("缺少 Glob executor 的目录应被拒绝")
	}
	if _, err := NewCatalogSnapshot(inertReadExecutor{}, inertGlobExecutor{}, nil, catalogPatchPreparer{}, catalogPatchExecutor{}, catalogWritePreparer{}, catalogWriteExecutor{}, catalogExecFixture{}, catalogExecFixture{}, RenderExecOutput, NewApprovalPolicy(), catalogArtifactExecutor{}); err == nil {
		t.Fatal("缺少 Grep executor 的目录应被拒绝")
	}
	changedFacades := first.Facades()
	changedFacades[0].description += " Changed."
	changedSchema, err := newCatalogSnapshot(changedFacades)
	if err != nil {
		t.Fatal(err)
	}
	if changedSchema.Fingerprint() == first.Fingerprint() {
		t.Fatal("schema/description 变化未使 fingerprint 失效")
	}
	text := string(first.CanonicalJSON())
	for _, absent := range []string{"write_stdin", "Skill", "MCP", "Agent", "/absolute/", "secret", "input_revision", "result_codec_revision"} {
		if strings.Contains(text, absent) {
			t.Fatalf("未实现或动态内容进入目录: %q", absent)
		}
	}
	for _, size := range []int{10, 11} {
		if _, err := newCatalogSnapshot(first.Facades()[:size]); err == nil {
			t.Fatal("superseded catalog accepted")
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
	call, err := NewReadReadyCall(providerID, input)
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
	if invocation.Input() != input || !reflect.DeepEqual(call.Clone(), call) {
		t.Fatal("typed 值 clone 后不等价")
	}
	policy := NewReadOnlyPolicy()
	if policy.Decide(call) != PolicyAllow || policy.Decide(ReadyCall{}) != PolicyDeny {
		t.Fatal("ReadOnlyPolicy 决策错误")
	}
}

func TestPatchInputReadyCallAndInvocationAreClosedAndImmutable(t *testing.T) {
	t.Parallel()

	replacement, err := NewPatchReplacement("old\n", "new\n", false)
	if err != nil {
		t.Fatal(err)
	}
	source := []PatchReplacement{replacement}
	input, err := NewPatchInput("internal/tool/tool.go", source)
	if err != nil {
		t.Fatalf("创建Patch input: %v", err)
	}
	source[0] = PatchReplacement{}
	if input.Replacements()[0] != replacement {
		t.Fatal("Patch input未深拷贝调用方slice")
	}
	copyOut := input.Replacements()
	copyOut[0] = PatchReplacement{}
	if input.Replacements()[0] != replacement {
		t.Fatal("Patch input getter泄漏可变slice")
	}

	callID, _ := ParseProviderCallID("call-patch")
	call, err := NewPatchReadyCall(callID, WireCustomFreeform, input)
	if err != nil {
		t.Fatalf("创建Patch ready call: %v", err)
	}
	if call.Capability() != CapabilityPatch || call.WireKind() != WireCustomFreeform || !call.PatchInput().Equal(input) {
		t.Fatal("Patch ready call标签或payload错误")
	}
	invocationID, _ := ParseInvocationID("018f1d8a-7b5c-7def-8123-456789abcdef")
	invocation, err := NewPatchInvocation(invocationID, call)
	if err != nil || invocation.Validate() != nil || !invocation.Input().Equal(input) {
		t.Fatalf("创建Patch invocation失败: %#v %v", invocation, err)
	}
	if _, err := NewPatchReadyCall(callID, ProviderWireKind("unknown"), input); err == nil {
		t.Fatal("未知wire kind应被拒绝")
	}
	if _, err := NewReadInvocation(invocationID, call); err == nil {
		t.Fatal("Patch payload被Read invocation接受")
	}
}

func TestPatchInputRejectsInvalidPlans(t *testing.T) {
	t.Parallel()

	valid, _ := NewPatchReplacement("old", "new", false)
	tests := []struct {
		name         string
		path         string
		replacements []PatchReplacement
	}{
		{name: "空路径", path: "", replacements: []PatchReplacement{valid}},
		{name: "绝对路径", path: "/tmp/a", replacements: []PatchReplacement{valid}},
		{name: "路径穿越", path: "../a", replacements: []PatchReplacement{valid}},
		{name: "空计划", path: "a.txt"},
		{name: "重复replacement", path: "a.txt", replacements: []PatchReplacement{valid, valid}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewPatchInput(test.path, test.replacements); err == nil {
				t.Fatal("期望拒绝非法Patch计划")
			}
		})
	}
	if _, err := NewPatchReplacement("", "new", false); err == nil {
		t.Fatal("空old text应被拒绝")
	}
	if _, err := NewPatchReplacement("same", "same", false); err == nil {
		t.Fatal("空变化应被拒绝")
	}
	large, _ := NewPatchReplacement("old", strings.Repeat("x", MaxPatchInputBytes), false)
	if _, err := NewPatchInput("a.txt", []PatchReplacement{large}); err == nil {
		t.Fatal("超限Patch输入应被拒绝")
	}
}

func TestReadyCallAndInvocationRejectCapabilityPayloadMismatch(t *testing.T) {
	t.Parallel()
	callID, _ := ParseProviderCallID("call-search")
	readInput, _ := NewReadInput("README.md", 1, 20)
	globInput, _ := NewGlobInput("**/*.go", "", 10)
	grepInput, _ := NewGrepInput("TODO", "", "*.go", GrepOutputContent, false, 0, 0, 10)
	readCall, err := NewReadReadyCall(callID, readInput)
	if err != nil {
		t.Fatal(err)
	}
	globCall, err := NewGlobReadyCall(callID, globInput)
	if err != nil {
		t.Fatal(err)
	}
	grepCall, err := NewGrepReadyCall(callID, grepInput)
	if err != nil {
		t.Fatal(err)
	}
	if readCall.Capability() != CapabilityRead || globCall.Capability() != CapabilityGlob || grepCall.Capability() != CapabilityGrep {
		t.Fatal("ready call capability 标签错误")
	}
	invocationID, _ := ParseInvocationID("018f1d8a-7b5c-7def-8123-456789abcdef")
	if _, err := NewReadInvocation(invocationID, globCall); err == nil {
		t.Fatal("Glob payload 被 Read invocation 接受")
	}
	if _, err := NewGlobInvocation(invocationID, grepCall); err == nil {
		t.Fatal("Grep payload 被 Glob invocation 接受")
	}
	if _, err := NewGrepInvocation(invocationID, readCall); err == nil {
		t.Fatal("Read payload 被 Grep invocation 接受")
	}
	if (GlobInvocation{}).Validate() == nil || (GrepInvocation{}).Validate() == nil {
		t.Fatal("搜索 invocation 零值必须失败关闭")
	}
}

func TestSearchResultUnionRejectsMismatchedMetadata(t *testing.T) {
	t.Parallel()
	callID, _ := ParseProviderCallID("call-search")
	invocationID, _ := ParseInvocationID("018f1d8a-7b5c-7def-8123-456789abcdef")
	globInput, _ := NewGlobInput("**/*.go", "", 10)
	globCall, _ := NewGlobReadyCall(callID, globInput)
	globInvocation, _ := NewGlobInvocation(invocationID, globCall)
	metadata, err := NewGlobResultMetadata([]string{"internal/tool/tool.go"}, false, 0, 1, SearchComplete, SearchSkipCounts{})
	if err != nil {
		t.Fatal(err)
	}
	preview, _ := NewModelPreview("internal/tool/tool.go\n")
	result, err := NewGlobInvocationResult(globInvocation, ResultSuccess, "ok", preview, metadata)
	if err != nil || result.Capability() != CapabilityGlob || len(result.GlobMetadata().Matches()) != 1 {
		t.Fatalf("合法 Glob result 错误: %#v %v", result, err)
	}
	broken := result
	broken.capability = CapabilityGrep
	if broken.Validate() == nil {
		t.Fatal("capability 与 metadata 错配未被拒绝")
	}
}

func TestPatchResultUnionCoversTerminalStatusesAndWireKind(t *testing.T) {
	t.Parallel()

	invocation := testPatchInvocation(t)
	for _, test := range []struct {
		status ResultStatus
		code   string
	}{
		{status: ResultDenied, code: "approval_required"},
		{status: ResultConflict, code: "patch_conflict"},
		{status: ResultCancelled, code: "user_cancelled"},
		{status: ResultError, code: "patch_failed"},
		{status: ResultOutcomeUncertain, code: "outcome_uncertain"},
	} {
		result := NewPatchErrorResult(invocation, test.status, test.code, "Patch did not complete.")
		if err := result.Validate(); err != nil {
			t.Fatalf("验证%s结果: %v", test.status, err)
		}
		if result.Status() != test.status || result.Code() != test.code || result.WireKind() != WireCustomFreeform || result.Capability() != CapabilityPatch {
			t.Fatalf("Patch结果标签错误: %#v", result)
		}
	}

	base := []byte("old\n")
	evidence, _ := NewFileBaseEvidence(NewContentDigest(base), int64(len(base)), 0o644, 1, 2)
	metadata, _ := NewPatchResultMetadata("main.go", 1, 1, 1)
	fingerprint, _ := PatchFingerprint(invocation.Input(), evidence, []byte("new\n"))
	candidate, _ := NewPatchCandidate(invocation, evidence, []byte("new\n"), "--- a/main.go\n+++ b/main.go\n-old\n+new\n", fingerprint, metadata)
	success := NewPatchSuccessResult(candidate)
	if err := success.Validate(); err != nil || success.Status() != ResultSuccess || success.Code() != "ok" || success.PatchMetadata() != metadata {
		t.Fatalf("Patch成功结果错误: %#v %v", success, err)
	}
	broken := success
	broken.wireKind = ProviderWireKind("unknown")
	if broken.Validate() == nil {
		t.Fatal("非法Patch result wire kind未被拒绝")
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
	if first.ReadMetadata().ReachedEOF() || first.ReadMetadata().StartLine() != 3 || first.ReadMetadata().EndLine() != 4 {
		t.Fatalf("范围元数据错误: %#v", first.ReadMetadata())
	}

	longInvocation := testInvocation(t, "unicode.txt", 1, 2000)
	long := RenderReadSuccess(longInvocation, "unicode.txt", []string{strings.Repeat("界", 2001)}, 1)
	if !long.ReadMetadata().LongLineTruncated() || !utf8.ValidString(long.Preview().Text()) || !strings.Contains(long.Preview().Text(), "… [line truncated]") {
		t.Fatal("长行未在 Unicode code point 边界稳定截断")
	}
	lines := make([]string, 2000)
	for index := range lines {
		lines[index] = strings.Repeat("界", 2000)
	}
	bounded := RenderReadSuccess(longInvocation, "unicode.txt", lines, 4000)
	if len(bounded.Preview().Text()) > MaxModelPreviewBytes || !utf8.ValidString(bounded.Preview().Text()) || !bounded.ReadMetadata().OutputTruncated() {
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
	call, _ := NewReadReadyCall(callID, input)
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

// catalogPatchPreparer 只用于验证目录装配，测试不会通过它访问文件系统。
type catalogPatchPreparer struct{}

func (catalogPatchPreparer) Prepare(context.Context, PatchInvocation) (PatchCandidate, error) {
	return PatchCandidate{}, nil
}

type catalogPatchExecutor struct{}

func (catalogPatchExecutor) Execute(context.Context, PatchCandidate) InvocationResult {
	return InvocationResult{}
}

// catalogWritePreparer 只用于验证目录装配，测试不会通过它访问文件系统。
type catalogWritePreparer struct{}

func (catalogWritePreparer) Prepare(context.Context, WriteInvocation, ReadObservation) (WriteCandidate, error) {
	return WriteCandidate{}, nil
}

type catalogWriteExecutor struct{}

func (catalogWriteExecutor) Execute(context.Context, WriteCandidate) InvocationResult {
	return InvocationResult{}
}
