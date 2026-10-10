package tool

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	if len(data) == 0 || len(data) > maxSearchInputBytes || !utf8.Valid(data) {
		return GlobInput{}, errors.New("invalid Glob input")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('{') {
		return GlobInput{}, errors.New("glob input must be an object")
	}
	input := GlobInput{limit: defaultGlobLimit}
	seen := make(map[string]struct{}, 3)
	for decoder.More() {
		name, fieldErr := nextSearchField(decoder, seen, "Glob")
		if fieldErr != nil {
			return GlobInput{}, fieldErr
		}
		switch name {
		case "pattern":
			if err := decoder.Decode(&input.pattern); err != nil {
				return GlobInput{}, errors.New("invalid Glob pattern")
			}
		case "path":
			if err := decoder.Decode(&input.path); err != nil {
				return GlobInput{}, errors.New("invalid Glob path")
			}
		case "limit":
			if err := decoder.Decode(&input.limit); err != nil {
				return GlobInput{}, errors.New("invalid Glob limit")
			}
		default:
			return GlobInput{}, fmt.Errorf("unknown Glob input field %q", name)
		}
	}
	if err := closeSearchObject(decoder, "Glob"); err != nil {
		return GlobInput{}, err
	}
	if _, ok := seen["pattern"]; !ok {
		return GlobInput{}, errors.New("glob pattern is required")
	}
	if err := input.Validate(); err != nil {
		return GlobInput{}, err
	}
	return input, nil
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
	if len(data) == 0 || len(data) > maxSearchInputBytes || !utf8.Valid(data) {
		return GrepInput{}, errors.New("invalid Grep input")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('{') {
		return GrepInput{}, errors.New("grep input must be an object")
	}
	input := GrepInput{outputMode: GrepOutputContent, limit: defaultGrepLimit}
	seen := make(map[string]struct{}, 8)
	for decoder.More() {
		name, fieldErr := nextSearchField(decoder, seen, "Grep")
		if fieldErr != nil {
			return GrepInput{}, fieldErr
		}
		switch name {
		case "pattern":
			if err := decoder.Decode(&input.pattern); err != nil {
				return GrepInput{}, errors.New("invalid Grep pattern")
			}
		case "path":
			if err := decoder.Decode(&input.path); err != nil {
				return GrepInput{}, errors.New("invalid Grep path")
			}
		case "glob":
			if err := decoder.Decode(&input.glob); err != nil {
				return GrepInput{}, errors.New("invalid Grep glob")
			}
		case "output_mode":
			if err := decoder.Decode(&input.outputMode); err != nil {
				return GrepInput{}, errors.New("invalid Grep output_mode")
			}
		case "case_insensitive":
			if err := decoder.Decode(&input.caseInsensitive); err != nil {
				return GrepInput{}, errors.New("invalid Grep case_insensitive")
			}
		case "before_context":
			if err := decoder.Decode(&input.beforeContext); err != nil {
				return GrepInput{}, errors.New("invalid Grep before_context")
			}
		case "after_context":
			if err := decoder.Decode(&input.afterContext); err != nil {
				return GrepInput{}, errors.New("invalid Grep after_context")
			}
		case "limit":
			if err := decoder.Decode(&input.limit); err != nil {
				return GrepInput{}, errors.New("invalid Grep limit")
			}
		default:
			return GrepInput{}, fmt.Errorf("unknown Grep input field %q", name)
		}
	}
	if err := closeSearchObject(decoder, "Grep"); err != nil {
		return GrepInput{}, err
	}
	if _, ok := seen["pattern"]; !ok {
		return GrepInput{}, errors.New("grep pattern is required")
	}
	if err := input.Validate(); err != nil {
		return GrepInput{}, err
	}
	return input, nil
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

func nextSearchField(decoder *json.Decoder, seen map[string]struct{}, capability string) (string, error) {
	token, err := decoder.Token()
	if err != nil {
		return "", fmt.Errorf("invalid %s input", capability)
	}
	name, ok := token.(string)
	if !ok {
		return "", fmt.Errorf("invalid %s input field", capability)
	}
	if _, duplicate := seen[name]; duplicate {
		return "", fmt.Errorf("duplicate %s input field %q", capability, name)
	}
	seen[name] = struct{}{}
	return name, nil
}

func closeSearchObject(decoder *json.Decoder, capability string) error {
	closing, err := decoder.Token()
	if err != nil || closing != json.Delim('}') {
		return fmt.Errorf("invalid %s input", capability)
	}
	var trailing json.RawMessage
	err = decoder.Decode(&trailing)
	if errors.Is(err, io.EOF) {
		return nil
	}
	return fmt.Errorf("%s input has trailing JSON", strings.ToLower(capability))
}
