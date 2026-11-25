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

type sourceSkillFixture struct {
	calls      atomic.Int32
	attachment domain.SkillAttachment
}

func (s *sourceSkillFixture) Prepare(context.Context, string) (domain.SkillAttachment, bool, error) {
	s.calls.Add(1)
	return s.attachment, s.attachment != (domain.SkillAttachment{}), nil
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

func TestRecalledDraftSkillIntentDoesNotFallBackOrAttachImplicitly(t *testing.T) {
	ctx := context.Background()
	files := &fileSourceFixture{contents: map[string]string{"a": "current file contents"}}
	skills := &sourceSkillFixture{}
	source, err := newWorkspaceInputSource(files, skills)
	if err != nil {
		t.Fatal(err)
	}
	if err := source.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := source.Close(ctx); err != nil {
			t.Error(err)
		}
	})
	command := func(text string, intent domain.InputSkillIntent) protocol.Command {
		t.Helper()
		draft, err := domain.NewInteractiveInputDraftWithSkillIntent(text, intent)
		if err != nil {
			t.Fatal(err)
		}
		value, err := protocol.NewSubmitConversationalInputDraftCommand("rewind-test", draft)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	required := command("/rewind @a", domain.InputSkillRequired)
	if required.ValidateForAdmission() == nil {
		t.Fatal("unprepared required skill passed admission")
	}
	if _, err := source.Prepare(ctx, required); err == nil || !strings.Contains(err.Error(), "skill_not_found") || files.reads.Load() != 0 {
		t.Fatal("unavailable skill fell back to conversational input or triggered file work", err)
	}
	skills.attachment, err = domain.NewSkillAttachment(domain.SkillSource{Scope: "user", Directory: "rewind", Kind: "easycode"}, "rewind", strings.Repeat("a", 64), "@a", "current Skill prompt")
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []struct {
		text      string
		intent    domain.InputSkillIntent
		wantSkill bool
		wantFiles int
	}{
		{"/rewind @a", domain.InputSkillLiteral, false, 1},
		{"/compact @a", domain.InputSkillLiteral, false, 0},
		{"/rewind @a", domain.InputSkillRequired, true, 1},
	} {
		got, err := source.Prepare(ctx, command(scenario.text, scenario.intent))
		attachment, present := got.SkillAttachment()
		if err != nil || got.ValidateForAdmission() != nil || present != scenario.wantSkill ||
			(present && attachment != skills.attachment) || got.FileAttachments().Len() != scenario.wantFiles || got.Text() != scenario.text {
			t.Fatalf("wrong preparation for %q/%d: %+v %v", scenario.text, scenario.intent, got, err)
		}
	}
	if skills.calls.Load() != 2 {
		t.Fatal("literal input reached Skill preparation")
	}
}
