package codec

import (
	"bytes"
	"encoding/json"
	"errors"
	"unicode/utf8"
)

// jsonValue 仅在一次校验调用内引用输入，公开返回值另行复制，避免逐层物化同一子树。
type jsonValue struct {
	raw      []byte
	members  map[string]jsonValue
	elements []jsonValue
}

// scanJSON 由标准库校验完整语法，再一次遍历检查无损语义并索引字段范围。
func scanJSON(data []byte, maxDepth int, rejectNull bool) (jsonValue, error) {
	return scanJSONFields(data, maxDepth, rejectNull, -1)
}

// indexDepth 限制保留的字段索引深度；零表示只校验，负数表示严格 typed 形状所需的全部层级。
func scanJSONFields(data []byte, maxDepth int, rejectNull bool, indexDepth int) (jsonValue, error) {
	if !utf8.Valid(data) || !losslessJSONUnicode(data) {
		return jsonValue{}, errors.New("invalid JSON Unicode")
	}
	if !validJSONSyntax(data) {
		return jsonValue{}, errors.New("invalid JSON syntax or trailing content")
	}
	scanner := jsonScanner{data: data, maxDepth: maxDepth, rejectNull: rejectNull, indexDepth: indexDepth}
	return scanner.value(0)
}

// validJSONSyntax 避免常规输入逐 token 分配；深度超过标准快速检查上限时保留既有流式语义。
func validJSONSyntax(data []byte) bool {
	if json.Valid(data) {
		return true
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	depth := 0
	for {
		token, err := decoder.Token()
		if err != nil {
			return false
		}
		if delimiter, ok := token.(json.Delim); ok {
			switch delimiter {
			case '{', '[':
				depth++
			case '}', ']':
				depth--
			}
		}
		if depth == 0 {
			return requireJSONEOF(decoder) == nil
		}
	}
}

type jsonScanner struct {
	data       []byte
	position   int
	maxDepth   int
	rejectNull bool
	indexDepth int
}

func (s *jsonScanner) space() {
	for s.position < len(s.data) {
		switch s.data[s.position] {
		case ' ', '\t', '\r', '\n':
			s.position++
		default:
			return
		}
	}
}

// stringEnd 的输入已经过完整语法校验，IndexByte 跳过大段普通文本。
func (s *jsonScanner) stringEnd() {
	s.position++
	for {
		end := s.position + bytes.IndexByte(s.data[s.position:], '"')
		back := end
		for back > 0 && s.data[back-1] == '\\' {
			back--
		}
		s.position = end + 1
		if (end-back)%2 == 0 {
			return
		}
	}
}

func (s *jsonScanner) value(depth int) (jsonValue, error) {
	s.space()
	start := s.position
	var value jsonValue
	kind := s.data[start]
	if kind == '{' || kind == '[' {
		depth++
		if s.maxDepth > 0 && depth > s.maxDepth {
			return jsonValue{}, errors.New("JSON exceeds depth limit")
		}
	}
	indexed := s.indexDepth < 0 || depth <= s.indexDepth
	switch kind {
	case '{':
		s.position++
		s.space()
		var seen map[string]bool
		if indexed {
			value.members = make(map[string]jsonValue)
		} else {
			seen = make(map[string]bool)
		}
		for s.data[s.position] != '}' {
			keyStart := s.position
			s.stringEnd()
			key := s.data[keyStart:s.position]
			name := string(key[1 : len(key)-1])
			if bytes.IndexByte(key, '\\') >= 0 {
				// 仅转义字段名需要反转义；值文本保持切片，不进入临时通用对象。
				if err := json.Unmarshal(key, &name); err != nil {
					return jsonValue{}, err
				}
			}
			if _, exists := value.members[name]; exists || seen[name] {
				return jsonValue{}, errors.New("duplicate or invalid JSON field")
			}
			s.space()
			s.position++
			child, err := s.value(depth)
			if err != nil {
				return jsonValue{}, err
			}
			if indexed {
				value.members[name] = child
			} else {
				seen[name] = true
			}
			s.space()
			if s.data[s.position] == ',' {
				s.position++
				s.space()
			}
		}
		s.position++
	case '[':
		s.position++
		s.space()
		for s.data[s.position] != ']' {
			child, err := s.value(depth)
			if err != nil {
				return jsonValue{}, err
			}
			if indexed {
				value.elements = append(value.elements, child)
			}
			s.space()
			if s.data[s.position] == ',' {
				s.position++
			}
		}
		s.position++
	case '"':
		s.stringEnd()
	default:
		for s.position < len(s.data) && !jsonValueEnd(s.data[s.position]) {
			s.position++
		}
		if kind == 'n' && s.rejectNull {
			return jsonValue{}, errors.New("JSON null is not supported")
		}
	}
	value.raw = s.data[start:s.position]
	return value, nil
}

func jsonValueEnd(b byte) bool {
	switch b {
	case ',', '}', ']', ' ', '\t', '\r', '\n':
		return true
	default:
		return false
	}
}
