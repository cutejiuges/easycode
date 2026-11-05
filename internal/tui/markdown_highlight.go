package tui

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

type highlightLanguage uint8

const (
	highlightPlain highlightLanguage = iota
	highlightGo
	highlightJSON
	highlightPython
	highlightJS
	highlightTS
	highlightShell
)

type highlightKind uint8

const (
	highlightText highlightKind = iota
	highlightKeyword
	highlightIdentifier
	highlightNumber
	highlightString
	highlightComment
	highlightOperator
	highlightProperty
	highlightVariable
)

type highlightFailure uint8

const (
	highlightOK highlightFailure = iota
	highlightBudgetExceeded
	highlightScanFailed
)

type highlightToken struct {
	start, end int32
	kind       highlightKind
}
type highlightLimits struct{ steps, nodes, bytes, depth int }
type highlightResult struct {
	tokens                []highlightToken
	failure               highlightFailure
	steps, capacity, peak int
}

// highlightScanner 的游标、预算和词法状态均属于一次扫描，不共享可变表或后台工作。
type highlightScanner struct {
	split            bool
	source           string
	language         highlightLanguage
	pos, steps, peak int
	limits           highlightLimits
	tokens           []highlightToken
	failure          highlightFailure
	depth            int
	expression       bool
	heredocs         []highlightHereDoc
}
type highlightHereDoc struct {
	start, end int
	tabs       bool
}

func codeLanguage(info string) highlightLanguage {
	fields := strings.Fields(info)
	if len(fields) == 0 {
		return highlightPlain
	}
	switch strings.ToLower(fields[0]) {
	case "go", "golang":
		return highlightGo
	case "json":
		return highlightJSON
	case "py", "python", "python3":
		return highlightPython
	case "js", "javascript", "mjs", "cjs":
		return highlightJS
	case "ts", "typescript":
		return highlightTS
	case "sh", "bash", "shell", "zsh":
		return highlightShell
	default:
		return highlightPlain
	}
}

