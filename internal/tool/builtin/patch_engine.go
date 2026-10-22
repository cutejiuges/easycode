package builtin

import (
	"bytes"
	"errors"
	"sort"
	"strings"
	"unicode/utf8"

	"easycode/internal/tool"
)

// PatchPreparationError 只携带稳定错误码，不暴露文件正文。
type PatchPreparationError struct{ code string }

func (err *PatchPreparationError) Error() string { return "Patch preparation failed: " + err.code }
func (err *PatchPreparationError) Code() string  { return err.code }

func patchError(code string) error { return &PatchPreparationError{code: code} }

type patchEdit struct {
	start, end int
	text       string
}

// applyPatchPlan 对同一base解析全部精确匹配，拒绝重叠后一次性产生final bytes。
func applyPatchPlan(base []byte, input tool.PatchInput) ([]byte, error) {
	if input.Validate() != nil {
		return nil, patchError("invalid_patch_input")
	}
	if len(base) > maxReadFileBytes {
		return nil, patchError("file_too_large")
	}
	if !utf8.Valid(base) || bytes.IndexByte(base, 0) >= 0 {
		return nil, patchError("invalid_text_file")
	}
	text := string(base)
	edits := make([]patchEdit, 0, len(input.Replacements()))
	for _, replacement := range input.Replacements() {
		matches := exactPatchMatches(text, replacement)
		if len(matches) == 0 {
			return nil, patchError("patch_match_not_found")
		}
		if !replacement.ReplaceAll() && len(matches) != 1 {
			return nil, patchError("patch_match_ambiguous")
		}
		edits = append(edits, matches...)
	}
	sort.SliceStable(edits, func(i, j int) bool { return edits[i].start < edits[j].start })
	finalSize := len(base)
	for index, edit := range edits {
		if index > 0 && edit.start < edits[index-1].end {
			return nil, patchError("patch_overlapping_replacements")
		}
		finalSize += len(edit.text) - (edit.end - edit.start)
		if finalSize > maxReadFileBytes {
			return nil, patchError("file_too_large")
		}
	}
	var final bytes.Buffer
	final.Grow(finalSize)
	cursor := 0
	for _, edit := range edits {
		final.Write(base[cursor:edit.start])
		final.WriteString(edit.text)
		cursor = edit.end
	}
	final.Write(base[cursor:])
	if bytes.Equal(base, final.Bytes()) {
		return nil, patchError("patch_no_changes")
	}
	return final.Bytes(), nil
}

func exactPatchMatches(base string, replacement tool.PatchReplacement) []patchEdit {
	oldText, newText := replacement.OldText(), replacement.NewText()
	var matches []patchEdit
	// 每个UTF-8匹配位置都参与重叠检查，不能使用跳过匹配长度的Count。
	for offset := 0; offset < len(base); {
		index := strings.Index(base[offset:], oldText)
		if index < 0 {
			break
		}
		start := offset + index
		if !replacement.LineBased() || start == 0 || base[start-1] == '\n' {
			matches = append(matches, patchEdit{start: start, end: start + len(oldText), text: newText})
		}
		offset = start + 1
	}
	if replacement.LineBased() {
		withoutTerminator := strings.TrimSuffix(oldText, "\n")
		if withoutTerminator != "" && strings.HasSuffix(base, withoutTerminator) && !strings.HasSuffix(base, "\n") {
			start := len(base) - len(withoutTerminator)
			if start == 0 || base[start-1] == '\n' {
				matches = append(matches, patchEdit{start: start, end: len(base), text: strings.TrimSuffix(newText, "\n")})
			}
		}
	}
	return matches
}

// patchPreparationCode 将已知准备错误归一化为稳定code。
func patchPreparationCode(err error) string {
	var preparation *PatchPreparationError
	if errors.As(err, &preparation) {
		return preparation.Code()
	}
	return "patch_prepare_failed"
}
