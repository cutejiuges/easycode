package tool

import (
	"bytes"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"easycode/internal/codec"
)

const (
	// MaxWriteInputBytes 限制原始与规范化写入参数的字节数。
	MaxWriteInputBytes = 1 << 20
	// MaxWriteDirectories 限制一次原子发布创建的目录层数。
	MaxWriteDirectories = 16
)

// WriteInput 保存模型要求原样写入的文本，不进行换行或编码转换。
type WriteInput struct {
	filePath string
	content  string
}

// NewWriteInput 构造唯一当前写入输入。
func NewWriteInput(filePath, content string) (WriteInput, error) {
	input := WriteInput{filePath: filePath, content: content}
	return input, input.Validate()
}

func (input WriteInput) FilePath() string { return input.filePath }
func (input WriteInput) Content() string  { return input.content }

// Validate 验证路径、精确文本和规范化输入预算。
func (input WriteInput) Validate() error {
	if validateWritePath(input.filePath) != nil || !utf8.ValidString(input.content) || strings.ContainsRune(input.content, 0) {
		return errors.New("invalid Write input")
	}
	if len(input.filePath)+len(input.content) > MaxWriteInputBytes {
		return errors.New("Write input exceeds 1 MiB")
	}
	canonical, err := codec.MarshalStable(struct {
		FilePath string `json:"file_path"`
		Content  string `json:"content"`
	}{input.filePath, input.content})
	if err != nil || len(canonical) > MaxWriteInputBytes {
		return errors.New("normalized Write input exceeds 1 MiB")
	}
	return nil
}

func validateWritePath(path string) error {
	if path == "" || len(path) > 4096 || !utf8.ValidString(path) || strings.Contains(path, "\\") {
		return errors.New("invalid Write path")
	}
	parts := strings.Split(path, "/")
	if len(parts) > 64 {
		return errors.New("Write path exceeds 64 components")
	}
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return errors.New("invalid Write path component")
		}
		for _, r := range part {
			if unicode.IsControl(r) {
				return errors.New("invalid Write path character")
			}
		}
	}
	return nil
}

// DecodeWriteInput 拒绝重复、缺失、未知、null 和有损 Unicode 参数。
func DecodeWriteInput(data []byte) (WriteInput, error) {
	if len(data) == 0 || len(data) > MaxWriteInputBytes || !utf8.Valid(data) || !losslessJSONUnicode(data) {
		return WriteInput{}, errors.New("invalid Write input encoding or size")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('{') {
		return WriteInput{}, errors.New("Write input must be an object")
	}
	var input WriteInput
	seen := make(map[string]bool, 2)
	for decoder.More() {
		token, err := decoder.Token()
		name, ok := token.(string)
		if err != nil || !ok || seen[name] {
			return WriteInput{}, errors.New("invalid or duplicate Write field")
		}
		seen[name] = true
		valueToken, err := decoder.Token()
		value, ok := valueToken.(string)
		if err != nil || !ok {
			return WriteInput{}, errors.New("Write fields must be strings")
		}
		switch name {
		case "file_path":
			input.filePath = value
		case "content":
			input.content = value
		default:
			return WriteInput{}, errors.New("unknown Write field")
		}
	}
	closing, err := decoder.Token()
	if err != nil || closing != json.Delim('}') || requireJSONEOF(decoder) != nil || !seen["file_path"] || !seen["content"] {
		return WriteInput{}, errors.New("incomplete Write input")
	}
	return input, input.Validate()
}

// losslessJSONUnicode 在标准 JSON 解码前拒绝会被替换成 U+FFFD 的孤立代理项。
func losslessJSONUnicode(data []byte) bool {
	for i := 0; i < len(data); i++ {
		if data[i] != '\\' {
			continue
		}
		i++
		if i >= len(data) {
			return false
		}
		if data[i] != 'u' {
			continue
		}
		if i+4 >= len(data) {
			return false
		}
		n, err := strconv.ParseUint(string(data[i+1:i+5]), 16, 16)
		if err != nil {
			return false
		}
		i += 4
		if n >= 0xdc00 && n <= 0xdfff {
			return false
		}
		if n < 0xd800 || n > 0xdbff {
			continue
		}
		if i+6 >= len(data) || data[i+1] != '\\' || data[i+2] != 'u' {
			return false
		}
		low, err := strconv.ParseUint(string(data[i+3:i+7]), 16, 16)
		if err != nil || low < 0xdc00 || low > 0xdfff {
			return false
		}
		i += 6
	}
	return true
}
