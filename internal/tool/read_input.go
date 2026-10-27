package tool

import (
	"bytes"
	"easycode/internal/codec"
	"errors"
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
	if len(data) == 0 || len(data) > maxReadInputBytes {
		return ReadInput{}, errors.New("invalid ReadInput size")
	}
	wire := struct {
		FilePath string `json:"file_path"`
		Offset   int    `json:"offset"`
		Limit    int    `json:"limit"`
	}{Offset: 1, Limit: maxReadLines}
	if err := codec.DecodeObject(data, &wire, "file_path"); err != nil {
		return ReadInput{}, err
	}
	input := ReadInput{filePath: wire.FilePath, offset: wire.Offset, limit: wire.Limit}
	return input, input.Validate()
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
