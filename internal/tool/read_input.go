package tool

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"unicode/utf8"
)

const (
	maxReadInputBytes = 64 << 10
	maxReadLines      = 2000
)

// ReadInput 是 Read 的不可变、已默认化输入。
type ReadInput struct {
	filePath string
	offset   int
	limit    int
}

// NewReadInput 构造并校验 Read 输入。
func NewReadInput(filePath string, offset int, limit int) (ReadInput, error) {
	input := ReadInput{filePath: filePath, offset: offset, limit: limit}
	if err := input.Validate(); err != nil {
		return ReadInput{}, err
	}
	return input, nil
}

// DecodeReadInput 严格解码 Read JSON 对象并应用默认值。
func DecodeReadInput(data []byte) (ReadInput, error) {
	if len(data) == 0 || len(data) > maxReadInputBytes || !utf8.Valid(data) {
		return ReadInput{}, errors.New("invalid Read input")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('{') {
		return ReadInput{}, errors.New("read input must be an object")
	}
	input := ReadInput{offset: 1, limit: maxReadLines}
	seen := make(map[string]struct{}, 3)
	for decoder.More() {
		token, tokenErr := decoder.Token()
		if tokenErr != nil {
			return ReadInput{}, errors.New("invalid Read input")
		}
		name, ok := token.(string)
		if !ok {
			return ReadInput{}, errors.New("invalid Read input field")
		}
		if _, duplicate := seen[name]; duplicate {
			return ReadInput{}, fmt.Errorf("duplicate Read input field %q", name)
		}
		seen[name] = struct{}{}
		switch name {
		case "file_path":
			if err := decoder.Decode(&input.filePath); err != nil {
				return ReadInput{}, errors.New("invalid Read file_path")
			}
		case "offset":
			if err := decoder.Decode(&input.offset); err != nil {
				return ReadInput{}, errors.New("invalid Read offset")
			}
		case "limit":
			if err := decoder.Decode(&input.limit); err != nil {
				return ReadInput{}, errors.New("invalid Read limit")
			}
		default:
			return ReadInput{}, fmt.Errorf("unknown Read input field %q", name)
		}
	}
	closing, err := decoder.Token()
	if err != nil || closing != json.Delim('}') {
		return ReadInput{}, errors.New("invalid Read input")
	}
	if err := requireJSONEOF(decoder); err != nil {
		return ReadInput{}, err
	}
	if _, ok := seen["file_path"]; !ok {
		return ReadInput{}, errors.New("read file_path is required")
	}
	if err := input.Validate(); err != nil {
		return ReadInput{}, err
	}
	return input, nil
}

// FilePath 返回调用方提供的路径。
func (input ReadInput) FilePath() string { return input.filePath }

// Offset 返回默认化后的 1-based 起始行。
func (input ReadInput) Offset() int { return input.offset }

// Limit 返回默认化后的最大行数。
func (input ReadInput) Limit() int { return input.limit }

// Validate 验证 Read 输入边界。
func (input ReadInput) Validate() error {
	if input.filePath == "" || !utf8.ValidString(input.filePath) || bytes.IndexByte([]byte(input.filePath), 0) >= 0 {
		return errors.New("read file_path must be non-empty UTF-8")
	}
	if input.offset < 1 {
		return errors.New("read offset must be at least 1")
	}
	if input.limit < 1 || input.limit > maxReadLines {
		return errors.New("read limit must be between 1 and 2000")
	}
	return nil
}

func requireJSONEOF(decoder *json.Decoder) error {
	var trailing json.RawMessage
	err := decoder.Decode(&trailing)
	if errors.Is(err, io.EOF) {
		return nil
	}
	return errors.New("read input has trailing JSON")
}
