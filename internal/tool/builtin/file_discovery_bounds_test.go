//go:build darwin || linux

package builtin

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"easycode/internal/domain"
)

type discoveryTreeCounts struct{ opened, closed, entries, maxBatch atomic.Int32 }
type discoveryTreeDirectory struct {
	counts    *discoveryTreeCounts
	level, at int
	deep      bool
	stack     bool
	closed    atomic.Bool
}

func (d *discoveryTreeDirectory) readEntryNames(n int) ([]string, error) {
	if n > 256 || n < 1 {
		return nil, fmt.Errorf("invalid batch")
	}
	d.counts.maxBatch.Store(max(d.counts.maxBatch.Load(), int32(n)))
	count := 1001
	if d.level == 0 {
		count = 200
	}
	if d.deep {
		count = 1
	}
	if d.stack {
		count = 4000
	}
	if d.at == count {
		return nil, io.EOF
	}
	end := min(count, d.at+n)
	names := make([]string, 0, end-d.at)
	for i := d.at; i < end; i++ {
		name := fmt.Sprintf("n%04d", i)
		if d.stack {
			if i == 0 {
				name = "a"
			} else {
				name = strings.Repeat("x", 239) + fmt.Sprintf("%04d", i)
			}
		}
		names = append(names, name)
	}
	d.at = end
	d.counts.entries.Add(int32(len(names)))
	return names, nil
}
func (d *discoveryTreeDirectory) openChild(name string, _ workspaceHooks) (workspaceChild, error) {
	if name == ".gitignore" || name == ".ignore" || name == ".rgignore" {
		return nil, os.ErrNotExist
	}
	if !d.stack && d.level == 0 || d.deep || d.stack && name == "a" {
		child := &discoveryTreeDirectory{counts: d.counts, level: d.level + 1, deep: d.deep, stack: d.stack}
		d.counts.opened.Add(1)
		return discoveryTreeChild{child}, nil
	}
	return discoveryRegularChild{}, nil
}
func (d *discoveryTreeDirectory) close() error {
	if d.closed.CompareAndSwap(false, true) {
		d.counts.closed.Add(1)
	}
	return nil
}
func (d *discoveryTreeDirectory) interrupt() error { return d.close() }

type discoveryTreeChild struct{ dir *discoveryTreeDirectory }

func (discoveryTreeChild) kind() workspaceNodeKind         { return workspaceNodeDirectory }
func (discoveryTreeChild) size() int64                     { return 0 }
func (discoveryTreeChild) file() *os.File                  { return nil }
func (c discoveryTreeChild) directory() workspaceDirectory { return c.dir }
func (c discoveryTreeChild) close() error                  { return c.dir.close() }

func TestFileDiscoveryRawEntryDepthAndExactDirectoryBounds(t *testing.T) {
	for _, deep := range []bool{false, true} {
		counts := &discoveryTreeCounts{}
		counts.opened.Store(1)
		directory := &discoveryTreeDirectory{counts: counts, deep: deep}
		workspace := &Workspace{handle: &discoveryNamesHandle{directory: directory}}
		published := 0
		report, err := workspace.DiscoverFileNames(context.Background(), func(paths []string, _ domain.FileDiscoveryReport) (bool, error) {
			if len(paths) > 256 {
				t.Fatal("unbounded batch")
			}
			published += len(paths)
			return true, nil
		})
		if err != nil || counts.opened.Load() != counts.closed.Load() || counts.maxBatch.Load() > 256 {
			t.Fatal("discovery lost directory ownership", counts, err)
		}
		if deep {
			if report.Issues&domain.FileDiscoveryDepthLimit == 0 || counts.opened.Load() != 66 || published != 0 {
				t.Fatal("depth traversal escaped bound", report, counts.opened.Load())
			}
		} else if report.Issues&domain.FileDiscoveryEntryLimit == 0 || report.Visited != 200000 || counts.entries.Load() != 200000 || published != 199199 {
			t.Fatal("raw enumeration escaped bound or published incomplete directory", report, published)
		}
	}
	for _, size := range []int{4096, 4097} {
		names := make([]string, size)
		for i := range names {
			names[i] = fmt.Sprintf("n%04d", i)
		}
		dir := &discoveryNamesDirectory{batchedNamesFixture: &batchedNamesFixture{names: names}}
		workspace := &Workspace{handle: &discoveryNamesHandle{directory: dir}}
		paths, report, err := discoverFixtureFiles(t, workspace)
		if err != nil || size == 4096 && (len(paths) != 4096 || report.Issues != 0) || size == 4097 && (len(paths) != 0 || report.Issues&domain.FileDiscoveryDirectoryLimit == 0) {
			t.Fatal("exact directory boundary wrong", size, len(paths), report, err)
		}
	}
	names := make([]string, 4000)
	for i := range names {
		names[i] = fmt.Sprintf("%04d", i) + strings.Repeat("x", 280)
	}
	dir := &discoveryNamesDirectory{batchedNamesFixture: &batchedNamesFixture{names: names}}
	workspace := &Workspace{handle: &discoveryNamesHandle{directory: dir}}
	paths, report, err := discoverFixtureFiles(t, workspace)
	if err != nil || len(paths) != 0 || report.Issues&domain.FileDiscoveryDirectoryLimit == 0 {
		t.Fatal("directory byte bound escaped", report, err)
	}
}

func TestFileDiscoveryRetainedDirectoryNamesNeverExceedStackBudget(t *testing.T) {
	counts := &discoveryTreeCounts{}
	counts.opened.Store(1)
	dir := &discoveryTreeDirectory{counts: counts, stack: true}
	w := &Workspace{handle: &discoveryNamesHandle{directory: dir}}
	maxPublished := 0
	report, err := w.DiscoverFileNames(context.Background(), func(paths []string, _ domain.FileDiscoveryReport) (bool, error) {
		maxPublished = max(maxPublished, len(paths))
		return true, nil
	})
	if err != nil || report.Issues&domain.FileDiscoveryStackLimit == 0 || report.Issues&domain.FileDiscoveryDirectoryLimit != 0 || counts.opened.Load() != 9 || counts.opened.Load() != counts.closed.Load() || maxPublished > 256 {
		t.Fatal("retained name stack escaped budget or leaked owner", counts.opened.Load(), counts.closed.Load(), report, err)
	}
}
