package tool

import (
	"easycode/internal/codec"
	"errors"
	"fmt"
	"path"
	"regexp"
	"strings"
	"unicode/utf8"
)

const (
	maxSearchInputBytes   = 64 << 10
	maxSearchPatternBytes = 4 << 10
	maxSearchPathBytes    = 4 << 10
	maxGlobSegments       = 256
	defaultGlobLimit      = 100
	maxSearchLimit        = 1000
	defaultGrepLimit      = 250
	maxGrepContext        = 20
)

// GrepOutputMode 描述 Grep 的模型可见结果形态。
type GrepOutputMode string

const (
	GrepOutputContent          GrepOutputMode = "content"
	GrepOutputFilesWithMatches GrepOutputMode = "files_with_matches"
	GrepOutputCount            GrepOutputMode = "count"
)

// Valid 判断输出模式是否受当前实现支持。
func (mode GrepOutputMode) Valid() bool {
	switch mode {
	case GrepOutputContent, GrepOutputFilesWithMatches, GrepOutputCount:
		return true
	default:
		return false
	}
}

// GlobInput 是 Glob 的不可变、已默认化输入。
type GlobInput struct {
	pattern string
	path    string
	limit   int
}

// NewGlobInput 构造并校验 Glob 输入；零 limit 使用默认值。
func NewGlobInput(pattern string, searchPath string, limit int) (GlobInput, error) {
	if limit == 0 {
		limit = defaultGlobLimit
	}
	input := GlobInput{pattern: pattern, path: searchPath, limit: limit}
	if err := input.Validate(); err != nil {
		return GlobInput{}, err
	}
	return input, nil
}

// DecodeGlobInput 严格解码 Glob JSON 对象并应用默认值。
func DecodeGlobInput(data []byte) (GlobInput, error) {
	if len(data) == 0 || len(data) > maxSearchInputBytes {
		return GlobInput{}, errors.New("invalid GlobInput size")
	}
	wire := struct {
		Pattern string `json:"pattern"`
		Path    string `json:"path"`
		Limit   int    `json:"limit"`
	}{Limit: defaultGlobLimit}
	if err := codec.DecodeObject(data, &wire, "pattern"); err != nil {
		return GlobInput{}, err
	}
	input := GlobInput{pattern: wire.Pattern, path: wire.Path, limit: wire.Limit}
	return input, input.Validate()
}

func (input GlobInput) Pattern() string { return input.pattern }
func (input GlobInput) Path() string    { return input.path }
func (input GlobInput) Limit() int      { return input.limit }

// Validate 验证 Glob 表达式、相对路径和结果上限。
func (input GlobInput) Validate() error {
	if err := validateGlobPattern(input.pattern, false); err != nil {
		return fmt.Errorf("invalid Glob pattern: %w", err)
	}
	if err := validateSearchPath(input.path); err != nil {
		return fmt.Errorf("invalid Glob path: %w", err)
	}
	if input.limit < 1 || input.limit > maxSearchLimit {
		return errors.New("glob limit must be between 1 and 1000")
	}
	return nil
}

// GrepInput 是 Grep 的不可变、已默认化输入。
type GrepInput struct {
	pattern         string
	path            string
	glob            string
	outputMode      GrepOutputMode
	caseInsensitive bool
	beforeContext   int
	afterContext    int
	limit           int
}

// NewGrepInput 构造并校验 Grep 输入；空 mode 和零 limit 使用默认值。
func NewGrepInput(pattern string, searchPath string, glob string, outputMode GrepOutputMode, caseInsensitive bool, beforeContext int, afterContext int, limit int) (GrepInput, error) {
	if outputMode == "" {
		outputMode = GrepOutputContent
	}
	if limit == 0 {
		limit = defaultGrepLimit
	}
	input := GrepInput{
		pattern: pattern, path: searchPath, glob: glob, outputMode: outputMode,
		caseInsensitive: caseInsensitive, beforeContext: beforeContext, afterContext: afterContext, limit: limit,
	}
	if err := input.Validate(); err != nil {
		return GrepInput{}, err
	}
	return input, nil
}

