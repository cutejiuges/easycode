package tool

import (
	"errors"
	"sort"
	"unicode/utf8"
)

// SearchIncompleteReason 说明搜索空间为何无法完整扫描。
type SearchIncompleteReason string

const (
	SearchComplete          SearchIncompleteReason = ""
	SearchEntryLimitReached SearchIncompleteReason = "entry_limit_reached"
	SearchFileLimitReached  SearchIncompleteReason = "file_limit_reached"
	SearchByteLimitReached  SearchIncompleteReason = "byte_limit_reached"
)

func (reason SearchIncompleteReason) Valid() bool {
	switch reason {
	case SearchComplete, SearchEntryLimitReached, SearchFileLimitReached, SearchByteLimitReached:
		return true
	default:
		return false
	}
}

// SearchSkipCounts 保存不含路径和正文的跳过文件统计。
type SearchSkipCounts struct {
	binary      int
	invalidUTF8 int
	tooLarge    int
	unreadable  int
	unsupported int
	disappeared int
}

func NewSearchSkipCounts(binary int, invalidUTF8 int, tooLarge int, unreadable int, unsupported int, disappeared int) (SearchSkipCounts, error) {
	counts := SearchSkipCounts{binary: binary, invalidUTF8: invalidUTF8, tooLarge: tooLarge, unreadable: unreadable, unsupported: unsupported, disappeared: disappeared}
	if err := counts.Validate(); err != nil {
		return SearchSkipCounts{}, err
	}
	return counts, nil
}

func (counts SearchSkipCounts) Binary() int      { return counts.binary }
func (counts SearchSkipCounts) InvalidUTF8() int { return counts.invalidUTF8 }
func (counts SearchSkipCounts) TooLarge() int    { return counts.tooLarge }
func (counts SearchSkipCounts) Unreadable() int  { return counts.unreadable }
func (counts SearchSkipCounts) Unsupported() int { return counts.unsupported }
func (counts SearchSkipCounts) Disappeared() int { return counts.disappeared }

func (counts SearchSkipCounts) Validate() error {
	if counts.binary < 0 || counts.invalidUTF8 < 0 || counts.tooLarge < 0 || counts.unreadable < 0 || counts.unsupported < 0 || counts.disappeared < 0 {
		return errors.New("search skip counts are invalid")
	}
	return nil
}

// GlobResultMetadata 保存确定性匹配与有界扫描信息。
type GlobResultMetadata struct {
	matches          []string
	truncated        bool
	omittedMatches   int
	visitedEntries   int
	incompleteReason SearchIncompleteReason
	skipped          SearchSkipCounts
}

func NewGlobResultMetadata(matches []string, truncated bool, omittedMatches int, visitedEntries int, incompleteReason SearchIncompleteReason, skipped SearchSkipCounts) (GlobResultMetadata, error) {
	metadata := GlobResultMetadata{
		matches: append([]string(nil), matches...), truncated: truncated, omittedMatches: omittedMatches,
		visitedEntries: visitedEntries, incompleteReason: incompleteReason, skipped: skipped,
	}
	if err := metadata.Validate(); err != nil {
		return GlobResultMetadata{}, err
	}
	return metadata, nil
}

func (metadata GlobResultMetadata) Matches() []string {
	return append([]string(nil), metadata.matches...)
}
func (metadata GlobResultMetadata) Truncated() bool     { return metadata.truncated }
func (metadata GlobResultMetadata) OmittedMatches() int { return metadata.omittedMatches }
func (metadata GlobResultMetadata) VisitedEntries() int { return metadata.visitedEntries }
func (metadata GlobResultMetadata) IncompleteReason() SearchIncompleteReason {
	return metadata.incompleteReason
}
func (metadata GlobResultMetadata) Skipped() SearchSkipCounts { return metadata.skipped }

func (metadata GlobResultMetadata) Validate() error {
	if metadata.omittedMatches < 0 || metadata.visitedEntries < 0 || !metadata.incompleteReason.Valid() || metadata.skipped.Validate() != nil {
		return errors.New("glob result metadata is invalid")
	}
	if metadata.truncated != (metadata.omittedMatches > 0 || metadata.incompleteReason != SearchComplete) {
		return errors.New("glob truncation metadata is inconsistent")
	}
	if !sortedUniquePaths(metadata.matches) {
		return errors.New("glob matches must be sorted unique relative paths")
	}
	return nil
}

func (metadata GlobResultMetadata) zero() bool {
	return len(metadata.matches) == 0 && !metadata.truncated && metadata.omittedMatches == 0 && metadata.visitedEntries == 0 && metadata.incompleteReason == "" && metadata.skipped == (SearchSkipCounts{})
}

// GrepMatch 保存一种输出 mode 下的单个规范化结果。
type GrepMatch struct {
	relativePath string
	line         int
	text         string
	matchingLine bool
	count        int
}

func NewGrepContentMatch(relativePath string, line int, text string, matchingLine bool) (GrepMatch, error) {
	match := GrepMatch{relativePath: relativePath, line: line, text: text, matchingLine: matchingLine}
	if err := match.validateFor(GrepOutputContent); err != nil {
		return GrepMatch{}, err
	}
	return match, nil
}

func NewGrepFileMatch(relativePath string) (GrepMatch, error) {
	match := GrepMatch{relativePath: relativePath}
	if err := match.validateFor(GrepOutputFilesWithMatches); err != nil {
		return GrepMatch{}, err
	}
	return match, nil
}

