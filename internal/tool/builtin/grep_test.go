package builtin

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"easycode/internal/tool"
)

func TestGrepContentOrdersMatchesAndDeduplicatesContext(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeSearchFile(t, root, "b.go", "zero\nTODO one\ncontext\nTODO two\ntail")
	writeSearchFile(t, root, "a.go", "TODO first\nafter")
	executor := openTestGrepExecutor(t, root)
	result := executor.Execute(context.Background(), grepInvocationFor(t, "TODO", "", "*.go", tool.GrepOutputContent, false, 1, 1, 250))
	if result.Status() != tool.ResultSuccess {
		t.Fatalf("Grep 失败: %s/%s", result.Status(), result.Code())
	}
	matches := result.GrepMetadata().Matches()
	if result.GrepMetadata().MatchingLines() != 3 || len(matches) != 7 {
		t.Fatalf("匹配/上下文计数错误: matching=%d matches=%d", result.GrepMetadata().MatchingLines(), len(matches))
	}
	paths := make([]string, len(matches))
	for index := range matches {
		paths[index] = matches[index].RelativePath()
	}
	if !reflect.DeepEqual(paths, []string{"a.go", "a.go", "b.go", "b.go", "b.go", "b.go", "b.go"}) {
		t.Fatalf("顺序错误: %#v", paths)
	}
	if strings.Count(result.Preview().Text(), "b.go-3-context") != 1 {
		t.Fatalf("重叠 context 未去重: %q", result.Preview().Text())
	}
}

func TestGrepFilesCountCaseAndLineCountSemantics(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeSearchFile(t, root, "a.txt", "todo TODO\nnone\nTODO")
	writeSearchFile(t, root, "b.txt", "none")
	executor := openTestGrepExecutor(t, root)
	files := executor.Execute(context.Background(), grepInvocationFor(t, "todo", "", "*.txt", tool.GrepOutputFilesWithMatches, true, 0, 0, 250))
	if got := files.Preview().Text(); got != "a.txt\n" {
		t.Fatalf("files mode = %q", got)
	}
	count := executor.Execute(context.Background(), grepInvocationFor(t, "TODO", "", "*.txt", tool.GrepOutputCount, false, 0, 0, 250))
	if got := count.Preview().Text(); got != "a.txt:2\n" || count.GrepMetadata().MatchingLines() != 2 {
		t.Fatalf("count mode = %q matching=%d", got, count.GrepMetadata().MatchingLines())
	}
}

func TestGrepSkipsBinaryInvalidUTF8AndOversize(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeSearchFile(t, root, "ok.txt", "needle")
	if err := os.WriteFile(filepath.Join(root, "binary"), []byte{'x', 0, 'y'}, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "invalid"), []byte{0xff}, 0o600); err != nil {
		t.Fatal(err)
	}
	large, err := os.OpenFile(filepath.Join(root, "large"), os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := large.Truncate(maxReadFileBytes + 1); err != nil {
		t.Fatal(err)
	}
	_ = large.Close()
	executor := openTestGrepExecutor(t, root)
	result := executor.Execute(context.Background(), grepInvocationFor(t, "needle", "", "", tool.GrepOutputContent, false, 0, 0, 250))
	skipped := result.GrepMetadata().Skipped()
	if skipped.Binary() != 1 || skipped.InvalidUTF8() != 1 || skipped.TooLarge() != 1 {
		t.Fatalf("skip counts = %#v", skipped)
	}
	if strings.Contains(result.Preview().Text(), "binary") || strings.Contains(result.Preview().Text(), "invalid") || strings.Contains(result.Preview().Text(), "large") {
		t.Fatal("跳过文件泄漏到 preview")
	}
}

func openTestGrepExecutor(t *testing.T, root string) *GrepExecutor {
	t.Helper()
	workspace, err := OpenWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = workspace.Close() })
	executor, err := NewGrepExecutor(workspace)
	if err != nil {
		t.Fatal(err)
	}
	return executor
}

func grepInvocationFor(t *testing.T, pattern string, searchPath string, glob string, mode tool.GrepOutputMode, insensitive bool, before int, after int, limit int) tool.GrepInvocation {
	t.Helper()
	input, err := tool.NewGrepInput(pattern, searchPath, glob, mode, insensitive, before, after, limit)
	if err != nil {
		t.Fatal(err)
	}
	callID, _ := tool.ParseProviderCallID("call-grep")
	call, _ := tool.NewGrepReadyCall(callID, input)
	invocationID, _ := tool.ParseInvocationID("018f1d8a-7b5c-7def-8123-456789abcdef")
	invocation, err := tool.NewGrepInvocation(invocationID, call)
	if err != nil {
		t.Fatal(err)
	}
	return invocation
}

func writeSearchFile(t *testing.T, root string, relative string, content string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
