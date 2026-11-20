package app

import (
	"context"
	"errors"

	"easycode/internal/domain"
)

var errFileMatchingLimit = errors.New("file matching work limit exceeded")

type fileMatchBudget struct{ remaining, spent int }

func (b *fileMatchBudget) spend(ctx context.Context, n int) error {
	if n > b.remaining {
		return errFileMatchingLimit
	}
	b.remaining -= n
	b.spent += n
	if b.spent%128 < n {
		return ctx.Err()
	}
	return nil
}

type rankedFilePath struct {
	path string
	tier int
}
type fileMatcher struct {
	identity domain.FileSearchIdentity
	query    []rune
	prefix   []int
	next     int
	best     []rankedFilePath
	budget   fileMatchBudget
	limited  bool
}

func foldFileRune(r rune) rune {
	if r >= 'A' && r <= 'Z' {
		return r + 'a' - 'A'
	}
	return r
}

func newFileMatcher(q domain.FileSearchQuery) *fileMatcher {
	query := []rune(q.Query())
	for i, r := range query {
		query[i] = foldFileRune(r)
	}
	m := &fileMatcher{identity: q.Identity(), query: query, prefix: make([]int, len(query)), best: make([]rankedFilePath, 0, domain.MaxFileSearchResults), budget: fileMatchBudget{remaining: domain.MaxFileSearchWork}}
	for i, j := 1, 0; i < len(query); i++ {
		for j > 0 && query[i] != query[j] {
			j = m.prefix[j-1]
			m.budget.spent++
			m.budget.remaining--
		}
		if query[i] == query[j] {
			j++
		}
		m.prefix[i] = j
		m.budget.spent++
		m.budget.remaining--
	}
	return m
}

func fileRunePrefix(ctx context.Context, text, query []rune, budget *fileMatchBudget) (bool, error) {
	if len(query) > len(text) {
		return false, nil
	}
	for i, r := range query {
		if err := budget.spend(ctx, 1); err != nil {
			return false, err
		}
		if text[i] != r {
			return false, nil
		}
	}
	return true, nil
}

func (m *fileMatcher) tier(ctx context.Context, path string) (int, error) {
	if err := ctx.Err(); err != nil {
		return -1, err
	}
	if len(m.query) == 0 {
		return 1, nil
	}
	if err := m.budget.spend(ctx, len(path)); err != nil {
		return -1, err
	}
	text := make([]rune, 0, len(path))
	base := 0
	for _, r := range path {
		text = append(text, foldFileRune(r))
		if r == '/' {
			base = len(text)
		}
	}
	matched, err := fileRunePrefix(ctx, text, m.query, &m.budget)
	if err != nil {
		return -1, err
	}
	if matched {
		if len(text) == len(m.query) {
			return 0, nil
		}
		return 1, nil
	}
	matched, err = fileRunePrefix(ctx, text[base:], m.query, &m.budget)
	if err != nil {
		return -1, err
	}
	if matched {
		return 2, nil
	}
	// KMP 失败跳转也逐次计费，不将内层回退藏在一次候选计数中。
	for i, j := 0, 0; i < len(text); i++ {
		for j > 0 && text[i] != m.query[j] {
			if err := m.budget.spend(ctx, 1); err != nil {
				return -1, err
			}
			j = m.prefix[j-1]
		}
		if err := m.budget.spend(ctx, 1); err != nil {
			return -1, err
		}
		if text[i] == m.query[j] {
			j++
		}
		if j == len(m.query) {
			return 3, nil
		}
	}
	position := 0
	for _, r := range text {
		if err := m.budget.spend(ctx, 1); err != nil {
			return -1, err
		}
		if r == m.query[position] {
			position++
			if position == len(m.query) {
				return 4, nil
			}
		}
	}
	return -1, nil
}

func compareRankedFiles(ctx context.Context, left, right rankedFilePath, budget *fileMatchBudget) (int, error) {
	if err := budget.spend(ctx, 1); err != nil {
		return 0, err
	}
	if left.tier < right.tier {
		return -1, nil
	}
	if left.tier > right.tier {
		return 1, nil
	}
	for i := 0; i < min(len(left.path), len(right.path)); i++ {
		if err := budget.spend(ctx, 1); err != nil {
			return 0, err
		}
		if left.path[i] < right.path[i] {
			return -1, nil
		}
		if left.path[i] > right.path[i] {
			return 1, nil
		}
	}
	if len(left.path) < len(right.path) {
		return -1, nil
	}
	if len(left.path) > len(right.path) {
		return 1, nil
	}
	return 0, nil
}

func (m *fileMatcher) insert(ctx context.Context, value rankedFilePath) error {
	at := len(m.best)
	for i, previous := range m.best {
		order, err := compareRankedFiles(ctx, value, previous, &m.budget)
		if err != nil {
			return err
		}
		if order < 0 {
			at = i
			break
		}
	}
	if at == domain.MaxFileSearchResults {
		return nil
	}
	if len(m.best) == domain.MaxFileSearchResults {
		m.best = m.best[:domain.MaxFileSearchResults-1]
	}
	m.best = append(m.best, rankedFilePath{})
	copy(m.best[at+1:], m.best[at:])
	m.best[at] = value
	return nil
}

func (m *fileMatcher) advance(ctx context.Context, index []string) error {
	if m.limited {
		return nil
	}
	for m.next < len(index) {
		tier, err := m.tier(ctx, index[m.next])
		if err == nil && tier >= 0 {
			err = m.insert(ctx, rankedFilePath{index[m.next], tier})
		}
		if errors.Is(err, errFileMatchingLimit) {
			m.limited = true
			return nil
		}
		if err != nil {
			return err
		}
		m.next++
	}
	return nil
}

func (m *fileMatcher) paths() []string {
	paths := make([]string, 0, len(m.best))
	for _, item := range m.best {
		paths = append(paths, item.path)
	}
	return domain.FileSearchResultPrefix(m.identity.Target.Query, paths)
}
