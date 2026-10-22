package tool

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

// DecodeAnthropicEditInput 严格解码Anthropic Edit参数为共享Patch计划。
func DecodeAnthropicEditInput(data []byte) (PatchInput, error) {
	if len(data) == 0 || len(data) > MaxPatchInputBytes || !utf8.Valid(data) {
		return PatchInput{}, errors.New("invalid edit input")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('{') {
		return PatchInput{}, errors.New("edit input must be an object")
	}
	var filePath string
	var oldText string
	var newText string
	var replaceAll bool
	seen := make(map[string]struct{}, 4)
	for decoder.More() {
		token, tokenErr := decoder.Token()
		if tokenErr != nil {
			return PatchInput{}, errors.New("invalid edit input")
		}
		name, ok := token.(string)
		if !ok {
			return PatchInput{}, errors.New("invalid edit input field")
		}
		if _, duplicate := seen[name]; duplicate {
			return PatchInput{}, fmt.Errorf("duplicate edit input field %q", name)
		}
		seen[name] = struct{}{}
		switch name {
		case "file_path":
			if err := decoder.Decode(&filePath); err != nil {
				return PatchInput{}, errors.New("invalid edit file_path")
			}
		case "old_string":
			if err := decoder.Decode(&oldText); err != nil {
				return PatchInput{}, errors.New("invalid edit old_string")
			}
		case "new_string":
			if err := decoder.Decode(&newText); err != nil {
				return PatchInput{}, errors.New("invalid edit new_string")
			}
		case "replace_all":
			if err := decoder.Decode(&replaceAll); err != nil {
				return PatchInput{}, errors.New("invalid edit replace_all")
			}
		default:
			return PatchInput{}, fmt.Errorf("unknown edit input field %q", name)
		}
	}
	closing, err := decoder.Token()
	if err != nil || closing != json.Delim('}') {
		return PatchInput{}, errors.New("invalid edit input")
	}
	if err := requirePatchJSONEOF(decoder); err != nil {
		return PatchInput{}, err
	}
	for _, required := range []string{"file_path", "old_string", "new_string"} {
		if _, ok := seen[required]; !ok {
			return PatchInput{}, fmt.Errorf("edit %s is required", required)
		}
	}
	replacement, err := NewPatchReplacement(oldText, newText, replaceAll)
	if err != nil {
		return PatchInput{}, err
	}
	return NewPatchInput(filePath, []PatchReplacement{replacement})
}

// ParseOpenAIApplyPatchInput 严格解析OpenAI custom apply_patch单文件更新语法。
func ParseOpenAIApplyPatchInput(data []byte) (PatchInput, error) {
	if len(data) == 0 || len(data) > MaxPatchInputBytes || !utf8.Valid(data) || bytes.IndexByte(data, 0) >= 0 {
		return PatchInput{}, errors.New("invalid apply_patch input")
	}
	if bytes.IndexByte(data, '\r') >= 0 {
		return PatchInput{}, errors.New("apply_patch input must use LF line endings")
	}
	lines := strings.Split(string(data), "\n")
	if len(lines) < 4 || lines[0] != "*** Begin Patch" {
		return PatchInput{}, errors.New("apply_patch input must start with Begin Patch")
	}
	const updatePrefix = "*** Update File: "
	if !strings.HasPrefix(lines[1], updatePrefix) {
		return PatchInput{}, errors.New("apply_patch supports exactly one Update File")
	}
	filePath := strings.TrimPrefix(lines[1], updatePrefix)
	if filePath == "" || filePath != strings.TrimSpace(filePath) {
		return PatchInput{}, errors.New("apply_patch Update File path is invalid")
	}

	index := 2
	replacements := make([]PatchReplacement, 0, 4)
	foundEnd := false
	for index < len(lines) {
		line := lines[index]
		if line == "*** End Patch" {
			foundEnd = true
			index++
			break
		}
		if line != "@@" {
			return PatchInput{}, errors.New("apply_patch requires an exact @@ hunk header")
		}
		index++
		oldLines := make([]string, 0, 8)
		newLines := make([]string, 0, 8)
		changed := false
		for index < len(lines) {
			line = lines[index]
			if line == "@@" || line == "*** End Patch" {
				break
			}
			if strings.HasPrefix(line, "*** ") {
				return PatchInput{}, errors.New("apply_patch contains an unsupported file operation")
			}
			if line == "" {
				return PatchInput{}, errors.New("apply_patch hunk line is missing a prefix")
			}
			switch line[0] {
			case ' ':
				oldLines = append(oldLines, line[1:])
				newLines = append(newLines, line[1:])
			case '-':
				oldLines = append(oldLines, line[1:])
				changed = true
			case '+':
				newLines = append(newLines, line[1:])
				changed = true
			default:
				return PatchInput{}, errors.New("apply_patch hunk line has an invalid prefix")
			}
			index++
		}
		if !changed || len(oldLines) == 0 {
			return PatchInput{}, errors.New("apply_patch hunk must replace existing content")
		}
		replacement, err := NewPatchLineReplacement(oldLines, newLines)
		if err != nil {
			return PatchInput{}, err
		}
		replacements = append(replacements, replacement)
	}
	if !foundEnd || len(replacements) == 0 {
		return PatchInput{}, errors.New("apply_patch input is incomplete")
	}
	for ; index < len(lines); index++ {
		if lines[index] != "" {
			return PatchInput{}, errors.New("apply_patch input has trailing content")
		}
	}
	return NewPatchInput(filePath, replacements)
}

func requirePatchJSONEOF(decoder *json.Decoder) error {
	var trailing json.RawMessage
	err := decoder.Decode(&trailing)
	if errors.Is(err, io.EOF) {
		return nil
	}
	return errors.New("edit input has trailing JSON")
}
