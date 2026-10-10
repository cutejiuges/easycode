package builtin

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"easycode/internal/tool"
)

func TestSearchPreviewIsDeterministicBoundedAndKeepsHeadTail(t *testing.T) {
	t.Parallel()
	lines := make([]string, 1000)
	for index := range lines {
		lines[index] = fmt.Sprintf("%04d:%s", index, strings.Repeat("界", 500))
	}
	first, omitted := renderSearchPreview(lines)
	second, secondOmitted := renderSearchPreview(lines)
	if first.Text() != second.Text() || omitted != secondOmitted {
		t.Fatal("搜索 preview 不确定")
	}
	if len(first.Text()) > tool.MaxSearchPreviewBytes || !utf8.ValidString(first.Text()) || omitted == 0 {
		t.Fatalf("preview 预算错误: bytes=%d omitted=%d", len(first.Text()), omitted)
	}
	if !strings.Contains(first.Text(), "0000:") || !strings.Contains(first.Text(), "0999:") || !strings.Contains(first.Text(), "results omitted") {
		t.Fatal("preview 未保留稳定首尾或省略 marker")
	}
}

func TestSearchPreviewTruncatesAtCodePointBoundary(t *testing.T) {
	t.Parallel()
	preview, omitted := renderSearchPreview([]string{strings.Repeat("界", 501)})
	if omitted != 0 || !utf8.ValidString(preview.Text()) || !strings.Contains(preview.Text(), "[line truncated]") {
		t.Fatalf("长行截断错误: omitted=%d preview=%q", omitted, preview.Text())
	}
	line := strings.TrimSuffix(preview.Text(), "... [line truncated]\n")
	if utf8.RuneCountInString(line) != 500 {
		t.Fatalf("长行保留 code points = %d", utf8.RuneCountInString(line))
	}
}
