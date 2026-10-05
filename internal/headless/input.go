// Package headless 实现一次性与长期非交互宿主及其稳定输出协议。
package headless

import (
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

const MaxPromptBytes = 4 << 20

const (
	stdinPrefix = "\n\n<stdin>\n"
	stdinSuffix = "</stdin>"
)

// Mode 表示应用宿主的输出模式。
type Mode uint8

const (
	ModeInteractive Mode = iota
	ModeText
	ModeJSON
	ModeStreamJSON
)

// InputErrorKind 区分用法错误和底层 stdin 读取失败。
type InputErrorKind uint8

const (
	InputErrorUsage InputErrorKind = iota + 1
	InputErrorRead
)

// InputError 是不包含 prompt 内容的稳定输入错误。
type InputError struct {
	Kind    InputErrorKind
	Message string
}

func (err *InputError) Error() string {
	if err == nil {
		return ""
	}
	return err.Message
}

// ResolvePrompt 在任何应用装配副作用前解析完整 headless prompt。
func ResolvePrompt(arguments []string, input io.Reader, stdinIsTerminal bool) (string, error) {
	if len(arguments) > 1 {
		return "", usageError("headless mode accepts at most one prompt argument")
	}
	if len(arguments) == 0 {
		if stdinIsTerminal {
			return "", usageError("prompt is required when stdin is a terminal")
		}
		content, err := readPrompt(input, MaxPromptBytes)
		if err != nil {
			return "", err
		}
		return validatePrompt(content)
	}

	argument := arguments[0]
	if argument == "-" {
		content, err := readPrompt(input, MaxPromptBytes)
		if err != nil {
			return "", err
		}
		return validatePrompt(content)
	}
	if !utf8.ValidString(argument) {
		return "", usageError("prompt must be valid UTF-8")
	}
	if len(argument) > MaxPromptBytes {
		return "", usageError("prompt exceeds 4 MiB")
	}
	if stdinIsTerminal {
		return validatePrompt([]byte(argument))
	}

	maxStdin := MaxPromptBytes - len(argument) - len(stdinPrefix) - len(stdinSuffix)
	readLimit := maxStdin
	if readLimit < 0 {
		readLimit = 0
	}
	content, err := readPrompt(input, readLimit)
	if err != nil {
		return "", err
	}
	if len(content) == 0 {
		return validatePrompt([]byte(argument))
	}

	result := make([]byte, 0, len(argument)+len(stdinPrefix)+len(content)+1+len(stdinSuffix))
	result = append(result, argument...)
	result = append(result, stdinPrefix...)
	result = append(result, content...)
	if content[len(content)-1] != '\n' {
		result = append(result, '\n')
	}
	result = append(result, stdinSuffix...)
	return validatePrompt(result)
}

// ValidateResolvedPrompt 防止装配层被绕过时接受非法已解析输入。
func ValidateResolvedPrompt(prompt string) error {
	_, err := validatePrompt([]byte(prompt))
	return err
}

func readPrompt(input io.Reader, maxBytes int) ([]byte, error) {
	if input == nil {
		return nil, &InputError{Kind: InputErrorRead, Message: "prompt input is unavailable"}
	}
	content, err := io.ReadAll(io.LimitReader(input, int64(maxBytes)+1))
	if err != nil {
		return nil, &InputError{Kind: InputErrorRead, Message: "read prompt from stdin failed"}
	}
	if len(content) > maxBytes {
		return nil, usageError("prompt exceeds 4 MiB")
	}
	return content, nil
}

func validatePrompt(content []byte) (string, error) {
	if len(content) > MaxPromptBytes {
		return "", usageError("prompt exceeds 4 MiB")
	}
	if !utf8.Valid(content) {
		return "", usageError("prompt must be valid UTF-8")
	}
	result := string(content)
	if strings.TrimSpace(result) == "" {
		return "", usageError("prompt must not be empty")
	}
	return result, nil
}

func usageError(message string) error {
	return &InputError{Kind: InputErrorUsage, Message: fmt.Sprintf("invalid prompt: %s", message)}
}
