package tool

import (
	"errors"
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
	if len(data) == 0 || len(data) > MaxWriteInputBytes {
		return WriteInput{}, errors.New("invalid WriteInput size")
	}
	wire := struct {
		FilePath string `json:"file_path"`
		Content  string `json:"content"`
	}{}
	if err := codec.DecodeObject(data, &wire, "file_path", "content"); err != nil {
		return WriteInput{}, err
	}
	input := WriteInput{filePath: wire.FilePath, content: wire.Content}
	return input, input.Validate()
}
