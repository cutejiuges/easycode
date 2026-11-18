package builtin

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"
)

var errFileDiscoveryWorkLimit = errors.New("file discovery work limit exceeded")

type fileWorkBudget struct{ remaining, spent int }

func (b *fileWorkBudget) spend(ctx context.Context, count int) error {
	if count > b.remaining {
		return errFileDiscoveryWorkLimit
	}
	b.remaining -= count
	b.spent += count
	if b.spent%128 < count {
		return ctx.Err()
	}
	return nil
}

type ignoreTokenKind uint8

const (
	ignoreLiteral ignoreTokenKind = iota
	ignoreAny
	ignoreStar
	ignoreGlobstar
	ignoreDirectories
	ignoreClass
)

type ignoreRuneRange struct{ first, last rune }
type ignoreToken struct {
	kind    ignoreTokenKind
	literal rune
	ranges  []ignoreRuneRange
	classes []string
	negate  bool
}
type fileIgnoreRule struct {
	scope                           string
	tokens                          []ignoreToken
	negative, directories, anchored bool
}

func trimIgnoreTrailingSpaces(pattern string) string {
	for strings.HasSuffix(pattern, " ") {
		backslashes := 0
		for i := len(pattern) - 2; i >= 0 && pattern[i] == '\\'; i-- {
			backslashes++
		}
		if backslashes%2 == 1 {
			break
		}
		pattern = pattern[:len(pattern)-1]
	}
	return pattern
}

func parseIgnoreClass(pattern []rune, at int) (ignoreToken, int, error) {
	token := ignoreToken{kind: ignoreClass}
	i := at + 1
	if i < len(pattern) && (pattern[i] == '!' || pattern[i] == '^') {
		token.negate = true
		i++
	}
	first := true
	read := func() (rune, error) {
		if i >= len(pattern) {
			return 0, errors.New("invalid ignore class")
		}
		r := pattern[i]
		i++
		if r == '\\' {
			if i >= len(pattern) {
				return 0, errors.New("invalid ignore class")
			}
			r = pattern[i]
			i++
		}
		return r, nil
	}
	for i < len(pattern) {
		if pattern[i] == ']' && !first {
			return token, i + 1, nil
		}
		if i+1 < len(pattern) && pattern[i] == '[' && pattern[i+1] == ':' {
			end := i + 2
			for end+1 < len(pattern) && (pattern[end] != ':' || pattern[end+1] != ']') {
				end++
			}
			if end+1 >= len(pattern) {
				return token, 0, errors.New("invalid ignore character class")
			}
			name := string(pattern[i+2 : end])
			if _, valid := matchIgnoreCharacterClass(name, 0); !valid {
				return token, 0, errors.New("invalid ignore character class")
			}
			token.classes = append(token.classes, name)
			i = end + 2
			first = false
			continue
		}
		lo, err := read()
		if err != nil {
			return token, 0, err
		}
		first = false
		hi := lo
		if i+1 < len(pattern) && pattern[i] == '-' && pattern[i+1] != ']' {
			i++
			hi, err = read()
			if err != nil || hi < lo {
				return token, 0, errors.New("invalid ignore class range")
			}
		}
		token.ranges = append(token.ranges, ignoreRuneRange{lo, hi})
	}
	return token, 0, errors.New("unterminated ignore class")
}

func matchIgnoreCharacterClass(name string, r rune) (bool, bool) {
	switch name {
	case "alnum":
		return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9', true
	case "alpha":
		return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z', true
	case "blank":
		return r == ' ' || r == '\t', true
	case "cntrl":
		return r < 0x20 || r == 0x7f, true
	case "digit":
		return r >= '0' && r <= '9', true
	case "graph":
		return r >= 0x21 && r <= 0x7e, true
	case "lower":
		return r >= 'a' && r <= 'z', true
	case "print":
		return r >= 0x20 && r <= 0x7e, true
	case "punct":
		return r >= 0x21 && r <= 0x7e && !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9'), true
	case "space":
		return r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '\v' || r == '\f', true
	case "upper":
		return r >= 'A' && r <= 'Z', true
	case "xdigit":
		return r >= '0' && r <= '9' || r >= 'a' && r <= 'f' || r >= 'A' && r <= 'F', true
	default:
		return false, false
	}
}

