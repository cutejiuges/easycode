package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
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
	return newStopRuntimeRequestFixture(t, journal, nil, func(int) string { return "  answer\n" })
}
func newStopRuntimeRequestFixture(t *testing.T, journal Journal, inspect func(int, []byte), answer func(int) string) (*Runtime, *stopRunnerFixture, *atomic.Int32) {
	t.Helper()
	requests := new(atomic.Int32)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		index := int(requests.Add(1)) - 1
		if inspect != nil {
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Error(err)
				return
			}
			inspect(index, body)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		text, _ := json.Marshal(answer(index))
		_, _ = fmt.Fprintf(w, "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"response-test\"}}\n\nevent: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"item\":{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":%s}]}}\n\nevent: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"response-test\"}}\n\n", text)
	}))
	t.Cleanup(server.Close)
	p, err := openai.New(openai.Config{BaseURL: server.URL, APIKey: secret.New("fixture-key"), Model: "gpt-test"})
	requireNoError(t, err)
	t.Cleanup(func() { _ = p.Close() })
	conversation, err := p.NewConversation(runtimeThreadID)
	requireNoError(t, err)
	r := newTestRuntime(t, conversation, journal)
	r.interactiveApprovals = true
	d, err := domain.NewStopHookDeclaration(domain.HookSourceProject, domain.HookFeedbackModel, domain.HookDigest(nil), "completion", "true", 5)
	requireNoError(t, err)
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
	installRuntimeTestHooks(t, r, runner, "/workspace", []domain.HookDeclaration{d})
	return r, runner, requests
}
func TestStopRuntimeContinuesTwiceWithoutCompletingEarly(t *testing.T) {
	j := &fakeJournal{}
	const steering = "preserve this steering input"
	planText := func(index int) string {
		return fmt.Sprintf("<proposed_plan>\nPlan revision %d\n</proposed_plan>", index)
	}
	r, runner, requests := newStopRuntimeRequestFixture(t, j, func(index int, body []byte) {
		var request struct {
			Instructions string            `json:"instructions"`
			Tools        []json.RawMessage `json:"tools"`
		}
		if err := json.Unmarshal(body, &request); err != nil || request.Instructions != domain.WorkModePlan.Instructions() || len(request.Tools) != 7 {
			t.Errorf("sampling request %d lost Plan guidance or declarations: %v", index, err)
		}
		wantSteering := 0
		if index > 0 {
			wantSteering = 1
		}
		if bytes.Count(body, []byte("check again")) != index || bytes.Count(body, []byte(steering)) != wantSteering {
			t.Errorf("sampling request %d lost native Stop feedback or steering", index)
		}
	}, planText)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	host := openPlanReviewFixture(t, ctx, r)
	trust := hookTrustEmitter(t, r, protocol.HookTrustSession, nil)
	input, _ := domain.NewLiteralInputDraft("task")
	if result, err := host.Append(ctx, input); err != nil || result.Status() != protocol.CommandStatusAccepted {
		t.Fatalf("cannot start Plan: %v, %v", result, err)
	}
	completed := 0
	for {
		item := receivePlanControl(t, ctx, host)
		if event, ok := item.TurnEvent(); ok {
			if event.Kind == protocol.EventHookTrustRequested {
				correction, _ := domain.NewLiteralInputDraft(steering)
				if result, err := host.Append(ctx, correction); err != nil || result.Disposition() != protocol.CommandDispositionQueued {
					t.Fatalf("cannot queue correction during Stop: %v, %v", result, err)
				}
				trust(event)
			}
			if event.Kind == protocol.EventTurnCompleted {
				completed++
			}
		}
		if offer, ok := item.PlanReviewOffer(); ok {
			if completed != 1 || offer.Ref.Sample.SampleIndex != 2 || offer.Markdown != "Plan revision 2\n" {
				t.Fatalf("review offered a superseded sample: %+v", offer)
			}
			break
		}
	}
	if completed != 1 || requests.Load() != 3 || len(runner.inputs) != 3 {
		t.Fatalf("wrong counts %d/%d/%d", completed, requests.Load(), len(runner.inputs))
	}
	for i, input := range runner.inputs {
		if input.Active() != (i > 0) || input.Message() != planText(i) || input.Sample().SampleIndex != uint32(i) {
			t.Fatal("wrong exact sample", i, input)
		}
	}
	var decisions []domain.StopDecision
	for _, batch := range j.snapshot() {
		for _, draft := range batch {
			if draft.EventKind() == session.EventTurnStopHookDecided {
				d, err := domain.DecodeStopDecision(draft.PayloadBytes())
				requireNoError(t, err)
				decisions = append(decisions, d)
			}
		}
	}
	if len(decisions) != 3 || decisions[0].Disposition != domain.StopContinued || decisions[1].Disposition != domain.StopContinued || decisions[2].Disposition != domain.StopPassed {
		t.Fatal("wrong decisions", decisions)
	}
}
