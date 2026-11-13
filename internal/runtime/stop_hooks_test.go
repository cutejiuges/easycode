package runtime

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"easycode/internal/domain"
	"easycode/internal/protocol"
	"easycode/internal/provider/openai"
	"easycode/internal/secret"
	"easycode/internal/session"
)

type stopRunnerFixture struct {
	inputs     []domain.StopInput
	run        func(context.Context, domain.HookRunIdentity, domain.HookDeclaration) domain.HookExecution
	preflights int
}

func (f *stopRunnerFixture) Preflight(context.Context) domain.HookCode {
	f.preflights++
	return domain.HookOK
}
func (f *stopRunnerFixture) Run(ctx context.Context, id domain.HookRunIdentity, d domain.HookDeclaration, input domain.HookInput, _ time.Duration) domain.HookExecution {
	s, _ := input.StopInput()
	f.inputs = append(f.inputs, s)
	return f.run(ctx, id, d)
}
func newStopRuntimeFixture(t *testing.T, journal Journal) (*Runtime, *stopRunnerFixture, *atomic.Int32) {
	t.Helper()
	requests := new(atomic.Int32)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"response-test\"}}\n\nevent: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"item\":{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"  answer\\n\"}]}}\n\nevent: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"response-test\"}}\n\n")
	}))
	t.Cleanup(server.Close)
	p, err := openai.New(openai.Config{BaseURL: server.URL, APIKey: secret.New("fixture-key"), Model: "gpt-test"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close() })
	conversation, err := p.NewConversation(runtimeThreadID)
	if err != nil {
		t.Fatal(err)
	}
	r := newTestRuntime(t, conversation, journal)
	r.interactiveApprovals = true
	d, err := domain.NewStopHookDeclaration(domain.HookSourceProject, domain.HookFeedbackModel, domain.HookDigest(nil), "completion", "true", 5)
	if err != nil {
		t.Fatal(err)
	}
	s, _ := domain.NewHookSnapshot([]domain.HookDeclaration{d}, nil)
	runner := &stopRunnerFixture{}
	runner.run = func(_ context.Context, id domain.HookRunIdentity, _ domain.HookDeclaration) domain.HookExecution {
		result := domain.HookRunResult{Identity: id, Status: domain.HookContinue, Code: domain.HookOK, ExitKnown: true, CleanupConfirmed: true, Origin: "execution"}
		if len(runner.inputs) <= 2 {
			result.Status, result.Code = domain.HookBlock, domain.HookDenied
			candidate, _ := domain.NewHookFeedbackCandidate("check again")
			return domain.HookExecution{Result: result, Feedback: candidate}
		}
		return domain.HookExecution{Result: result}
	}
	if err := r.InstallHooks(s, runner, "/workspace", domain.HookOK); err != nil {
		t.Fatal(err)
	}
	return r, runner, requests
}
func TestStopRuntimeContinuesTwiceWithoutCompletingEarly(t *testing.T) {
	j := &fakeJournal{}
	r, runner, requests := newStopRuntimeFixture(t, j)
	trust := hookTrustEmitter(t, r, protocol.HookTrustSession, nil)
	var completed int
	input, _ := domain.NewUserInput("01900000-0000-7000-8000-000000000001", "task")
	err := r.RunTurn(context.Background(), []domain.UserInput{input}, func(e protocol.Event) {
		trust(e)
		if e.Kind == protocol.EventTurnCompleted {
			completed++
			if len(runner.inputs) != 3 {
				t.Error("completed before checks")
			}
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if completed != 1 || requests.Load() != 3 || len(runner.inputs) != 3 {
		t.Fatalf("wrong counts %d/%d/%d", completed, requests.Load(), len(runner.inputs))
	}
	for i, input := range runner.inputs {
		if input.Active() != (i > 0) || input.Message() != "  answer\n" || input.Sample().SampleIndex != uint32(i) {
			t.Fatal("wrong exact sample", i, input)
		}
	}
	var decisions []domain.StopDecision
	for _, batch := range j.snapshot() {
		for _, draft := range batch {
			if draft.EventKind() == session.EventTurnStopHookDecided {
				d, err := domain.DecodeStopDecision(draft.PayloadBytes())
				if err != nil {
					t.Fatal(err)
				}
				decisions = append(decisions, d)
			}
		}
	}
	if len(decisions) != 3 || decisions[0].Disposition != domain.StopContinued || decisions[1].Disposition != domain.StopContinued || decisions[2].Disposition != domain.StopPassed {
		t.Fatal("wrong decisions", decisions)
	}
}
