//go:build darwin || linux

package catalog

import (
	"context"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"easycode/internal/domain"
)

func TestListValidatesBeforeStorage(t *testing.T) {
	for _, query := range []ListQuery{
		{Scope: 99, Limit: 1}, {Scope: ListCurrentDirectory, Limit: 1},
		{Scope: ListCurrentDirectory, CreationCWD: "relative", Limit: 1},
		{Scope: ListCurrentDirectory, CreationCWD: "/a/../b", Limit: 1},
		{Scope: ListAllDirectories, CreationCWD: "/a", Limit: 1},
		{Scope: ListAllDirectories, Limit: 0}, {Scope: ListAllDirectories, Limit: -1},
		{Scope: ListAllDirectories, Limit: 201},
	} {
		if _, err := (*Catalog)(nil).List(context.Background(), query); err == nil {
			t.Fatalf("accepted %#v", query)
		}
	}
}

func TestListScopeOrderingAndLimit(t *testing.T) {
	dir := privateTempDir(t)
	instance := openTestCatalog(t, filepath.Join(dir, "state.sqlite"), filepath.Join(dir, "sessions"))
	defer func() { _ = instance.Close() }()
	ctx := context.Background()
	query := ListQuery{Scope: ListCurrentDirectory, CreationCWD: "/workspace/repo", Limit: 1}
	empty, err := instance.List(ctx, query)
	if err != nil || len(empty.Entries) != 0 || empty.HasMore {
		t.Fatalf("empty=%+v %v", empty, err)
	}
	entries := []Entry{}
	present := map[domain.ThreadID]struct{}{}
	for i, cwd := range []string{"/workspace/repo", "/workspace/repo", "/workspace/repo/sub", "/workspace/repo-other", "/workspace/worktree"} {
		e := catalogTestEntry(t, cwd, domain.ProviderOpenAI, "responses", "first", time.Unix(100, 0).UTC())
		// 固定身份与时间，避免随机 UUID 决定测试预期。
		id, parseErr := domain.ParseThreadID(fmt.Sprintf("019abcde-1234-7123-8123-%012d", i+1))
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		e.ThreadID = id
		e.JournalPath = catalogTestJournalPath(t, id)
		if i == 1 {
			e.ProviderFamily = domain.ProviderAnthropic
			e.ProviderWire = "messages"
			e.Model = "second"
		}
		if i > 1 {
			e.UpdatedAt = e.UpdatedAt.Add(time.Hour)
		}
		entries = append(entries, e)
		present[id] = struct{}{}
	}
	if err := instance.applyReconciliation(ctx, entries, present, nil); err != nil {
		t.Fatal(err)
	}
	for _, limit := range []int{1, 2, 200} {
		query.Limit = limit
		got, err := instance.List(ctx, query)
		want := []Entry{entries[1], entries[0]}
		if limit == 1 {
			want = want[:1]
		}
		if err != nil || !reflect.DeepEqual(got.Entries, want) || got.HasMore != (limit == 1) {
			t.Fatalf("limit %d: %+v %v", limit, got, err)
		}
	}
	all, err := instance.List(ctx, ListQuery{Scope: ListAllDirectories, Limit: 200})
	if err != nil || len(all.Entries) != 5 || all.HasMore || all.Entries[0].ThreadID != entries[4].ThreadID {
		t.Fatalf("all=%+v %v", all, err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if result, err := instance.List(cancelled, query); err == nil || len(result.Entries) != 0 {
		t.Fatalf("cancel=%+v %v", result, err)
	}
}
