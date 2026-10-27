package tool

import (
	"bytes"
	"easycode/internal/codec"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

const (
	// MaxExecCommandBytes 限制单次前台命令的 UTF-8 字节数。
	MaxExecCommandBytes = 64 << 10
	// DefaultExecTimeoutMillis 是命令未显式设置超时时使用的默认值。
	DefaultExecTimeoutMillis = 120_000
	// MinExecTimeoutMillis 是模型可请求的最短命令超时。
	MinExecTimeoutMillis = 1_000
	// MaxExecTimeoutMillis 是模型可请求的最长命令超时。
	MaxExecTimeoutMillis = 600_000

	maxExecInputBytes = 80 << 10
)

// ExecInput 是前台命令执行的不可变、已默认化输入。
type ExecInput struct {
	command       string
	workdir       string
	timeoutMillis int
}

// NewExecInput 构造并校验命令输入。
func NewExecInput(command, workdir string, timeoutMillis int) (ExecInput, error) {
	if timeoutMillis == 0 {
		timeoutMillis = DefaultExecTimeoutMillis
	}
	input := ExecInput{command: command, workdir: workdir, timeoutMillis: timeoutMillis}
	if err := input.Validate(); err != nil {
		return ExecInput{}, err
	}
	return input, nil
}

// DecodeExecInput 严格解码命令 JSON 对象并应用默认超时。
func DecodeExecInput(data []byte) (ExecInput, error) {
	if len(data) == 0 || len(data) > maxExecInputBytes {
		return ExecInput{}, errors.New("invalid ExecInput size")
	}
	wire := struct {
		Command       string `json:"command"`
		Workdir       string `json:"workdir"`
		TimeoutMillis int    `json:"timeout_ms"`
	}{TimeoutMillis: DefaultExecTimeoutMillis}
	if err := codec.DecodeObject(data, &wire, "command"); err != nil {
		return ExecInput{}, err
	}
	input := ExecInput{command: wire.Command, workdir: wire.Workdir, timeoutMillis: wire.TimeoutMillis}
	return input, input.Validate()
}

// Command 返回完整命令文本。
func (input ExecInput) Command() string { return input.command }

// Workdir 返回规范化前的 workspace 相对工作目录；空值表示 workspace 根。
func (input ExecInput) Workdir() string { return input.workdir }

// TimeoutMillis 返回默认化后的超时毫秒数。
func (input ExecInput) TimeoutMillis() int { return input.timeoutMillis }

// Validate 验证命令、相对工作目录和超时边界。
func (input ExecInput) Validate() error {
	if strings.TrimSpace(input.command) == "" || len(input.command) > MaxExecCommandBytes || !utf8.ValidString(input.command) || bytes.IndexByte([]byte(input.command), 0) >= 0 {
		return errors.New("Exec command must be non-empty UTF-8 and at most 64 KiB")
	}
	if err := validateSearchPath(input.workdir); err != nil {
		return fmt.Errorf("invalid Exec workdir: %w", err)
	}
	if input.timeoutMillis < MinExecTimeoutMillis || input.timeoutMillis > MaxExecTimeoutMillis {
		return errors.New("Exec timeout_ms must be between 1000 and 600000")
	}
	return nil
}

func (input ExecInput) zero() bool {
	return input.command == "" && input.workdir == "" && input.timeoutMillis == 0
}
