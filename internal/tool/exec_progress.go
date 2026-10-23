package tool

import (
	"errors"
	"unicode/utf8"
)

// ExecProgress 是owner发布的有界输出统计；不属于Session事实源。
type ExecProgress struct {
	elapsedMillis int64
	stdoutBytes   int64
	stderrBytes   int64
	stdoutLines   int64
	stderrLines   int64
	recent        string
	truncated     bool
}

// MaxExecRecentBytes 限制单个瞬态进度快照的近期输出。
const MaxExecRecentBytes = 4096

// NewExecProgress 构造不含PID或执行环境的不可变进度。
func NewExecProgress(elapsedMillis, stdoutBytes, stderrBytes, stdoutLines, stderrLines int64, recent string, truncated bool) (ExecProgress, error) {
	progress := ExecProgress{elapsedMillis: elapsedMillis, stdoutBytes: stdoutBytes, stderrBytes: stderrBytes, stdoutLines: stdoutLines, stderrLines: stderrLines, recent: recent, truncated: truncated}
	if err := progress.Validate(); err != nil {
		return ExecProgress{}, err
	}
	return progress, nil
}

func (progress ExecProgress) ElapsedMillis() int64 { return progress.elapsedMillis }
func (progress ExecProgress) StdoutBytes() int64   { return progress.stdoutBytes }
func (progress ExecProgress) StderrBytes() int64   { return progress.stderrBytes }
func (progress ExecProgress) StdoutLines() int64   { return progress.stdoutLines }
func (progress ExecProgress) StderrLines() int64   { return progress.stderrLines }
func (progress ExecProgress) Recent() string       { return progress.recent }
func (progress ExecProgress) Truncated() bool      { return progress.truncated }

// Validate 验证瞬态统计与文本预算。
func (progress ExecProgress) Validate() error {
	if progress.elapsedMillis < 0 || progress.stdoutBytes < 0 || progress.stderrBytes < 0 || progress.stdoutLines < 0 || progress.stderrLines < 0 || progress.stdoutLines > progress.stdoutBytes || progress.stderrLines > progress.stderrBytes || len(progress.recent) > MaxExecRecentBytes || !utf8.ValidString(progress.recent) {
		return errors.New("invalid Exec progress")
	}
	return nil
}