// DecodeGrepInput 严格解码 Grep JSON 对象并应用默认值。
func DecodeGrepInput(data []byte) (GrepInput, error) {
	if len(data) == 0 || len(data) > maxSearchInputBytes {
		return GrepInput{}, errors.New("invalid GrepInput size")
	}
	wire := struct {
		Pattern         string         `json:"pattern"`
		Path            string         `json:"path"`
		Glob            string         `json:"glob"`
		OutputMode      GrepOutputMode `json:"output_mode"`
		CaseInsensitive bool           `json:"case_insensitive"`
		BeforeContext   int            `json:"before_context"`
		AfterContext    int            `json:"after_context"`
		Limit           int            `json:"limit"`
	}{OutputMode: GrepOutputContent, Limit: defaultGrepLimit}
	if err := codec.DecodeObject(data, &wire, "pattern"); err != nil {
		return GrepInput{}, err
	}
	input := GrepInput{pattern: wire.Pattern, path: wire.Path, glob: wire.Glob, outputMode: wire.OutputMode, caseInsensitive: wire.CaseInsensitive, beforeContext: wire.BeforeContext, afterContext: wire.AfterContext, limit: wire.Limit}
	return input, input.Validate()
}

func (input GrepInput) Pattern() string            { return input.pattern }
func (input GrepInput) Path() string               { return input.path }
func (input GrepInput) Glob() string               { return input.glob }
func (input GrepInput) OutputMode() GrepOutputMode { return input.outputMode }
func (input GrepInput) CaseInsensitive() bool      { return input.caseInsensitive }
func (input GrepInput) BeforeContext() int         { return input.beforeContext }
func (input GrepInput) AfterContext() int          { return input.afterContext }
func (input GrepInput) Limit() int                 { return input.limit }

// Validate 验证 Grep regexp、过滤规则、相对路径和结果预算。
func (input GrepInput) Validate() error {
	if input.pattern == "" || len(input.pattern) > maxSearchPatternBytes || !utf8.ValidString(input.pattern) || strings.ContainsRune(input.pattern, 0) {
		return errors.New("grep pattern must be non-empty UTF-8 and at most 4 KiB")
	}
	regexpPattern := input.pattern
	if input.caseInsensitive {
		regexpPattern = "(?i:" + regexpPattern + ")"
	}
	if _, err := regexp.Compile(regexpPattern); err != nil {
		return errors.New("grep pattern is not a valid RE2 expression")
	}
	if err := validateSearchPath(input.path); err != nil {
		return fmt.Errorf("invalid Grep path: %w", err)
	}
	if input.glob != "" {
		if err := validateGlobPattern(input.glob, false); err != nil {
			return fmt.Errorf("invalid Grep glob: %w", err)
		}
	}
	if !input.outputMode.Valid() {
		return errors.New("grep output_mode is unsupported")
	}
	if input.beforeContext < 0 || input.beforeContext > maxGrepContext || input.afterContext < 0 || input.afterContext > maxGrepContext {
		return errors.New("grep context must be between 0 and 20")
	}
	if input.limit < 1 || input.limit > maxSearchLimit {
		return errors.New("grep limit must be between 1 and 1000")
	}
	return nil
}

func validateSearchPath(value string) error {
	if value == "" {
		return nil
	}
	if len(value) > maxSearchPathBytes || !utf8.ValidString(value) || strings.ContainsRune(value, 0) || strings.Contains(value, `\`) {
		return errors.New("path must be workspace-relative UTF-8")
	}
	if strings.HasPrefix(value, "/") {
		return errors.New("path must be workspace-relative")
	}
	segments := strings.Split(value, "/")
	for _, segment := range segments {
		if segment == "" || segment == "." || segment == ".." {
			return errors.New("path must not contain empty or traversal segments")
		}
	}
	return nil
}

func validateGlobPattern(value string, optional bool) error {
	if value == "" {
		if optional {
			return nil
		}
		return errors.New("pattern is required")
	}
	if len(value) > maxSearchPatternBytes || !utf8.ValidString(value) || strings.ContainsRune(value, 0) || strings.Contains(value, `\`) || strings.HasPrefix(value, "/") {
		return errors.New("pattern must be workspace-relative UTF-8 and at most 4 KiB")
	}
	segments := strings.Split(value, "/")
	if len(segments) > maxGlobSegments {
		return errors.New("pattern has more than 256 segments")
	}
	for _, segment := range segments {
		if segment == "" || segment == "." || segment == ".." {
			return errors.New("pattern contains an invalid path segment")
		}
		if segment == "**" {
			continue
		}
		if _, err := path.Match(segment, "probe"); err != nil {
			return errors.New("pattern contains invalid glob syntax")
		}
	}
	return nil
}
