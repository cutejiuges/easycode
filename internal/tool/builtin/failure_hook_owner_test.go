package builtin

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"easycode/internal/domain"
	"easycode/internal/executor/process"
)

type failureHookLauncher struct {
	handle process.Handle
	calls  int
}

func (*failureHookLauncher) preflight(context.Context) domain.HookCode { return domain.HookOK }
func (l *failureHookLauncher) startHook(context.Context, domain.HookDeclaration) (process.Handle, error) {
	l.calls++
	return l.handle, nil
}

type hookDiscardInput struct{}

func (hookDiscardInput) Write(b []byte) (int, error) { return len(b), nil }
func (hookDiscardInput) Close() error                { return nil }

type hookBrokenOutput struct{}

func (hookBrokenOutput) Read([]byte) (int, error) { return 0, errors.New("PRIVATE-IO-CAUSE") }

func TestFailureHookRunnerRetainsOwnerAcrossDeterministicFaults(t *testing.T) {
	for _, mode := range []string{"timeout", "blocked-input", "output-limit", "io", "uncertain"} {
		t.Run(mode, func(t *testing.T) {
			entered, release := make(chan struct{}), make(chan struct{})
			h := &fakeOwnedProcess{out: io.NopCloser(strings.NewReader("")), errout: io.NopCloser(strings.NewReader("")), alive: true, waitResult: make(chan processWaitResult, 1), waitEntered: entered, waitRelease: release}
			var stdin io.WriteCloser = hookDiscardInput{}
			switch mode {
			case "blocked-input":
				reader, writer := io.Pipe()
				defer reader.Close()
				stdin = writer
			case "output-limit":
				h.out = io.NopCloser(strings.NewReader(strings.Repeat("x", 18<<10)))
			case "io":
				h.out = io.NopCloser(hookBrokenOutput{})
			case "uncertain":
				h.probeError = errors.New("PRIVATE-PROBE-CAUSE")
			}
			launcher := &failureHookLauncher{handle: ownerHandle{handle: h, input: stdin}}
			clock := &fakeProcessClock{timers: make(chan *fakeProcessTimer, 8)}
			runner := &HookRunner{launcher: launcher, clock: ownerClock{clock}, parse: func(domain.HookEvent, domain.HookFeedbackPolicy, int, []byte) domain.HookParsedOutput {
				t.Error("fault reached output parser")
				return domain.HookParsedOutput{}
			}}
			uuid := "018f1d8a-7b5c-7def-8abc-456789abcdef"
			d, _ := domain.NewHookDeclaration(domain.HookSourceProject, domain.HookPostToolUseFailure, domain.HookFeedbackModel, strings.Repeat("a", 64), "diagnose", 7, "true", 5)
			branch, _ := domain.NewPostHookBranch(d.Reference.Event, []domain.HookReference{d.Reference}, 16<<10)
			p, err := domain.NewPostHookPlan(uuid, domain.HookExec, []domain.PostHookBranch{branch}, 256<<10, 16<<10)
			if err != nil {
				t.Fatal(err)
			}
			id, _ := domain.NewPostHookRunIdentity(p, d.Reference.Event, 0)
			input, _ := domain.NewHookExecInput("true", "", 1000)
			payload, err := domain.NewFailureHookInput(domain.SessionID(uuid), domain.ThreadID(uuid), domain.TurnID(uuid), "call", "Bash", "/workspace", input, "process_start_failed")
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan domain.HookExecution, 1)
			go func() { done <- runner.Run(ctx, id, d, payload, time.Second); close(done) }()
			timeout := <-clock.timers
			if timeout.duration != time.Second {
				t.Fatal("wrong execution timeout")
			}
			if mode == "timeout" {
				timeout.ticks <- time.Unix(1, 0)
			}
			if mode == "blocked-input" || mode == "uncertain" {
				cancel()
			}
			grace := <-clock.timers
			if grace.duration != 250*time.Millisecond {
				t.Fatal("missing escalation")
			}
			grace.ticks <- time.Unix(2, 0)
			<-entered
			select {
			case <-done:
				t.Fatal("runner returned before final Wait")
			default:
			}
			close(release)
			execution := <-done
			want := domain.HookCancelled
			switch mode {
			case "timeout":
				want = domain.HookTimedOut
			case "output-limit":
				want = domain.HookOutputLimit
			case "io":
				want = domain.HookIOFailed
			case "uncertain":
				want = domain.HookOutcomeUncertain
			}
			if execution.Result.Validate() != nil || execution.Result.Code != want || !execution.Feedback.Empty() || h.waits != 1 || h.staleOperations != 0 || launcher.calls != 1 {
				t.Fatal("failure lost owner/classification", execution.Result)
			}
		})
	}
}
