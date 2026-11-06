package tui

import (
	"fmt"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"easycode/internal/domain"
)

const (
	maxCompletionCandidates = domain.MaxSkills + 5
	maxCompletionBytes      = 2 << 20
	maxCompletionLineBytes  = 16 << 10
)

type completionKind uint8

const (
	completionBuiltin completionKind = iota
	completionSkill
)

type completionIdentity struct {
	kind   completionKind
	name   string
	source domain.SkillSource
}

type completionCandidate struct {
	id                        completionIdentity
	description, hint         string
	searchDescription, origin string
}

// completionCatalog 发布后不可变；匹配只保存它的有界索引。
type completionCatalog struct {
	entries []completionCandidate
	bytes   int
}

func explicitSkillEntry(e domain.SkillListEntry) bool {
	return e.Status == "available" && e.Metadata.UserInvocable &&
		e.Name == e.Metadata.Name && e.Source == e.Metadata.Source && e.Metadata.Validate() == nil
}

func builtinCompletionCandidates() []completionCandidate {
	return []completionCandidate{
		{id: completionIdentity{kind: completionBuiltin, name: "cache"}, description: "Inspect prompt cache"},
		{id: completionIdentity{kind: completionBuiltin, name: "compact"}, description: "Compact conversation context", hint: "[instructions]"},
		{id: completionIdentity{kind: completionBuiltin, name: "context"}, description: "Inspect context budget"},
		{id: completionIdentity{kind: completionBuiltin, name: "hooks"}, description: "Browse command hooks"},
		{id: completionIdentity{kind: completionBuiltin, name: "skills"}, description: "Browse local skills"},
	}
}

func newCompletionCatalog(skills []domain.SkillListEntry) (*completionCatalog, error) {
	entries := builtinCompletionCandidates()
	bytes := 4096
	for _, e := range skills {
		// 先检查字节总量，再验证或派生字符串，避免超大来源先产生副本。
		cost := 512 + len(e.Name) + 2*len(e.Metadata.Description) + len(e.Metadata.ArgumentHint) +
			2*(len(e.Source.Scope)+len(e.Source.Kind)+len(e.Source.Directory))
		if e.Status != "available" || !e.Metadata.UserInvocable {
			continue
		}
		if cost > maxCompletionBytes-bytes {
			return nil, fmt.Errorf("completion metadata exceeds memory budget")
		}
		if !explicitSkillEntry(e) {
			continue
		}
		if len(entries) == maxCompletionCandidates {
			return nil, fmt.Errorf("completion catalog exceeds item limit")
		}
		entries = append(entries, completionCandidate{
			id:          completionIdentity{kind: completionSkill, name: e.Name, source: e.Source},
			description: e.Metadata.Description, hint: e.Metadata.ArgumentHint, origin: e.Source.ID(),
		})
		bytes += cost
	}
	sort.Slice(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]
		if a.id.kind != b.id.kind {
			return a.id.kind < b.id.kind
		}
		if a.id.name != b.id.name {
			return a.id.name < b.id.name
		}
		return a.origin < b.origin
	})
	for i := range entries {
		if i > 0 && entries[i].id.kind == completionSkill && entries[i-1].id.kind == completionSkill && entries[i].id.name == entries[i-1].id.name {
			return nil, fmt.Errorf("completion catalog has ambiguous skill names")
		}
		entries[i].searchDescription = strings.ToLower(entries[i].description)
	}
	return &completionCatalog{entries: entries, bytes: bytes}, nil
}

type completionTarget struct {
	start, end int
	sigil      byte
	token      string
	query      string
}

func findCompletionTarget(text string, cursor int) (completionTarget, bool) {
	if cursor < 0 || cursor > len(text) {
		return completionTarget{}, false
	}
	start := len(text) - len(strings.TrimLeftFunc(text, unicode.IsSpace))
	if start == len(text) || (text[start] != '/' && text[start] != '$') || cursor <= start {
		return completionTarget{}, false
	}
	end := start + 1
	for end < len(text) {
		b := text[end]
		if b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || b == '-' {
			end++
			if end-start > 65 {
				return completionTarget{}, false
			}
			continue
		}
		r, _ := utf8.DecodeRuneInString(text[end:])
		if !unicode.IsSpace(r) {
			return completionTarget{}, false
		}
		break
	}
	if cursor > end {
		return completionTarget{}, false
	}
	return completionTarget{start: start, end: end, sigil: text[start],
		token: strings.Clone(text[start:end]), query: strings.ToLower(strings.Clone(text[start+1 : cursor]))}, true
}

type completionMatch struct{ index, tier int }

func completionMatchTier(c completionCandidate, query string) int {
	switch {
	case c.id.name == query || query == "":
		return 0
	case strings.HasPrefix(c.id.name, query):
		return 1
	}
	pos := 0
	for i := 0; i < len(c.id.name) && pos < len(query); i++ {
		if c.id.name[i] == query[pos] {
			pos++
		}
	}
	if pos == len(query) {
		return 2
	}
	if strings.Contains(c.searchDescription, query) {
		return 3
	}
	return -1
}

func (c *completionCatalog) match(target completionTarget) []completionMatch {
	if c == nil {
		return nil
	}
	matches := make([]completionMatch, 0, len(c.entries))
	for i, candidate := range c.entries {
		if target.sigil == '$' && candidate.id.kind != completionSkill {
			continue
		}
		if target.sigil == '/' && candidate.id.kind == completionSkill && isBuiltinCompletionName(candidate.id.name) {
			continue
		}
		if tier := completionMatchTier(candidate, target.query); tier >= 0 {
			matches = append(matches, completionMatch{i, tier})
		}
	}
	// 目录已经按类别、名称、来源排序，稳定排序只需比较命中等级。
	sort.SliceStable(matches, func(i, j int) bool { return matches[i].tier < matches[j].tier })
	return matches
}

func isBuiltinCompletionName(name string) bool {
	switch name {
	case "cache", "compact", "context", "hooks", "skills":
		return true
	}
	return false
}

func (c *composer) complete(target completionTarget, candidate completionCandidate) error {
	inserted := string(target.sigil) + candidate.id.name
	hasSeparator := target.end < len(c.text)
	if !hasSeparator {
		inserted += " "
	}
	if err := c.replace(target.start, target.end, inserted); err != nil {
		return err
	}
	if hasSeparator {
		// 空白可与后续组合字符形成一个字素，不能只跳过首个 rune。
		c.horizontal(1)
	}
	return nil
}
