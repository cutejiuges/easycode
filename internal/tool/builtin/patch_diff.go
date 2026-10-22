package builtin

import (
	"fmt"
	"sort"
	"strings"

	"easycode/internal/tool"
)

type diffLine struct {
	kind byte
	text string
}

type diffAnchor struct{ old, new int }

// buildPatchDiff 使用确定性唯一行锚点与固定三行context生成完整unified diff。
func buildPatchDiff(path string, base, final []byte, replacements int) (string, tool.PatchResultMetadata, error) {
	oldLines, newLines := patchLines(string(base)), patchLines(string(final))
	script := patchDiffScript(oldLines, newLines)
	added, removed := 0, 0
	for _, line := range script {
		if line.kind == '+' {
			added++
		}
		if line.kind == '-' {
			removed++
		}
	}
	metadata, err := tool.NewPatchResultMetadata(path, replacements, added, removed)
	if err != nil {
		return "", tool.PatchResultMetadata{}, err
	}
	var output strings.Builder
	output.WriteString("--- a/" + path + "\n+++ b/" + path + "\n")
	oldAt, newAt := 1, 1
	cursor := 0
	for cursor < len(script) {
		change := cursor
		for change < len(script) && script[change].kind == ' ' {
			change++
		}
		if change == len(script) {
			break
		}
		start := max(cursor, change-3)
		for cursor < start {
			oldAt++
			newAt++
			cursor++
		}
		end := change + 1
		lastChange := change
		for end < len(script) {
			if script[end].kind != ' ' {
				lastChange = end
			}
			if end-lastChange > 6 {
				break
			}
			end++
		}
		end = min(len(script), lastChange+4)
		oldCount, newCount := 0, 0
		for _, line := range script[start:end] {
			if line.kind != '+' {
				oldCount++
			}
			if line.kind != '-' {
				newCount++
			}
		}
		oldStart, newStart := oldAt, newAt
		if oldCount == 0 {
			oldStart--
		}
		if newCount == 0 {
			newStart--
		}
		fmt.Fprintf(&output, "@@ -%d,%d +%d,%d @@\n", oldStart, oldCount, newStart, newCount)
		for _, line := range script[start:end] {
			output.WriteByte(line.kind)
			output.WriteString(strings.TrimSuffix(line.text, "\n"))
			output.WriteByte('\n')
			if !strings.HasSuffix(line.text, "\n") {
				output.WriteString("\\ No newline at end of file\n")
			}
			if output.Len() > tool.MaxApprovalDiffBytes {
				return "", tool.PatchResultMetadata{}, patchError("patch_preview_too_large")
			}
		}
		oldAt += oldCount
		newAt += newCount
		cursor = end
	}
	if output.Len() > tool.MaxApprovalDiffBytes {
		return "", tool.PatchResultMetadata{}, patchError("patch_preview_too_large")
	}
	return output.String(), metadata, nil
}

func patchLines(text string) []string {
	if text == "" {
		return nil
	}
	lines := strings.SplitAfter(text, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func patchDiffScript(oldLines, newLines []string) []diffLine {
	anchors := patchDiffAnchors(oldLines, newLines)
	result := make([]diffLine, 0, len(oldLines)+len(newLines))
	oldAt, newAt := 0, 0
	for _, anchor := range append(anchors, diffAnchor{len(oldLines), len(newLines)}) {
		result = appendDiffGap(result, oldLines[oldAt:anchor.old], newLines[newAt:anchor.new])
		if anchor.old < len(oldLines) {
			result = append(result, diffLine{' ', oldLines[anchor.old]})
		}
		oldAt, newAt = anchor.old+1, anchor.new+1
	}
	return result
}

func appendDiffGap(result []diffLine, oldLines, newLines []string) []diffLine {
	prefix := 0
	for prefix < len(oldLines) && prefix < len(newLines) && oldLines[prefix] == newLines[prefix] {
		result = append(result, diffLine{' ', oldLines[prefix]})
		prefix++
	}
	oldEnd, newEnd := len(oldLines), len(newLines)
	for oldEnd > prefix && newEnd > prefix && oldLines[oldEnd-1] == newLines[newEnd-1] {
		oldEnd--
		newEnd--
	}
	for _, line := range oldLines[prefix:oldEnd] {
		result = append(result, diffLine{'-', line})
	}
	for _, line := range newLines[prefix:newEnd] {
		result = append(result, diffLine{'+', line})
	}
	for _, line := range oldLines[oldEnd:] {
		result = append(result, diffLine{' ', line})
	}
	return result
}

func patchDiffAnchors(oldLines, newLines []string) []diffAnchor {
	type occurrence struct{ index, count int }
	oldSeen := make(map[string]occurrence, len(oldLines))
	newSeen := make(map[string]occurrence, len(newLines))
	for i, line := range oldLines {
		value := oldSeen[line]
		value.index = i
		value.count++
		oldSeen[line] = value
	}
	for i, line := range newLines {
		value := newSeen[line]
		value.index = i
		value.count++
		newSeen[line] = value
	}
	var matches []diffAnchor
	for i, line := range oldLines {
		other := newSeen[line]
		if oldSeen[line].count == 1 && other.count == 1 {
			matches = append(matches, diffAnchor{i, other.index})
		}
	}
	// 遍历顺序只由原文件行号决定，不依赖map的随机迭代。
	tails := make([]int, 0, len(matches))
	previous := make([]int, len(matches))
	for i, match := range matches {
		position := sort.Search(len(tails), func(j int) bool { return matches[tails[j]].new >= match.new })
		previous[i] = -1
		if position > 0 {
			previous[i] = tails[position-1]
		}
		if position == len(tails) {
			tails = append(tails, i)
		} else {
			tails[position] = i
		}
	}
	if len(tails) == 0 {
		return nil
	}
	anchors := make([]diffAnchor, len(tails))
	index := tails[len(tails)-1]
	for position := len(anchors) - 1; position >= 0; position-- {
		anchors[position] = matches[index]
		index = previous[index]
	}
	return anchors
}
