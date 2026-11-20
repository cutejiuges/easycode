package domain

import (
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"easycode/internal/codec"
)

type fileMentionToken struct {
	start, pathStart, pathEnd, end int
	quoted, closed                 bool
}

// IsLocalSlashInput 复用唯一保留名判断，只识别精确的首词。
func IsLocalSlashInput(text string) bool {
	head, _ := SplitInputHead(text)
	return strings.HasPrefix(head, "/") && IsReservedSkillSlashName(strings.TrimPrefix(head, "/"))
}

// walkFileMentions 单次扫描用户原文，访问者可提前停止；不展开任何来源。
func walkFileMentions(text string, visit func(fileMentionToken) bool) {
	fence := byte(0)
	fenceSize := 0
	for i := 0; i < len(text); {
		if i == 0 || text[i-1] == '\n' {
			lineEnd := strings.IndexByte(text[i:], '\n')
			if lineEnd < 0 {
				lineEnd = len(text) - i
			}
			lineEnd += i
			j := i
			for j < lineEnd && j-i < 4 && text[j] == ' ' {
				j++
			}
			if j < lineEnd && (text[j] == '`' || text[j] == '~') {
				k := j
				for k < lineEnd && text[k] == text[j] {
					k++
				}
				if k-j >= 3 && (fence == 0 || fence == text[j] && k-j >= fenceSize && strings.TrimSpace(text[k:lineEnd]) == "") {
					if fence == 0 {
						fence, fenceSize = text[j], k-j
					} else {
						fence, fenceSize = 0, 0
					}
					i = min(len(text), lineEnd+1)
					continue
				}
			}
			if fence != 0 {
				i = min(len(text), lineEnd+1)
				continue
			}
		}
		if text[i] == '`' {
			j := i
			for j < len(text) && text[j] == '`' {
				j++
			}
			marker := text[i:j]
			found := false
			for k := j; k < len(text); {
				if text[k] != '`' {
					_, n := utf8.DecodeRuneInString(text[k:])
					k += n
					continue
				}
				end := k
				for end < len(text) && text[end] == '`' {
					end++
				}
				if end-k == len(marker) {
					i, found = end, true
					break
				}
				k = end
			}
			if !found {
				return
			}
			continue
		}
		if text[i] != '@' {
			_, n := utf8.DecodeRuneInString(text[i:])
			i += n
			continue
		}
		boundary := i == 0
		if i > 0 {
			previous, _ := utf8.DecodeLastRuneInString(text[:i])
			boundary = unicode.IsSpace(previous)
		}
		if !boundary || i+1 < len(text) && text[i+1] == '@' {
			i++
			continue
		}
		token := fileMentionToken{start: i, pathStart: i + 1}
		j := token.pathStart
		if j < len(text) && text[j] == '"' {
			token.quoted = true
			j++
			for j < len(text) && text[j] != '\n' && text[j] != '\r' {
				if text[j] == '\\' {
					j = min(len(text), j+2)
					continue
				}
				if text[j] == '"' {
					j++
					token.closed = true
					break
				}
				j++
			}
		}
		if !token.quoted {
			for j < len(text) {
				c, n := utf8.DecodeRuneInString(text[j:])
				if unicode.IsSpace(c) || c == '#' {
					break
				}
				j += n
			}
			token.closed = true
		}
		token.pathEnd = j
		for j < len(text) {
			c, n := utf8.DecodeRuneInString(text[j:])
			if unicode.IsSpace(c) {
				break
			}
			j += n
		}
		token.end = j
		if !visit(token) {
			return
		}
		i = max(i+1, j)
	}
}

func decodeMentionPath(raw string, quoted bool) (string, error) {
	if len(raw) > MaxFileEditBytes {
		return "", NewFileFailure(FileLimit)
	}
	if !quoted {
		return raw, nil
	}
	var path string
	if codec.ValidateJSON([]byte(raw)) != nil || codec.Unmarshal([]byte(raw), &path) != nil {
		return "", NewFileFailure(FileInvalid)
	}
	return path, nil
}

func parseMentionSelection(raw string) (FileSelection, error) {
	if raw == "" {
		return NewWholeFileSelection(), nil
	}
	if !strings.HasPrefix(raw, "#L") {
		return FileSelection{}, NewFileFailure(FileInvalid)
	}
	first, last, ranged := strings.Cut(raw[2:], "-")
	for _, value := range []string{first, last} {
		for _, c := range value {
			if c < '0' || c > '9' {
				return FileSelection{}, NewFileFailure(FileInvalid)
			}
		}
	}
	start, err := strconv.ParseUint(first, 10, 64)
	if err != nil {
		return FileSelection{}, NewFileFailure(FileInvalid)
	}
	end := start
	if ranged {
		end, err = strconv.ParseUint(last, 10, 64)
		if err != nil {
			return FileSelection{}, NewFileFailure(FileInvalid)
		}
	}
	return NewLineFileSelection(start, end)
}

