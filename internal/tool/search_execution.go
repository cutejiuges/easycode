package tool

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

const MaxSearchLineCodePoints = 500

// SearchExecution 只持有选中结果的有界展示候选；原文由外部 capture 增量保存。
type SearchExecution struct {
	result  InvocationResult
	lines   []string
	lineCut bool
}

// BoundGrepMatch 保留类型与定位信息，只复制行预览允许的文本。
func BoundGrepMatch(match GrepMatch) GrepMatch {
	text, cut := truncateRunes(match.text, MaxSearchLineCodePoints)
	if cut {
		text, _ = truncateRunes(match.text, MaxSearchLineCodePoints-21)
		text += "... [line truncated]"
	}
	match.text = strings.Clone(text)
	return match
}

// RenderGrepMatch 是原始搜索保存与展示共用的确定性行格式。
func RenderGrepMatch(mode GrepOutputMode, match GrepMatch) string {
	switch mode {
	case GrepOutputFilesWithMatches:
		return match.relativePath
	case GrepOutputCount:
		return fmt.Sprintf("%s:%d", match.relativePath, match.count)
	default:
		separator := "-"
		if match.matchingLine {
			separator = ":"
		}
		return fmt.Sprintf("%s%s%d%s%s", match.relativePath, separator, match.line, separator, match.text)
	}
}

// NewSearchExecution 验证候选上限，不接受完整文件正文或未实现的能力。
func NewSearchExecution(result InvocationResult, lineCut bool) (SearchExecution, error) {
	if result.Validate() != nil || (result.capability != CapabilityGlob && result.capability != CapabilityGrep) {
		return SearchExecution{}, errors.New("invalid search execution")
	}
	e := SearchExecution{result: result, lineCut: lineCut}
	if result.status != ResultSuccess {
		return e, nil
	}
	if result.capability == CapabilityGlob {
		if len(result.globMetadata.matches) > 1000 {
			return SearchExecution{}, errors.New("too many search candidates")
		}
		e.lines = result.globMetadata.Matches()
	} else {
		if len(result.grepMetadata.matches) > 1000 {
			return SearchExecution{}, errors.New("too many search candidates")
		}
		for _, match := range result.grepMetadata.matches {
			if utf8.RuneCountInString(match.text) > MaxSearchLineCodePoints {
				return SearchExecution{}, errors.New("unbounded search candidate")
			}
			e.lines = append(e.lines, RenderGrepMatch(result.grepMetadata.mode, match))
		}
	}
	for i, line := range e.lines {
		bounded, cut := truncateRunes(line, MaxSearchLineCodePoints)
		e.lineCut = e.lineCut || cut
		if cut {
			bounded, _ = truncateRunes(line, MaxSearchLineCodePoints-utf8.RuneCountInString("... [line truncated]"))
			bounded += "... [line truncated]"
		}
		e.lines[i] = strings.Clone(bounded) + "\n"
	}
	return e, nil
}

func searchSelection(lines []string, limit int) (string, []int) {
	prefix := make([]int, len(lines)+1)
	for i, line := range lines {
		prefix[i+1] = prefix[i] + len(line)
	}
	keep := len(lines)
	if prefix[keep] > limit {
		keep = 0
		for n := 0; n < len(lines); n++ {
			head, tail := (n+1)/2, n/2
			marker := fmt.Sprintf("[... %d results omitted ...]\n", len(lines)-n)
			if prefix[head]+prefix[len(lines)]-prefix[len(lines)-tail]+len(marker) <= limit {
				keep = n
			}
		}
	}
	indices := make([]int, 0, keep)
	var body strings.Builder
	head, tail := (keep+1)/2, keep/2
	if keep == len(lines) {
		head, tail = keep, 0
	}
	for i := 0; i < head; i++ {
		body.WriteString(lines[i])
		indices = append(indices, i)
	}
	if keep < len(lines) {
		fmt.Fprintf(&body, "[... %d results omitted ...]\n", len(lines)-keep)
	}
	for i := len(lines) - tail; i < len(lines); i++ {
		body.WriteString(lines[i])
		indices = append(indices, i)
	}
	return body.String(), indices
}