func compileIgnoreRule(scope, line string) (fileIgnoreRule, bool, error) {
	line = strings.TrimSuffix(line, "\r")
	line = trimIgnoreTrailingSpaces(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return fileIgnoreRule{}, false, nil
	}
	if len(line) > 4096 || !utf8.ValidString(line) || strings.IndexByte(line, 0) >= 0 {
		return fileIgnoreRule{}, false, errors.New("invalid ignore pattern")
	}
	rule := fileIgnoreRule{scope: scope}
	if line[0] == '!' {
		rule.negative = true
		line = line[1:]
	}
	if strings.HasPrefix(line, "/") {
		rule.anchored = true
		line = line[1:]
	}
	if strings.HasSuffix(line, "/") {
		rule.directories = true
		line = line[:len(line)-1]
	}
	if line == "" {
		return fileIgnoreRule{}, false, errors.New("empty ignore pattern")
	}
	rule.anchored = rule.anchored || strings.Contains(line, "/")
	pattern := []rune(line)
	for i := 0; i < len(pattern); {
		r := pattern[i]
		switch r {
		case '\\':
			if i+1 == len(pattern) {
				return fileIgnoreRule{}, false, errors.New("invalid ignore escape")
			}
			rule.tokens = append(rule.tokens, ignoreToken{kind: ignoreLiteral, literal: pattern[i+1]})
			i += 2
		case '?':
			rule.tokens = append(rule.tokens, ignoreToken{kind: ignoreAny})
			i++
		case '*':
			j := i
			for j < len(pattern) && pattern[j] == '*' {
				j++
			}
			kind := ignoreStar
			if j-i == 2 && (i == 0 || pattern[i-1] == '/') && (j == len(pattern) || pattern[j] == '/') {
				kind = ignoreGlobstar
				if j < len(pattern) {
					kind = ignoreDirectories
					j++
				}
			}
			rule.tokens = append(rule.tokens, ignoreToken{kind: kind})
			i = j
		case '[':
			token, next, err := parseIgnoreClass(pattern, i)
			if err != nil {
				return fileIgnoreRule{}, false, err
			}
			rule.tokens = append(rule.tokens, token)
			i = next
		default:
			rule.tokens = append(rule.tokens, ignoreToken{kind: ignoreLiteral, literal: r})
			i++
		}
	}
	return rule, true, nil
}

// matchIgnoreTokens 使用有界 NFA，没有递归回溯；字符类和 epsilon 推进同样计费。
func matchIgnoreTokens(ctx context.Context, tokens []ignoreToken, text string, budget *fileWorkBudget) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	current, next := make([]int, 0, len(tokens)+1), make([]int, 0, len(tokens)+1)
	seen := make([]uint32, len(tokens)+1)
	epoch := uint32(1)
	add := func(state int, boundary bool, dst *[]int) error {
		for {
			if err := budget.spend(ctx, 1); err != nil {
				return err
			}
			if seen[state] == epoch {
				return nil
			}
			seen[state] = epoch
			*dst = append(*dst, state)
			if state == len(tokens) {
				return nil
			}
			kind := tokens[state].kind
			if kind != ignoreStar && kind != ignoreGlobstar && (kind != ignoreDirectories || !boundary) {
				return nil
			}
			state++
		}
	}
	if err := add(0, true, &current); err != nil {
		return false, err
	}
	for _, r := range text {
		epoch++
		next = next[:0]
		for _, state := range current {
			if err := budget.spend(ctx, 1); err != nil {
				return false, err
			}
			if state == len(tokens) {
				continue
			}
			token := tokens[state]
			matched := false
			destination := state + 1
			switch token.kind {
			case ignoreLiteral:
				matched = r == token.literal
			case ignoreAny:
				matched = r != '/'
			case ignoreStar:
				matched = r != '/'
				destination = state
			case ignoreGlobstar, ignoreDirectories:
				matched = true
				destination = state
			case ignoreClass:
				if r != '/' {
					for _, span := range token.ranges {
						if err := budget.spend(ctx, 1); err != nil {
							return false, err
						}
						matched = matched || r >= span.first && r <= span.last
					}
					for _, class := range token.classes {
						if err := budget.spend(ctx, 1); err != nil {
							return false, err
						}
						member, _ := matchIgnoreCharacterClass(class, r)
						matched = matched || member
					}
					if token.negate {
						matched = !matched
					}
				}
			}
			if matched {
				if err := add(destination, r == '/', &next); err != nil {
					return false, err
				}
			}
		}
		current, next = next, current
		if len(current) == 0 {
			return false, nil
		}
	}
	for _, state := range current {
		if state == len(tokens) {
			return true, nil
		}
	}
	return false, ctx.Err()
}

func matchFileIgnore(ctx context.Context, rules []fileIgnoreRule, path string, directory bool, budget *fileWorkBudget) (bool, error) {
	ignored := false
	for _, rule := range rules {
		if err := budget.spend(ctx, 1); err != nil {
			return false, err
		}
		if rule.directories && !directory {
			continue
		}
		subject := path
		if rule.scope != "" {
			prefix := rule.scope + "/"
			if !strings.HasPrefix(subject, prefix) {
				continue
			}
			subject = strings.TrimPrefix(subject, prefix)
		}
		if !rule.anchored {
			if at := strings.LastIndexByte(subject, '/'); at >= 0 {
				subject = subject[at+1:]
			}
		}
		matched, err := matchIgnoreTokens(ctx, rule.tokens, subject, budget)
		if err != nil {
			return false, err
		}
		if matched {
			ignored = !rule.negative
		}
	}
	return ignored, nil
}