func NewGrepCountMatch(relativePath string, count int) (GrepMatch, error) {
	match := GrepMatch{relativePath: relativePath, count: count}
	if err := match.validateFor(GrepOutputCount); err != nil {
		return GrepMatch{}, err
	}
	return match, nil
}

func (match GrepMatch) RelativePath() string { return match.relativePath }
func (match GrepMatch) Line() int            { return match.line }
func (match GrepMatch) Text() string         { return match.text }
func (match GrepMatch) MatchingLine() bool   { return match.matchingLine }
func (match GrepMatch) Count() int           { return match.count }

func (match GrepMatch) validateFor(mode GrepOutputMode) error {
	if validateResultRelativePath(match.relativePath) != nil {
		return errors.New("grep match path is invalid")
	}
	switch mode {
	case GrepOutputContent:
		if match.line < 1 || !utf8.ValidString(match.text) || match.count != 0 {
			return errors.New("grep content match is invalid")
		}
	case GrepOutputFilesWithMatches:
		if match.line != 0 || match.text != "" || match.matchingLine || match.count != 0 {
			return errors.New("grep file match is invalid")
		}
	case GrepOutputCount:
		if match.line != 0 || match.text != "" || match.matchingLine || match.count < 1 {
			return errors.New("grep count match is invalid")
		}
	default:
		return errors.New("grep match mode is invalid")
	}
	return nil
}

// GrepResultMetadata 保存规范化结果、匹配行计数与有界扫描信息。
type GrepResultMetadata struct {
	mode             GrepOutputMode
	matches          []GrepMatch
	matchingLines    int
	truncated        bool
	omittedMatches   int
	visitedEntries   int
	scannedFiles     int
	scannedBytes     int64
	incompleteReason SearchIncompleteReason
	skipped          SearchSkipCounts
}

func NewGrepResultMetadata(mode GrepOutputMode, matches []GrepMatch, matchingLines int, truncated bool, omittedMatches int, visitedEntries int, scannedFiles int, scannedBytes int64, incompleteReason SearchIncompleteReason, skipped SearchSkipCounts) (GrepResultMetadata, error) {
	metadata := GrepResultMetadata{
		mode: mode, matches: append([]GrepMatch(nil), matches...), matchingLines: matchingLines,
		truncated: truncated, omittedMatches: omittedMatches, visitedEntries: visitedEntries,
		scannedFiles: scannedFiles, scannedBytes: scannedBytes, incompleteReason: incompleteReason, skipped: skipped,
	}
	if err := metadata.Validate(); err != nil {
		return GrepResultMetadata{}, err
	}
	return metadata, nil
}

func (metadata GrepResultMetadata) Mode() GrepOutputMode { return metadata.mode }
func (metadata GrepResultMetadata) Matches() []GrepMatch {
	return append([]GrepMatch(nil), metadata.matches...)
}
func (metadata GrepResultMetadata) MatchingLines() int  { return metadata.matchingLines }
func (metadata GrepResultMetadata) Truncated() bool     { return metadata.truncated }
func (metadata GrepResultMetadata) OmittedMatches() int { return metadata.omittedMatches }
func (metadata GrepResultMetadata) VisitedEntries() int { return metadata.visitedEntries }
func (metadata GrepResultMetadata) ScannedFiles() int   { return metadata.scannedFiles }
func (metadata GrepResultMetadata) ScannedBytes() int64 { return metadata.scannedBytes }
func (metadata GrepResultMetadata) IncompleteReason() SearchIncompleteReason {
	return metadata.incompleteReason
}
func (metadata GrepResultMetadata) Skipped() SearchSkipCounts { return metadata.skipped }

func (metadata GrepResultMetadata) Validate() error {
	if !metadata.mode.Valid() || metadata.matchingLines < 0 || metadata.omittedMatches < 0 || metadata.visitedEntries < 0 || metadata.scannedFiles < 0 || metadata.scannedBytes < 0 || !metadata.incompleteReason.Valid() || metadata.skipped.Validate() != nil {
		return errors.New("grep result metadata is invalid")
	}
	if metadata.truncated != (metadata.omittedMatches > 0 || metadata.incompleteReason != SearchComplete) {
		return errors.New("grep truncation metadata is inconsistent")
	}
	previousPath := ""
	previousLine := 0
	for index, match := range metadata.matches {
		if err := match.validateFor(metadata.mode); err != nil {
			return err
		}
		if index > 0 && (match.relativePath < previousPath || (match.relativePath == previousPath && match.line <= previousLine)) {
			return errors.New("grep matches must be ordered by path and line")
		}
		previousPath, previousLine = match.relativePath, match.line
	}
	return nil
}

func (metadata GrepResultMetadata) zero() bool {
	return metadata.mode == "" && len(metadata.matches) == 0 && metadata.matchingLines == 0 && !metadata.truncated && metadata.omittedMatches == 0 && metadata.visitedEntries == 0 && metadata.scannedFiles == 0 && metadata.scannedBytes == 0 && metadata.incompleteReason == "" && metadata.skipped == (SearchSkipCounts{})
}

func sortedUniquePaths(paths []string) bool {
	if !sort.StringsAreSorted(paths) {
		return false
	}
	for index, value := range paths {
		if validateResultRelativePath(value) != nil || (index > 0 && paths[index-1] == value) {
			return false
		}
	}
	return true
}

func validateResultRelativePath(value string) error {
	if value == "" || !utf8.ValidString(value) {
		return errors.New("relative path is invalid")
	}
	return validateSearchPath(value)
}