func searchSavingPreview(saving ArtifactSaving) string {
	if saving.Status() == ArtifactNotNeeded {
		return ""
	}
	if d, ok := saving.Descriptor(); ok {
		return fmt.Sprintf("saved_output: %s; artifact_id: %s; saved_bytes: %d; source_complete: %t; use ReadArtifact to read more\n", saving.Status(), d.ID(), d.SavedBytes(), d.Complete())
	}
	return fmt.Sprintf("saved_output: unavailable; reason: %s\n", saving.Reason())
}

// NeedsSaving 在发布前决定是否需要原文对象；覆盖上限与保存完整性独立。
func (e SearchExecution) NeedsSaving(allowance int) bool {
	if e.result.status != ResultSuccess {
		return false
	}
	_, indices := searchSelection(e.lines, allowance)
	return e.lineCut || len(indices) < len(e.lines)
}

// Finalize 在保存状态确定后一次冻结预览与模型可见 metadata。
func (e SearchExecution) Finalize(allowance int, saving ArtifactSaving) (InvocationResult, error) {
	if allowance < SafetyResultBytes || allowance > MaxSearchPreviewBytes || saving.Validate() != nil {
		return InvocationResult{}, errors.New("invalid search finalization")
	}
	if e.result.status != ResultSuccess {
		return FinalizeResult(e.result, allowance, BudgetPreserved)
	}
	header := searchSavingPreview(saving)
	body, indices := searchSelection(e.lines, allowance-len(header))
	r := e.result
	r.preview = ModelPreview{text: header + body}
	r.searchSaving = saving
	if r.capability == CapabilityGlob {
		original := r.globMetadata.matches
		r.globMetadata.matches = nil
		for _, i := range indices {
			if strings.HasPrefix(e.lines[i], original[i]+"\n") {
				r.globMetadata.matches = append(r.globMetadata.matches, original[i])
			}
		}
		r.globMetadata.modelOmittedMatches = len(original) - len(r.globMetadata.matches)
		r.globMetadata.omittedMatches += r.globMetadata.modelOmittedMatches
		r.globMetadata.truncated = r.globMetadata.omittedMatches > 0 || r.globMetadata.incompleteReason != SearchComplete
	} else {
		original := r.grepMetadata.matches
		r.grepMetadata.matches = nil
		for _, i := range indices {
			match := original[i]
			prefix := RenderGrepMatch(r.grepMetadata.mode, GrepMatch{relativePath: match.relativePath, line: match.line, matchingLine: match.matchingLine, count: match.count})
			if r.grepMetadata.mode == GrepOutputContent {
				if !strings.HasPrefix(e.lines[i], prefix) {
					continue
				}
				shown := strings.TrimSuffix(strings.TrimPrefix(e.lines[i], prefix), "\n")

				match.text = shown
			} else if e.lines[i] != prefix+"\n" {
				continue
			}
			r.grepMetadata.matches = append(r.grepMetadata.matches, match)
		}
		r.grepMetadata.modelOmittedMatches = len(original) - len(r.grepMetadata.matches)
		r.grepMetadata.omittedMatches += r.grepMetadata.modelOmittedMatches
		r.grepMetadata.truncated = r.grepMetadata.omittedMatches > 0 || r.grepMetadata.incompleteReason != SearchComplete
	}
	reason := BudgetPreserved
	if e.lineCut || len(indices) < len(e.lines) {
		reason = BudgetShortened
	}
	metadata, err := NewResultBudgetMetadata(allowance, len(r.preview.text), reason)
	if err != nil {
		return InvocationResult{}, err
	}
	return r.WithBudget(metadata)
}
