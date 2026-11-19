package domain

import (
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"
)

const maxSessionTitleBytes = 256

// SessionTitle 表示规范化的显式会话标题，零值只用于投影未命名会话。
type SessionTitle string

// ParseSessionTitle 校验原文后裁去两端的 Unicode 空格分隔符。
func ParseSessionTitle(text string) (SessionTitle, error) {
	title, err := normalizeSessionTitle(text)
	return SessionTitle(strings.Clone(title)), err
}

func normalizeSessionTitle(text string) (string, error) {
	if err := ValidateSessionTitleText(text); err != nil {
		return "", err
	}
	title := strings.TrimFunc(text, func(character rune) bool {
		return unicode.Is(unicode.Zs, character)
	})
	if title == "" {
		return "", errors.New("session title must not be empty")
	}
	if len(title) > maxSessionTitleBytes {
		return "", errors.New("session title exceeds 256 UTF-8 bytes")
	}
	return title, nil
}

// ValidateSessionTitleText 校验原始文本编码和码点，允许尚未完成的编辑内容。
func ValidateSessionTitleText(text string) error {
	if !utf8.ValidString(text) {
		return errors.New("session title must be valid UTF-8")
	}
	for _, character := range text {
		if unicode.IsControl(character) || unicode.Is(unicode.Bidi_Control, character) ||
			character == '\u2028' || character == '\u2029' {
			return errors.New("session title contains a prohibited character")
		}
	}
	return nil
}

// Validate 要求标题已经规范化，且能作为已确认标题保存。
func (title SessionTitle) Validate() error {
	parsed, err := normalizeSessionTitle(string(title))
	if err != nil {
		return err
	}
	if parsed != string(title) {
		return errors.New("session title is not normalized")
	}
	return nil
}
