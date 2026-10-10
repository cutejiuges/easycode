package builtin

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"unicode/utf8"

	"easycode/internal/tool"
)

const (
	maxGrepFiles = 50_000
	maxGrepBytes = 256 << 20
)

// GrepExecutor 只依赖已打开 Workspace capability 的 Grep 执行器。
type GrepExecutor struct{ workspace *Workspace }

// NewGrepExecutor 纯内存组合一个 Grep executor，不访问文件系统。
func NewGrepExecutor(workspace *Workspace) (*GrepExecutor, error) {
	if workspace == nil || workspace.handle == nil {
		return nil, errors.New("workspace is required")
	}
	return &GrepExecutor{workspace: workspace}, nil
}

// Execute 按规范路径与行号顺序扫描 UTF-8 普通文件。
func (executor *GrepExecutor) Execute(ctx context.Context, invocation tool.GrepInvocation) tool.InvocationResult {
	if ctx == nil || ctx.Err() != nil {
		return grepError(invocation, tool.ResultCancelled, "cancelled", "Grep cancelled")
	}
	if executor == nil || executor.workspace == nil || invocation.Validate() != nil {
		return tool.InvocationResult{}
	}
	input := invocation.Input()
	pattern := input.Pattern()
	if input.CaseInsensitive() {
		pattern = "(?i:" + pattern + ")"
	}
	expression, err := regexp.Compile(pattern)
	if err != nil {
		return grepError(invocation, tool.ResultError, "invalid_pattern", "Grep failed: invalid pattern")
	}
	var filter globMatcher
	if input.Glob() != "" {
		filter, err = compileGlobMatcher(input.Glob())
		if err != nil {
			return grepError(invocation, tool.ResultError, "invalid_glob", "Grep failed: invalid glob")
		}
	}
	walked, err := walkWorkspace(ctx, executor.workspace, input.Path())
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return grepError(invocation, tool.ResultCancelled, "cancelled", "Grep cancelled")
		}
		return grepError(invocation, tool.ResultError, "search_unavailable", "Grep failed: workspace is unavailable")
	}
	skipped := walked.skipped
	matches := make([]tool.GrepMatch, 0)
	matchingLines := 0
	scannedFiles := 0
	var scannedBytes int64
	incompleteReason := walked.incompleteReason
	for _, candidate := range walked.files {
		if err := ctx.Err(); err != nil {
			return grepError(invocation, tool.ResultCancelled, "cancelled", "Grep cancelled")
		}
		if input.Glob() != "" && !filter.Match(candidate.relativePath) {
			continue
		}
		if scannedFiles >= maxGrepFiles {
			incompleteReason = tool.SearchFileLimitReached
			break
		}
		if candidate.size < 0 || candidate.size > maxReadFileBytes {
			skipped.tooLarge++
			continue
		}
		if scannedBytes+candidate.size > maxGrepBytes {
			incompleteReason = tool.SearchByteLimitReached
			break
		}
		file, openErr := executor.workspace.handle.openFile(candidate.relativePath, executor.workspace.hooks)
		if openErr != nil {
			classifyGrepOpenError(openErr, &skipped)
			continue
		}
		data, code, _ := readBounded(ctx, file)
		closeErr := file.Close()
		if closeErr != nil {
			skipped.unreadable++
			continue
		}
		if code == "cancelled" {
			return grepError(invocation, tool.ResultCancelled, "cancelled", "Grep cancelled")
		}
		if code == "file_too_large" {
			skipped.tooLarge++
			continue
		}
		if code != "" {
			skipped.unreadable++
			continue
		}
		scannedFiles++
		scannedBytes += int64(len(data))
		if containsNUL(data) {
			skipped.binary++
			continue
		}
		if !utf8.Valid(data) {
			skipped.invalidUTF8++
			continue
		}
		lines := splitTextLines(string(data))
		matchedIndexes := matchingLineIndexes(lines, expression)
		matchingLines += len(matchedIndexes)
		fileMatches, buildErr := grepMatchesForFile(candidate.relativePath, lines, matchedIndexes, input)
		if buildErr != nil {
			return grepError(invocation, tool.ResultError, "internal_error", "Grep failed: internal error")
		}
		matches = append(matches, fileMatches...)
	}
	omitted := 0
	if len(matches) > input.Limit() {
		omitted = len(matches) - input.Limit()
		matches = matches[:input.Limit()]
	}
	previewLines := make([]string, len(matches))
	for index, match := range matches {
		previewLines[index] = renderGrepMatch(input.OutputMode(), match)
	}
	preview, previewOmitted := renderSearchPreview(previewLines)
	omitted += previewOmitted
	truncated := omitted > 0 || incompleteReason != tool.SearchComplete
	metadata, err := tool.NewGrepResultMetadata(input.OutputMode(), matches, matchingLines, truncated, omitted, walked.visitedEntries, scannedFiles, scannedBytes, incompleteReason, skipped.freeze())
	if err != nil {
		return grepError(invocation, tool.ResultError, "internal_error", "Grep failed: internal error")
	}
	result, err := tool.NewGrepInvocationResult(invocation, tool.ResultSuccess, "ok", preview, metadata)
	if err != nil {
		return grepError(invocation, tool.ResultError, "internal_error", "Grep failed: internal error")
	}
	return result
}

