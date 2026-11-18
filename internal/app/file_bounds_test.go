package app

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"easycode/internal/domain"
)

func TestFileIndexAndMatcherActualEntryByteStateAndResultBounds(t *testing.T) {
	paths := make([]string, domain.MaxFileIndexEntries+1)
	for i := range paths {
		paths[i] = fmt.Sprintf("p/%05d", i)
	}
	index := &fileSearchSession{seen: make(map[string]struct{})}
	if index.appendPaths(paths) || len(index.index) != 50000 || index.indexBytes > 16<<20 || index.revision != 1 {
		t.Fatal("entry index bound failed", len(index.index), index.indexBytes)
	}
	index = &fileSearchSession{seen: make(map[string]struct{})}
	long := make([]string, 4100)
	for i := range long {
		long[i] = fmt.Sprintf("%04d/", i) + strings.Repeat("x", 4091)
	}
	if index.appendPaths(long) || index.indexBytes > domain.MaxFileIndexBytes || len(index.index) >= len(long) {
		t.Fatal("byte index bound failed")
	}
	query := fileQuery(t, "x", 1, 1)
	matcher := newFileMatcher(query)
	if err := matcher.advance(context.Background(), index.index); err != nil {
		t.Fatal(err)
	}
	if !matcher.limited || matcher.budget.spent > 16000000 || matcher.next >= len(index.index) {
		t.Fatal("actual matching states escaped budget", matcher.budget.spent, matcher.next)
	}
	results := matcher.paths()
	if len(results) == 0 || len(results) > 50 {
		t.Fatal("result limit failed", len(results))
	}
	snapshot, err := domain.NewFileSearchSnapshot(query.Identity(), index.revision, results, domain.FileSearchIncomplete, domain.FileDiscoveryReport{Issues: domain.FileSearchWorkLimit})
	if err != nil {
		t.Fatal(err)
	}
	bytes := 256 + len(query.Query())
	for _, path := range snapshot.Paths() {
		bytes += len(path) + 16
		if len(path) != 4096 {
			t.Fatal("result path was truncated")
		}
	}
	if bytes > 128<<10 {
		t.Fatal("result bytes escaped budget")
	}
	before := matcher.budget.spent
	_ = matcher.advance(context.Background(), index.index)
	if matcher.budget.spent != before {
		t.Fatal("exhausted matcher restarted work")
	}
}

func BenchmarkFileMatcherLargeIndex(b *testing.B) {
	paths := make([]string, 50000)
	for i := range paths {
		paths[i] = fmt.Sprintf("src/module-%04d/file-%05d.go", i%1000, i)
	}
	id := domain.FileSearchIdentity{WorkspaceGeneration: 1, SessionGeneration: 1, RequestGeneration: 1, Target: domain.FileMentionTarget{PathStart: 1, PathEnd: 5, End: 5, Query: "file"}}
	query, err := domain.NewFileSearchQuery(id)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m := newFileMatcher(query)
		if err := m.advance(context.Background(), paths); err != nil {
			b.Fatal(err)
		}
		if len(m.paths()) != 50 || m.budget.spent > 16000000 {
			b.Fatal("unbounded matching")
		}
		b.ReportMetric(float64(m.budget.spent), "states/op")
	}
}