// ParseFileReferences 在任意来源读取前校验全部显式引用，按首次出现去重。
func ParseFileReferences(text string) ([]FileReference, error) {
	if ValidateUserText(text) != nil {
		return nil, NewFileFailure(FileInvalid)
	}
	if IsLocalSlashInput(text) {
		return []FileReference{}, nil
	}
	refs := make([]FileReference, 0, MaxFileReferences)
	var parseErr error
	walkFileMentions(text, func(t fileMentionToken) bool {
		if !t.closed {
			parseErr = NewFileFailure(FileInvalid)
			return false
		}
		path, err := decodeMentionPath(text[t.pathStart:t.pathEnd], t.quoted)
		if err != nil {
			parseErr = err
			return false
		}
		selection, err := parseMentionSelection(text[t.pathEnd:t.end])
		if err != nil {
			parseErr = err
			return false
		}
		ref, err := NewFileReference(path, selection)
		if err != nil {
			parseErr = err
			return false
		}
		if !slices.Contains(refs, ref) {
			if len(refs) == MaxFileReferences {
				parseErr = NewFileFailure(FileLimit)
				return false
			}
			refs = append(refs, ref)
		}
		return true
	})
	if parseErr != nil {
		return nil, parseErr
	}
	return refs, nil
}

// FileMentionTarget 保存真实字节编辑范围，显示标签不得替代其中的路径。
type FileMentionTarget struct {
	Start, PathStart, PathEnd, End int
	Query                          string
}

// FindFileMentionTarget 和正式解析共用词法扫描，仅允许路径区而非行范围。
func FindFileMentionTarget(text string, cursor int) (FileMentionTarget, bool) {
	if len(text) > MaxUserInputBytes || !utf8.ValidString(text) || cursor < 0 || cursor > len(text) || cursor < len(text) && !utf8.RuneStart(text[cursor]) || IsLocalSlashInput(text) {
		return FileMentionTarget{}, false
	}
	var target FileMentionTarget
	found := false
	walkFileMentions(text, func(t fileMentionToken) bool {
		if cursor <= t.start || cursor > t.pathEnd {
			return true
		}
		prefix := text[t.pathStart:cursor]
		if len(prefix) > MaxFileEditBytes {
			return false
		}
		if t.quoted {
			if cursor == t.pathEnd && t.closed {
				prefix = prefix[:len(prefix)-1]
			}
			prefix += "\""
		}
		query, err := decodeMentionPath(prefix, t.quoted)
		if err != nil || len(query) > MaxFilePathBytes {
			return false
		}
		for strings.HasPrefix(query, "./") {
			query = strings.TrimPrefix(query, "./")
		}
		if query != "" && query != "." {
			if _, err := NewFileReference(strings.TrimSuffix(query, "/"), NewWholeFileSelection()); err != nil {
				return false
			}
		}
		target = FileMentionTarget{t.start, t.pathStart, t.pathEnd, t.end, strings.Clone(query)}
		found = true
		return false
	})
	return target, found
}

// InputDraft 区分普通文本与明确交互文件意图；内容准备之后才能接纳。
type InputDraft struct {
	text        string
	references  []FileReference
	interactive bool
}

// NewLiteralInputDraft 保持 headless 文本含义，不激活其中的 @。
func NewLiteralInputDraft(text string) (InputDraft, error) {
	if err := ValidateUserText(text); err != nil {
		return InputDraft{}, err
	}
	return InputDraft{text: text}, nil
}

// NewInteractiveInputDraft 仅解析真实原文，不读取文件或执行命令。
func NewInteractiveInputDraft(text string) (InputDraft, error) {
	refs, err := ParseFileReferences(text)
	if err != nil {
		return InputDraft{}, err
	}
	return InputDraft{text: text, references: refs, interactive: true}, nil
}

func (d InputDraft) Text() string                    { return d.text }
func (d InputDraft) FileReferences() []FileReference { return slices.Clone(d.references) }
func (d InputDraft) Interactive() bool               { return d.interactive }

// ValidateFiles 核对附件与已解析引用的顺序。文本入口仍须独立校验草稿。
func (d InputDraft) ValidateFiles(files FileAttachments) error {
	if !d.interactive {
		return ValidateFileAttachments(d.text, files)
	}
	if err := files.Validate(); err != nil || files.Len() == 0 {
		return err
	}
	return matchFileReferences(d.references, files)
}

// Validate 不信任外部携带的引用列表，重新核对完整有序原文集合。
func (d InputDraft) Validate() error {
	if err := ValidateUserText(d.text); err != nil {
		return err
	}
	if !d.interactive {
		if len(d.references) != 0 {
			return NewFileFailure(FileInvalid)
		}
		return nil
	}
	refs, err := ParseFileReferences(d.text)
	if err != nil {
		return err
	}
	if !slices.Equal(refs, d.references) {
		return NewFileFailure(FileInvalid)
	}
	return nil
}
