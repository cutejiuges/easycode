package builtin

import (
	"path"
	"strings"

	"easycode/internal/tool"
)

type globMatcher struct {
	segments []string
}

func compileGlobMatcher(pattern string) (globMatcher, error) {
	input, err := tool.NewGlobInput(pattern, "", 1)
	if err != nil {
		return globMatcher{}, err
	}
	return globMatcher{segments: strings.Split(input.Pattern(), "/")}, nil
}

// Match 对规范 workspace-relative 路径执行带 memo 的分段匹配。
func (matcher globMatcher) Match(candidate string) bool {
	if candidate == "" || strings.HasPrefix(candidate, "/") || strings.Contains(candidate, `\`) {
		return false
	}
	pathSegments := strings.Split(candidate, "/")
	for _, segment := range pathSegments {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
	}
	type state struct{ pattern, candidate int }
	stateCount := (len(matcher.segments) + 1) * (len(pathSegments) + 1)
	memo := make(map[state]bool, stateCount)
	known := make(map[state]struct{}, stateCount)
	var match func(int, int) bool
	match = func(patternIndex int, candidateIndex int) bool {
		current := state{pattern: patternIndex, candidate: candidateIndex}
		if _, exists := known[current]; exists {
			return memo[current]
		}
		known[current] = struct{}{}
		matched := false
		switch {
		case patternIndex == len(matcher.segments):
			matched = candidateIndex == len(pathSegments)
		case matcher.segments[patternIndex] == "**":
			matched = match(patternIndex+1, candidateIndex) ||
				(candidateIndex < len(pathSegments) && match(patternIndex, candidateIndex+1))
		case candidateIndex < len(pathSegments):
			segmentMatched, err := path.Match(matcher.segments[patternIndex], pathSegments[candidateIndex])
			matched = err == nil && segmentMatched && match(patternIndex+1, candidateIndex+1)
		}
		memo[current] = matched
		return matched
	}
	return match(0, 0)
}
