package runtime

import (
	"context"
	"easycode/internal/domain"
	"easycode/internal/protocol"
	"testing"
)

type installationPreparer struct{ calls int }

func (p *installationPreparer) Prepare(context.Context, string) (domain.SkillAttachment, bool, error) {
	p.calls++
	return domain.SkillAttachment{}, false, nil
}

func TestSkillInstallationIsPureSingleAndBeforeWork(t *testing.T) {
	for _, state := range []string{"fresh", "active", "poisoned", "journal-poisoned", "prepared", "started", "loop-started", "turn-started"} {
		t.Run(state, func(t *testing.T) {
			journal := &startedFailureJournal{}
			r := newTestRuntime(t, &fakeConversation{stream: fixedStream()}, journal)
			p := &installationPreparer{}
			source, _ := domain.NewSkillSource("plugin", "qa", "codex")
			m, _ := domain.NewSkillMetadata(source, "qa:review", "Review", "", true, false)
			catalog, _ := domain.NewSkillCatalog([]domain.SkillMetadata{m})
			switch state {
			case "active":
				r.active.Store(true)
			case "poisoned":
				r.poisoned.Store(true)
			case "journal-poisoned":
				journal.poisoned = true
			case "prepared":
				_, _ = r.PrepareInput(context.Background(), protocol.Command{})
			case "started":
				host := NewChatSession(r)
				if err := host.Start(context.Background()); err != nil {
					t.Fatal(err)
				}
				defer host.Shutdown(context.Background())
			case "loop-started":
				loop, err := NewAgentLoop(r, DefaultAgentLoopConfig())
				if err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithCancel(context.Background())
				finished := make(chan error, 1)
				go func() { finished <- loop.Run(ctx) }()
				<-loop.Ready()
				cancel()
				<-finished
			case "turn-started":
				_ = r.RunTurn(context.Background(), runtimeInputs("hello"), func(protocol.Event) {})
			}
			err := r.InstallSkills(catalog, p)
			if (state == "fresh") != (err == nil) || p.calls != 0 {
				t.Fatalf("install: %v calls=%d", err, p.calls)
			}
			if state == "fresh" {
				if r.skillCatalog.Revision() != catalog.Revision() || r.InstallSkills(catalog, p) == nil {
					t.Fatal("snapshot replacement accepted")
				}
			}
		})
	}
}
