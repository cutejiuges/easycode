package app

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"easycode/internal/domain"
	"easycode/internal/protocol"
)

type sourceSkillFixture struct{ calls atomic.Int32 }

func (s *sourceSkillFixture) Prepare(context.Context, string) (domain.SkillAttachment, bool, error) {
	s.calls.Add(1)
	return domain.SkillAttachment{}, false, nil
}

type fileSourceFixture struct {
	contents                 map[string]string
	reads, scans             atomic.Int32
	readEntered, readRelease chan struct{}
	discover                 func(context.Context, func([]string, domain.FileDiscoveryReport) (bool, error)) (domain.FileDiscoveryReport, error)
}

func (f *fileSourceFixture) ReadWorkspaceFile(ctx context.Context, path string, limit int) (string, int, error) {
	f.reads.Add(1)
	if f.readEntered != nil {
		close(f.readEntered)
		<-f.readRelease
	}
	if err := ctx.Err(); err != nil {
		return "", 0, err
	}
	text := f.contents[path]
	if len(text) > limit {
		return "", 0, domain.NewFileFailure(domain.FileLimit)
	}
	return text, len(text), nil
}
func (f *fileSourceFixture) DiscoverFileNames(ctx context.Context, publish func([]string, domain.FileDiscoveryReport) (bool, error)) (domain.FileDiscoveryReport, error) {
	f.scans.Add(1)
	if f.discover != nil {
		return f.discover(ctx, publish)
	}
	_, err := publish([]string{"a"}, domain.FileDiscoveryReport{Visited: 1})
	return domain.FileDiscoveryReport{Visited: 1}, err
}

func fileQuery(t *testing.T, query string, request, session uint64) domain.FileSearchQuery {
	t.Helper()
	q, err := domain.NewFileSearchQuery(domain.FileSearchIdentity{WorkspaceGeneration: 1, SessionGeneration: session, ComposerRevision: request, RequestGeneration: request, Target: domain.FileMentionTarget{Start: 0, PathStart: 1, PathEnd: len(query) + 1, End: len(query) + 1, Query: query}})
	if err != nil {
		t.Fatal(err)
	}
	return q
}
func awaitFileSnapshot(t *testing.T, source *workspaceInputSource, predicate func(domain.FileSearchSnapshot) bool) domain.FileSearchSnapshot {
	t.Helper()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for {
		select {
		case value, ok := <-source.FileResults():
			if !ok {
				t.Fatal("source closed before expected snapshot")
			}
			if predicate(value) {
				return value
			}
		case <-deadline.C:
			t.Fatal("missing file snapshot")
		}
	}
}