func matchingLineIndexes(lines []string, expression *regexp.Regexp) []int {
	indexes := make([]int, 0)
	for index, line := range lines {
		if expression.MatchString(line) {
			indexes = append(indexes, index)
		}
	}
	return indexes
}

func grepMatchesForFile(relativePath string, lines []string, matchedIndexes []int, input tool.GrepInput) ([]tool.GrepMatch, error) {
	if len(matchedIndexes) == 0 {
		return nil, nil
	}
	switch input.OutputMode() {
	case tool.GrepOutputFilesWithMatches:
		match, err := tool.NewGrepFileMatch(relativePath)
		return []tool.GrepMatch{match}, err
	case tool.GrepOutputCount:
		match, err := tool.NewGrepCountMatch(relativePath, len(matchedIndexes))
		return []tool.GrepMatch{match}, err
	case tool.GrepOutputContent:
		selected := make([]bool, len(lines))
		matched := make([]bool, len(lines))
		for _, index := range matchedIndexes {
			matched[index] = true
			start := index - input.BeforeContext()
			if start < 0 {
				start = 0
			}
			end := index + input.AfterContext()
			if end >= len(lines) {
				end = len(lines) - 1
			}
			for current := start; current <= end; current++ {
				selected[current] = true
			}
		}
		result := make([]tool.GrepMatch, 0)
		for index, include := range selected {
			if !include {
				continue
			}
			match, err := tool.NewGrepContentMatch(relativePath, index+1, lines[index], matched[index])
			if err != nil {
				return nil, err
			}
			result = append(result, match)
		}
		return result, nil
	default:
		return nil, errors.New("unsupported Grep output mode")
	}
}

func renderGrepMatch(mode tool.GrepOutputMode, match tool.GrepMatch) string {
	switch mode {
	case tool.GrepOutputFilesWithMatches:
		return match.RelativePath()
	case tool.GrepOutputCount:
		return match.RelativePath() + ":" + strconv.Itoa(match.Count())
	default:
		separator := "-"
		if match.MatchingLine() {
			separator = ":"
		}
		return fmt.Sprintf("%s%s%d%s%s", match.RelativePath(), separator, match.Line(), separator, match.Text())
	}
}

func classifyGrepOpenError(err error, counts *mutableSkipCounts) {
	switch {
	case errors.Is(err, errTargetTooLarge):
		counts.tooLarge++
	case errors.Is(err, errTargetNotReadable):
		counts.unreadable++
	case errors.Is(err, errTargetNotRegular):
		counts.unsupported++
	default:
		counts.disappeared++
	}
}

func grepError(invocation tool.GrepInvocation, status tool.ResultStatus, code string, message string) tool.InvocationResult {
	if invocation.Validate() != nil {
		return tool.InvocationResult{}
	}
	preview, _ := tool.NewModelPreview(message)
	metadata, _ := tool.NewGrepResultMetadata(invocation.Input().OutputMode(), []tool.GrepMatch{}, 0, false, 0, 0, 0, 0, tool.SearchComplete, tool.SearchSkipCounts{})
	result, err := tool.NewGrepInvocationResult(invocation, status, code, preview, metadata)
	if err == nil {
		return result
	}
	return tool.InvocationResult{}
}
