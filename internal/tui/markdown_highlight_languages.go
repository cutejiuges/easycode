package tui

import "unicode/utf8"

func (s *highlightScanner) next() highlightKind {
	c := s.at(s.pos)
	if s.language == highlightPlain {
		s.advance()
		return highlightText
	}
	if c == ' ' || c == '\t' || c == '\r' || c == '\n' {
		s.pos++
		if c == '\n' && s.language == highlightShell && len(s.heredocs) > 0 {
			s.hereBodies()
			return highlightString
		}
		return highlightText
	}
	if s.language == highlightShell {
		return s.shellToken()
	}
	if s.language == highlightPython && c == '#' {
		s.untilLine()
		return highlightComment
	}
	if s.language != highlightJSON && s.language != highlightPython {
		if s.prefix("//") {
			s.untilLine()
			return highlightComment
		}
		if s.prefix("/*") {
			s.blockComment()
			return highlightComment
		}
	}
	if c == '\'' || c == '"' {
		triple := s.language == highlightPython && s.at(s.pos+1) == c && s.at(s.pos+2) == c
		s.quoted(c, triple, false)
		s.expression = false
		if s.language == highlightJSON && c == '"' {
			i := s.pos
			for i < len(s.source) && s.failure == highlightOK {
				b := s.at(i)
				if b != ' ' && b != '\n' && b != '\r' && b != '\t' {
					break
				}
				i++
			}
			if s.at(i) == ':' {
				return highlightProperty
			}
		}
		return highlightString
	}
	if c == '`' && s.language == highlightGo {
		s.quoted(c, false, true)
		return highlightString
	}
	if c == '`' && (s.language == highlightJS || s.language == highlightTS) {
		s.template()
		s.expression = false
		return highlightString
	}
	if c == '/' && (s.language == highlightJS || s.language == highlightTS) && s.expression {
		s.regexLiteral()
		s.expression = false
		return highlightString
	}
	if c >= '0' && c <= '9' || c == '.' && s.at(s.pos+1) >= '0' && s.at(s.pos+1) <= '9' {
		s.number()
		s.expression = false
		return highlightNumber
	}
	if c == '_' || c == '$' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= 128 {
		start := s.pos
		word := s.identifier()
		if s.pos == start {
			s.advance()
			return highlightText
		}
		if s.language == highlightPython && pythonStringPrefix(word) && (s.at(s.pos) == '\'' || s.at(s.pos) == '"') {
			q := s.at(s.pos)
			triple := s.at(s.pos+1) == q && s.at(s.pos+2) == q
			s.pythonString(q, triple, word)
			return highlightString
		}
		s.expression = false
		if highlightWord(s.language, word) {
			switch word {
			case "return", "throw", "case", "yield", "typeof", "void", "delete", "new", "in", "of":
				s.expression = true
			}
			return highlightKeyword
		}
		return highlightIdentifier
	}
	s.advance()
	s.expression = c != ')' && c != ']' && c != '}'
	return highlightOperator
}
func pythonStringPrefix(word string) bool {
	switch word {
	case "r", "R", "u", "U", "b", "B", "f", "F", "br", "bR", "Br", "BR", "rb", "rB", "Rb", "RB", "fr", "fR", "Fr", "FR", "rf", "rF", "Rf", "RF":
		return true
	}
	return false
}
func (s *highlightScanner) pythonString(q byte, triple bool, prefix string) {
	formatted := false
	for _, c := range prefix {
		if c == 'f' || c == 'F' {
			formatted = true
		}
	}
	if !formatted {
		s.quoted(q, triple, false)
		return
	}
	count := 1
	if triple {
		count = 3
	}
	s.pos += count
	for s.pos < len(s.source) && s.failure == highlightOK {
		c := s.at(s.pos)
		if c == q {
			end := true
			for i := 1; i < count; i++ {
				if s.at(s.pos+i) != q {
					end = false
					break
				}
			}
			if end {
				s.pos += count
				return
			}
		}
		if c == '\\' {
			s.advance()
			if s.pos < len(s.source) {
				s.advance()
			}
			continue
		}
		if c == '{' {
			s.pos++
			if s.at(s.pos) == '{' {
				s.pos++
				continue
			}
			s.balanced('}', highlightPython)
			continue
		}
		s.advance()
	}
}
func (s *highlightScanner) template() {
	if !s.enter() {
		return
	}
	defer s.leave()
	s.pos++
	for s.pos < len(s.source) && s.failure == highlightOK {
		if s.at(s.pos) == '`' {
			s.pos++
			return
		}
		if s.at(s.pos) == '\\' {
			s.advance()
			if s.pos < len(s.source) {
				s.advance()
			}
			continue
		}
		if s.prefix("${") {
			s.pos += 2
			s.balanced('}', s.language)
			continue
		}
		s.advance()
	}
}
func (s *highlightScanner) balanced(close byte, language highlightLanguage) {
	if !s.enter() {
		return
	}
	defer s.leave()
	for s.pos < len(s.source) && s.failure == highlightOK {
		c := s.at(s.pos)
		if c == close {
			s.pos++
			return
		}
		if c == '\'' || c == '"' {
			s.quoted(c, language == highlightPython && s.at(s.pos+1) == c && s.at(s.pos+2) == c, false)
			continue
		}
		if c == '`' && (language == highlightJS || language == highlightTS) {
			s.template()
			continue
		}
		if language == highlightJS || language == highlightTS {
			if s.prefix("//") {
				s.untilLine()
				continue
			}
			if s.prefix("/*") {
				s.blockComment()
				continue
			}
		}
		if c == '#' && language == highlightPython {
			s.untilLine()
			continue
		}
		switch c {
		case '{':
			s.pos++
			s.balanced('}', language)
		case '(':
			s.pos++
			s.balanced(')', language)
		case '[':
			s.pos++
			s.balanced(']', language)
		default:
			s.advance()
		}
	}
}
func (s *highlightScanner) regexLiteral() {
	s.pos++
	group := false
	for s.pos < len(s.source) && s.failure == highlightOK {
		c := s.at(s.pos)
		if c == '\n' || c == '\r' {
			return
		}
		if c == '\\' {
			s.advance()
			if s.pos < len(s.source) {
				s.advance()
			}
			continue
		}
		if c == '[' {
			group = true
		}
		if c == ']' {
			group = false
		}
		s.advance()
		if c == '/' && !group {
			s.identifier()
			return
		}
	}
}
func (s *highlightScanner) shellToken() highlightKind {
	c := s.at(s.pos)
	if c == '#' && (s.pos == 0 || shellBoundary(s.at(s.pos-1))) {
		s.untilLine()
		return highlightComment
	}
	if c == '"' {
		s.shellStyledQuote()
		return highlightString
	}
	if c == '\'' || c == '`' {
		s.shellQuote(c)
		return highlightString
	}
	if s.prefix("$'") {
		s.pos++
		s.quoted('\'', false, false)
		return highlightString
	}
	if c == '$' {
		s.pos++
		if s.at(s.pos) == '{' {
			s.pos++
			s.shellBalanced('}')
		} else if s.at(s.pos) == '(' {
			s.pos++
			s.shellBalanced(')')
		} else {
			start := s.pos
			s.identifier()
			if s.pos == start && s.pos < len(s.source) {
				s.advance()
			}
		}
		return highlightVariable
	}
	if s.prefix("<<") && !s.prefix("<<<") {
		s.hereDeclaration()
		return highlightOperator
	}
	if c == '\\' {
		s.advance()
		if s.pos < len(s.source) {
			s.advance()
		}
		return highlightText
	}
	if c >= '0' && c <= '9' {
		s.number()
		return highlightNumber
	}
	if c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= 128 {
		start := s.pos
		word := s.identifier()
		if s.pos == start {
			s.advance()
		}
		if highlightWord(s.language, word) {
			return highlightKeyword
		}
		return highlightIdentifier
	}
	s.advance()
	return highlightOperator
}
func shellBoundary(c byte) bool {
	switch c {
	case ' ', '\t', '\n', '\r', ';', '|', '&', '(', ')':
		return true
	}
	return false
}
func (s *highlightScanner) shellQuote(q byte) {
	s.pos++
	for s.pos < len(s.source) && s.failure == highlightOK {
		c := s.at(s.pos)
		if c == q {
			s.pos++
			return
		}
		if q != '\'' && c == '\\' {
			s.advance()
			if s.pos < len(s.source) {
				s.advance()
			}
			continue
		}
		if q != '\'' && s.prefix("$(") {
			s.pos += 2
			s.shellBalanced(')')
			continue
		}
		if q != '\'' && s.prefix("${") {
			s.pos += 2
			s.shellBalanced('}')
			continue
		}
		s.advance()
	}
}
func (s *highlightScanner) shellBalanced(close byte) {
	if !s.enter() {
		return
	}
	defer s.leave()
	for s.pos < len(s.source) && s.failure == highlightOK {
		c := s.at(s.pos)
		if c == close {
			s.pos++
			return
		}
		if c == '\'' || c == '"' || c == '`' {
			s.shellQuote(c)
			continue
		}
		if c == '(' {
			s.pos++
			s.shellBalanced(')')
			continue
		}
		if c == '{' {
			s.pos++
			s.shellBalanced('}')
			continue
		}
		if c == '\\' {
			s.advance()
			if s.pos < len(s.source) {
				s.advance()
			}
			continue
		}
		s.advance()
	}
}
func (s *highlightScanner) hereDeclaration() {
	s.pos += 2
	tabs := false
	if s.at(s.pos) == '-' {
		tabs = true
		s.pos++
	}
	for s.pos < len(s.source) && (s.at(s.pos) == ' ' || s.at(s.pos) == '\t') && s.failure == highlightOK {
		s.pos++
	}
	q := s.at(s.pos)
	quoted := q == '\'' || q == '"'
	if quoted {
		s.pos++
	}
	start := s.pos
	for s.pos < len(s.source) && s.failure == highlightOK {
		c := s.at(s.pos)
		if quoted && c == q || !quoted && (shellBoundary(c) || c == '<' || c == '>') {
			break
		}
		s.advance()
	}
	end := s.pos
	if quoted && s.at(s.pos) == q {
		s.pos++
	}
	if start == end {
		return
	}
	if len(s.heredocs) >= min(64, s.limits.depth) {
		s.fail(highlightBudgetExceeded)
		return
	}
	if s.heredocs == nil {
		if 16<<10 > s.limits.bytes {
			s.fail(highlightBudgetExceeded)
			return
		}
		s.heredocs = make([]highlightHereDoc, 0, 64)
	}
	s.heredocs = append(s.heredocs, highlightHereDoc{start, end, tabs})
}
func (s *highlightScanner) hereBodies() {
	for _, doc := range s.heredocs {
		for s.pos < len(s.source) && s.failure == highlightOK {
			start := s.pos
			for s.pos < len(s.source) && s.at(s.pos) != '\n' && s.failure == highlightOK {
				s.advance()
			}
			end := s.pos
			if doc.tabs {
				for start < end && s.at(start) == '\t' && s.failure == highlightOK {
					start++
				}
			}
			equal := end-start == doc.end-doc.start
			if equal {
				for i := 0; i < end-start && s.failure == highlightOK; i++ {
					if s.at(start+i) != s.at(doc.start+i) {
						equal = false
						break
					}
				}
			}
			if s.pos < len(s.source) {
				s.pos++
			}
			if equal {
				break
			}
		}
	}
	s.heredocs = s.heredocs[:0]
}

// highlightRuneBoundary 供消费方验证源范围，不把展示偏移误作持久化偏移。
func highlightRuneBoundary(source string, offset int) bool {
	return offset == len(source) || offset >= 0 && offset < len(source) && utf8.RuneStart(source[offset])
}

// shellStyledQuote 在双引号中保留变量区间，其余字符仍属于字符串。
func (s *highlightScanner) shellStyledQuote() {
	start := s.pos
	s.pos++
	for s.pos < len(s.source) && s.failure == highlightOK {
		c := s.at(s.pos)
		if c == '"' {
			s.pos++
			break
		}
		if c == '\\' {
			s.advance()
			if s.pos < len(s.source) {
				s.advance()
			}
			continue
		}
		if c == '$' {
			s.emit(start, s.pos, highlightString)
			begin := s.pos
			kind := s.shellToken()
			s.emit(begin, s.pos, kind)
			start = s.pos
			continue
		}
		s.advance()
	}
	if s.pos > start {
		s.emit(start, s.pos, highlightString)
	}
	s.split = true
}