func TestFileSourceLifecycleBusyCancellationAndFinalOwnership(t *testing.T) {
	fixture := &fileSourceFixture{contents: map[string]string{"a": "content"}, readEntered: make(chan struct{}), readRelease: make(chan struct{})}
	skills := &sourceSkillFixture{}
	source, err := newWorkspaceInputSource(fixture, skills)
	if err != nil {
		t.Fatal(err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if source.Start(cancelled) == nil {
		t.Fatal("cancelled startup accepted")
	}
	if err := source.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if source.Start(context.Background()) == nil {
		t.Fatal("second worker started")
	}
	draft, _ := domain.NewInteractiveInputDraft("question @a")
	command, _ := protocol.NewSubmitConversationalInputDraftCommand("file-prepare", draft)
	finished := make(chan error, 1)
	go func() { _, err := source.Prepare(context.Background(), command); finished <- err }()
	<-fixture.readEntered
	if _, err := source.Prepare(context.Background(), command); err == nil || !strings.Contains(err.Error(), "file_reference_busy") {
		t.Fatal("excess preparation was queued", err)
	}
	source.BeginClose()
	if _, err := source.Prepare(context.Background(), command); err == nil {
		t.Fatal("preparation entered after close gate")
	}
	if err := source.Close(cancelled); !errors.Is(err, context.Canceled) {
		t.Fatal("shutdown deadline lost", err)
	}
	select {
	case <-source.Done():
		t.Fatal("released ownership before read stack exited")
	default:
	}
	close(fixture.readRelease)
	if err := <-finished; !errors.Is(err, context.Canceled) {
		t.Fatal("prepare cancellation", err)
	}
	if err := source.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := source.Close(context.Background()); err != nil {
		t.Fatal("non-idempotent close", err)
	}
	if fixture.reads.Load() != 1 || skills.calls.Load() != 1 {
		t.Fatal("duplicate source work")
	}
	fresh, _ := newWorkspaceInputSource(&fileSourceFixture{}, &sourceSkillFixture{})
	if err := fresh.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if fresh.Start(context.Background()) == nil {
		t.Fatal("closed source restarted")
	}
}

func TestFileSourceReusesDiscoveryAndRejectsClosedOrOlderQueries(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var discoveries atomic.Int32
	fixture := &fileSourceFixture{discover: func(ctx context.Context, publish func([]string, domain.FileDiscoveryReport) (bool, error)) (domain.FileDiscoveryReport, error) {
		if discoveries.Add(1) > 1 {
			report := domain.FileDiscoveryReport{Visited: 1}
			_, err := publish([]string{"beta.go"}, report)
			return report, err
		}
		deadline, ok := ctx.Deadline()
		if !ok {
			t.Error("discovery has no deadline")
		}
		report := domain.FileDiscoveryReport{Visited: 2}
		if _, err := publish([]string{"src/alpha.go", "docs/notes.md"}, report); err != nil {
			return report, err
		}
		close(entered)
		select {
		case <-release:
		case <-ctx.Done():
			return report, ctx.Err()
		}
		if current, _ := ctx.Deadline(); current != deadline {
			t.Error("query reset discovery deadline")
		}
		report.Visited++
		_, err := publish([]string{"src/beta.go"}, report)
		return report, err
	}}
	source, _ := newWorkspaceInputSource(fixture, &sourceSkillFixture{})
	if err := source.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer source.Close(context.Background())
	first := fileQuery(t, "alpha", 1, 1)
	if err := source.QueryFiles(first); err != nil {
		t.Fatal(err)
	}
	<-entered
	initial := awaitFileSnapshot(t, source, func(v domain.FileSearchSnapshot) bool { return v.Identity() == first.Identity() && len(v.Paths()) > 0 })
	if initial.Paths()[0] != "src/alpha.go" {
		t.Fatal(initial.Paths())
	}
	second := fileQuery(t, "notes", 2, 1)
	if err := source.QueryFiles(second); err != nil {
		t.Fatal(err)
	}
	if source.QueryFiles(first) == nil {
		t.Fatal("older request replaced current slot")
	}
	close(release)
	result := awaitFileSnapshot(t, source, func(v domain.FileSearchSnapshot) bool {
		return v.Identity() == second.Identity() && v.State() == domain.FileSearchComplete
	})
	if strings.Join(result.Paths(), ",") != "docs/notes.md" || fixture.scans.Load() != 1 || result.IndexRevision() < initial.IndexRevision() {
		t.Fatal("discovery restarted or stale matching", result.Paths())
	}
	source.CancelFiles(second.Identity())
	if source.QueryFiles(second) == nil {
		t.Fatal("closed candidate session revived")
	}
	third := fileQuery(t, "beta", 3, 2)
	if err := source.QueryFiles(third); err != nil {
		t.Fatal(err)
	}
	reopened := awaitFileSnapshot(t, source, func(v domain.FileSearchSnapshot) bool {
		return v.Identity() == third.Identity() && v.State() == domain.FileSearchComplete
	})
	if strings.Join(reopened.Paths(), ",") != "beta.go" || fixture.scans.Load() != 2 {
		t.Fatal("reopen reused a stale index")
	}
}

func TestFileSourceDeadlineRetainsCompleteBatchesForLaterQueries(t *testing.T) {
	fixture := &fileSourceFixture{discover: func(ctx context.Context, publish func([]string, domain.FileDiscoveryReport) (bool, error)) (domain.FileDiscoveryReport, error) {
		if _, ok := ctx.Deadline(); !ok {
			t.Error("missing discovery deadline")
		}
		report := domain.FileDiscoveryReport{Visited: 1}
		if _, err := publish([]string{"a.go"}, report); err != nil {
			return report, err
		}
		return report, context.DeadlineExceeded
	}}
	source, _ := newWorkspaceInputSource(fixture, &sourceSkillFixture{})
	if err := source.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer source.Close(context.Background())
	query := fileQuery(t, "a", 1, 1)
	if err := source.QueryFiles(query); err != nil {
		t.Fatal(err)
	}
	partial := awaitFileSnapshot(t, source, func(v domain.FileSearchSnapshot) bool { return v.State() == domain.FileSearchIncomplete })
	if len(partial.Paths()) != 1 || partial.Report().Issues&domain.FileDiscoveryDeadline == 0 {
		t.Fatal("partial coverage hidden")
	}
	query = fileQuery(t, "a.go", 2, 1)
	if err := source.QueryFiles(query); err != nil {
		t.Fatal(err)
	}
	latest := awaitFileSnapshot(t, source, func(v domain.FileSearchSnapshot) bool {
		return v.Identity() == query.Identity() && v.State() == domain.FileSearchIncomplete
	})
	if len(latest.Paths()) != 1 || fixture.scans.Load() != 1 {
		t.Fatal("deadline restarted discovery or lost partial index")
	}
}

func TestFileMatcherStableRankingWorkAndUnicode(t *testing.T) {
	query := fileQuery(t, "main", 1, 1)
	paths := []string{"src/ma-in.go", "xmain.go", "main.go", "src/main.go", "main"}
	matcher := newFileMatcher(query)
	if err := matcher.advance(context.Background(), paths); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(matcher.paths(), ","); got != "main,main.go,src/main.go,xmain.go,src/ma-in.go" {
		t.Fatal("ranking changed", got)
	}
	for left, right := 0, len(paths)-1; left < right; left, right = left+1, right-1 {
		paths[left], paths[right] = paths[right], paths[left]
	}
	other := newFileMatcher(query)
	_ = other.advance(context.Background(), paths)
	if strings.Join(other.paths(), ",") != strings.Join(matcher.paths(), ",") {
		t.Fatal("enumeration affected ranking")
	}
	matcher = newFileMatcher(fileQuery(t, "界", 1, 1))
	_ = matcher.advance(context.Background(), []string{"中文/世界.go", "界.go"})
	if len(matcher.paths()) != 2 || matcher.paths()[0] != "界.go" {
		t.Fatal("unicode match", matcher.paths())
	}
	matcher = newFileMatcher(query)
	matcher.budget.remaining = 5
	if err := matcher.advance(context.Background(), paths); err != nil || !matcher.limited || matcher.budget.spent > domain.MaxFileSearchWork {
		t.Fatal("matching unbounded", err)
	}
	state := &fileSearchSession{index: make([]string, 0), seen: make(map[string]struct{})}
	if !state.appendPaths([]string{"a", "a"}) || len(state.index) != 1 || state.revision != 1 {
		t.Fatal("index duplicated paths")
	}
	state.indexBytes = domain.MaxFileIndexBytes
	if state.appendPaths([]string{"b"}) || len(state.index) != 1 {
		t.Fatal("index exceeded memory")
	}
}