func scanHighlight(source string, language highlightLanguage, limits highlightLimits) highlightResult {
	s := highlightScanner{source: source, language: language, limits: limits, expression: true}
	if len(source) > 256<<10 || limits.depth < 0 || len(source) > 0 && limits.bytes < 16<<10 {
		return highlightResult{failure: highlightBudgetExceeded}
	}
	// 校验同样计入字节工作预算，避免把标准库调用藏在计数之外。
	if !s.charge(len(source)) {
		return s.result()
	}
	if !utf8.ValidString(source) {
		s.failure = highlightScanFailed
		return s.result()
	}
	for s.pos < len(source) && s.failure == highlightOK {
		start := s.pos
		s.split = false
		kind := s.next()
		if s.pos <= start {
			s.fail(highlightScanFailed)
			break
		}
		if !s.split {
			s.emit(start, s.pos, kind)
		}
	}
	return s.result()
}
func (s *highlightScanner) result() highlightResult {
	result := highlightResult{failure: s.failure, steps: s.steps, peak: s.peak}
	if s.failure == highlightOK {
		result.tokens = s.tokens
		result.capacity = cap(s.tokens) * 12
	}
	return result
}
func (s *highlightScanner) fail(reason highlightFailure) {
	if s.failure == highlightOK {
		s.failure = reason
	}
}
func (s *highlightScanner) charge(n int) bool {
	if s.failure != highlightOK {
		return false
	}
	if n > s.limits.steps-s.steps {
		s.fail(highlightBudgetExceeded)
		return false
	}
	s.steps += n
	return true
}
func (s *highlightScanner) at(i int) byte {
	if i < 0 || i >= len(s.source) || !s.charge(1) {
		return 0
	}
	return s.source[i]
}
func (s *highlightScanner) prefix(value string) bool {
	if len(value) > len(s.source)-s.pos {
		return false
	}
	for i := range len(value) {
		if s.at(s.pos+i) != value[i] {
			return false
		}
	}
	return s.failure == highlightOK
}
func (s *highlightScanner) advance() {
	if s.pos >= len(s.source) || !s.charge(min(4, len(s.source)-s.pos)+1) {
		return
	}
	_, size := utf8.DecodeRuneInString(s.source[s.pos:])
	s.pos += size
}
func (s *highlightScanner) emit(start, end int, kind highlightKind) {
	if start == end {
		return
	}
	if !s.charge(1) {
		return
	}
	n := len(s.tokens)
	if n > 0 && s.tokens[n-1].kind == kind && int(s.tokens[n-1].end) == start {
		s.tokens[n-1].end = int32(end)
		return
	}
	if n >= s.limits.nodes {
		s.fail(highlightBudgetExceeded)
		return
	}
	if n == cap(s.tokens) {
		capacity := min(s.limits.nodes, max(16, cap(s.tokens)*2))
		// 扩容时旧数组仍存活，按两份容量预留。
		reservation := (capacity+cap(s.tokens))*12 + 16<<10
		if reservation > s.limits.bytes {
			s.fail(highlightBudgetExceeded)
			return
		}
		s.peak = max(s.peak, reservation)
		next := make([]highlightToken, n, capacity)
		copy(next, s.tokens)
		s.tokens = next
	}
	s.tokens = append(s.tokens, highlightToken{int32(start), int32(end), kind})
}
func (s *highlightScanner) enter() bool {
	if !s.charge(1) {
		return false
	}
	if s.depth >= min(64, s.limits.depth) {
		s.fail(highlightBudgetExceeded)
		return false
	}
	s.depth++
	return true
}
func (s *highlightScanner) leave() {
	if s.charge(1) {
		s.depth--
	}
}
func (s *highlightScanner) untilLine() {
	for s.pos < len(s.source) && s.at(s.pos) != '\n' && s.failure == highlightOK {
		s.advance()
	}
}
func (s *highlightScanner) quoted(quote byte, triple, raw bool) {
	count := 1
	if triple {
		count = 3
	}
	s.pos += count
	for s.pos < len(s.source) && s.failure == highlightOK {
		if s.at(s.pos) == quote {
			matched := true
			for i := 1; i < count; i++ {
				if s.at(s.pos+i) != quote {
					matched = false
					break
				}
			}
			if matched {
				s.pos += count
				return
			}
		}
		if s.at(s.pos) == '\\' && (!raw || quote != '`') {
			s.advance()
			if s.pos < len(s.source) {
				s.advance()
			}
			continue
		}
		s.advance()
	}
}
func (s *highlightScanner) blockComment() {
	s.pos += 2
	for s.pos < len(s.source) && s.failure == highlightOK {
		if s.prefix("*/") {
			s.pos += 2
			return
		}
		s.advance()
	}
}
func (s *highlightScanner) identifier() string {
	start := s.pos
	for s.pos < len(s.source) && s.failure == highlightOK {
		if !s.charge(4) {
			break
		}
		r, size := utf8.DecodeRuneInString(s.source[s.pos:])
		if r != '_' && r != '$' && !unicode.IsLetter(r) && !unicode.IsDigit(r) && !unicode.IsMark(r) {
			break
		}
		s.pos += size
	}
	if !s.charge(s.pos - start) {
		return ""
	}
	return s.source[start:s.pos]
}
func (s *highlightScanner) number() {
	base := byte(10)
	if s.at(s.pos) == '0' {
		switch s.at(s.pos + 1) {
		case 'x', 'X':
			base = 16
			s.pos += 2
		case 'b', 'B':
			base = 2
			s.pos += 2
		case 'o', 'O':
			base = 8
			s.pos += 2
		}
	}
	fraction, exponent := false, false
	for s.pos < len(s.source) && s.failure == highlightOK {
		c := s.at(s.pos)
		valid := c >= '0' && c <= '9' && c-'0' < base || base == 16 && (c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F')
		if valid || c == '_' {
			s.pos++
			continue
		}
		if c == '.' && !fraction && !exponent && (base == 10 || base == 16) {
			fraction = true
			s.pos++
			continue
		}
		if !exponent && (base == 10 && (c == 'e' || c == 'E') || base == 16 && (c == 'p' || c == 'P')) {
			exponent = true
			base = 10
			s.pos++
			if s.at(s.pos) == '+' || s.at(s.pos) == '-' {
				s.pos++
			}
			continue
		}
		break
	}
	c := s.at(s.pos)
	if s.language == highlightGo && c == 'i' || s.language == highlightPython && (c == 'j' || c == 'J') || (s.language == highlightJS || s.language == highlightTS) && c == 'n' {
		s.pos++
	}
}
func highlightWord(language highlightLanguage, word string) bool {
	switch language {
	case highlightGo:
		switch word {
		case "break", "case", "chan", "const", "continue", "default", "defer", "else", "fallthrough", "for", "func", "go", "goto", "if", "import", "interface", "map", "package", "range", "return", "select", "struct", "switch", "type", "var", "true", "false", "nil":
			return true
		}
	case highlightJSON:
		switch word {
		case "true", "false", "null":
			return true
		}
	case highlightPython:
		switch word {
		case "False", "None", "True", "and", "as", "assert", "async", "await", "break", "class", "continue", "def", "del", "elif", "else", "except", "finally", "for", "from", "global", "if", "import", "in", "is", "lambda", "nonlocal", "not", "or", "pass", "raise", "return", "try", "while", "with", "yield", "match", "case":
			return true
		}
	case highlightJS, highlightTS:
		switch word {
		case "async", "await", "break", "case", "catch", "class", "const", "continue", "debugger", "default", "delete", "do", "else", "export", "extends", "false", "finally", "for", "function", "if", "import", "in", "instanceof", "let", "new", "null", "return", "super", "switch", "this", "throw", "true", "try", "typeof", "var", "void", "while", "with", "yield", "interface", "type", "enum", "implements", "declare", "namespace", "readonly", "public", "private", "protected", "as", "satisfies", "keyof", "infer", "never", "unknown", "number", "string", "boolean":
			return true
		}
	case highlightShell:
		switch word {
		case "if", "then", "else", "elif", "fi", "case", "esac", "for", "select", "while", "until", "do", "done", "in", "function", "time", "coproc":
			return true
		}
	}
	return false
}
