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
