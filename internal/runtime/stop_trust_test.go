package runtime

import (
	"context"
	"strings"
	"testing"
	"time"

	"easycode/internal/domain"
	"easycode/internal/protocol"
	"easycode/internal/provider/openai"
	"easycode/internal/session"
)

type countingStopConversation struct {
	*openai.Conversation
	reads int
	text  string
}

func (c *countingStopConversation) ProjectAssistantSample(ref domain.AssistantSampleReference) (domain.AssistantSampleText, error) {
	c.reads++
	if c.text != "" {
		return domain.NewAssistantSampleText(ref, c.text)
	}
	return c.Conversation.ProjectAssistantSample(ref)
}

func TestStopPreflightPriorityAndNoHeadlessProjection(t *testing.T) {
	for _, mode := range []string{"cancelled", "invalid", "too-many", "headless", "unsupported", "input-limit", "valid-no-stop"} {
		t.Run(mode, func(t *testing.T) {
			j := &fakeJournal{}
			r, runner, requests := newStopRuntimeFixture(t, j)
			conversation := &countingStopConversation{Conversation: r.conversation.(*openai.Conversation)}
			r.conversation = conversation
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			want := domain.HookTrustRequired
			switch mode {
			case "cancelled":
				j.hook = func(ds []session.RecordDraft) {
					for _, d := range ds {
						if d.EventKind() == session.EventTurnStopHookPlanned {
							cancel()
						}
					}
				}
				r.hooks.snapshot, _ = domain.NewHookSnapshot(nil, []domain.HookDiagnostic{{Source: domain.HookSourceUser, Code: domain.HookConfigInvalid}})
				want = domain.HookCancelled
			case "invalid":
				r.hooks.snapshot, _ = domain.NewHookSnapshot(nil, []domain.HookDiagnostic{{Source: domain.HookSourceUser, Code: domain.HookConfigInvalid}})
				want = domain.HookConfigInvalid
			case "too-many":
				var ds []domain.HookDeclaration
				for i := range 9 {
					d, _ := domain.NewStopHookDeclaration(domain.HookSourceProject, domain.HookFeedbackNone, domain.HookDigest(nil), "check-"+string(rune('a'+i)), "true", 5)
					ds = append(ds, d)
				}
				r.hooks.snapshot, _ = domain.NewHookSnapshot(ds, nil)
				want = domain.HookMatchLimit
			case "unsupported":
				r.hooks.availability = domain.HookUnsupported
				want = domain.HookUnsupported
			case "input-limit":
				conversation.text = strings.Repeat("<", domain.MaxHookInputBytes/6+1)
				want = domain.HookInputLimit
			case "valid-no-stop":
				r.hooks.snapshot, _ = domain.NewHookSnapshot(nil, nil)
			}
			if mode == "headless" {
				r.interactiveApprovals = false
				r.hooks.availability = domain.HookUnsupported
			}
			input, _ := domain.NewUserInput("01900000-0000-7000-8000-000000000001", "task")
			trusts := 0
			err := r.RunTurn(ctx, []domain.UserInput{input}, hookTrustEmitter(t, r, protocol.HookTrustSession, &trusts))
			if mode == "valid-no-stop" {
				if err != nil {
					t.Fatal(err)
				}
				if len(j.snapshot()[2]) != 3 || j.snapshot()[2][2].EventKind() != session.EventTurnCompleted {
					t.Fatal("no-Stop atomic completion changed")
				}
				return
			}
			if err == nil || requests.Load() != 1 || len(runner.inputs) != 0 || trusts != 0 || runner.preflights != 0 {
				t.Fatalf("preflight side effects: %v network=%d runs=%d trusts=%d probes=%d", err, requests.Load(), len(runner.inputs), trusts, runner.preflights)
			}
			if mode != "input-limit" && conversation.reads != 0 {
				t.Fatal("lower-priority projection ran")
			}
			found := false
			for _, batch := range j.snapshot() {
				for _, draft := range batch {
					if draft.EventKind() == session.EventTurnStopHookDecided {
						decision, _ := domain.DecodeStopDecision(draft.PayloadBytes())
						if decision.Code != want {
							t.Fatal("wrong preflight precedence", decision.Code, want)
						}
						found = true
					}
				}
			}
			if !found {
				t.Fatal("saved answer lost its completion decision")
			}
		})
	}
}

func TestStopTrustReviewsAllDeclarationsBeforeFirstRun(t *testing.T) {
	for _, denySecond := range []bool{false, true} {
		t.Run(map[bool]string{false: "all-trusted", true: "second-denied"}[denySecond], func(t *testing.T) {
			r, runner, _ := newStopRuntimeFixture(t, &fakeJournal{})
			ds := r.hooks.snapshot.Declarations()
			second, _ := domain.NewStopHookDeclaration(domain.HookSourceProject, domain.HookFeedbackNone, domain.HookDigest(nil), "second", "true", 5)
			ds = append(ds, second)
			r.hooks.snapshot, _ = domain.NewHookSnapshot(ds, nil)
			trusts := 0
			runner.run = func(_ context.Context, id domain.HookRunIdentity, _ domain.HookDeclaration) domain.HookExecution {
				if trusts != 2 {
					t.Error("ran before all trust resolved")
				}
				return domain.HookExecution{Result: domain.HookRunResult{Identity: id, Status: domain.HookContinue, Code: domain.HookOK, ExitKnown: true, CleanupConfirmed: true, Origin: "execution"}}
			}
			input, _ := domain.NewUserInput("01900000-0000-7000-8000-000000000001", "task")
			err := r.RunTurn(context.Background(), []domain.UserInput{input}, func(e protocol.Event) {
				if e.Kind != protocol.EventHookTrustRequested {
					return
				}
				p, _ := protocol.DecodeHookTrustRequested(e)
				trusts++
				response, _ := protocol.NewHookTrustResponse(r.sessionID, r.threadID, e.TurnID, p, protocol.HookTrustSession)
				for _, field := range []string{"sample", "target", "plan", "source"} {
					bad := response
					switch field {
					case "sample":
						sample, _ := bad.Target.AssistantSample()
						sample.SampleIndex++
						bad.Target, _ = domain.NewStopHookTarget(sample)
					case "target":
						bad.Target, _ = domain.NewToolHookTarget(string(runtimeTurnID), domain.HookExec)
					case "plan":
						bad.PlanDigest = domain.HookDigest([]byte("other"))
					case "source":
						bad.Reference.Source = domain.HookSourceUser
					}
					if r.ResolveHookTrust(bad) == nil {
						t.Error("mismatched trust granted", field)
					}
				}
				if denySecond && trusts == 2 {
					response.Decision = protocol.HookTrustDeny
				}
				if err := r.ResolveHookTrust(response); err != nil {
					t.Fatal(err)
				}
			})
			if denySecond {
				if err == nil || len(runner.inputs) != 0 {
					t.Fatal("partial trust executed a prefix")
				}
			} else if err != nil || len(runner.inputs) != 2 {
				t.Fatal("trusted checks not run", err)
			}
		})
	}
}

func TestStopExecutionBudgetDoesNotResetAcrossContinuations(t *testing.T) {
	j := &fakeJournal{}
	r, runner, requests := newStopRuntimeFixture(t, j)
	now := time.Unix(0, 0)
	r.hooks.now = func() time.Time { return now }
	original := runner.run
	runner.run = func(ctx context.Context, id domain.HookRunIdentity, d domain.HookDeclaration) domain.HookExecution {
		result := original(ctx, id, d)
		if len(runner.inputs) == 1 {
			now = now.Add(29 * time.Second)
		} else {
			now = now.Add(time.Second)
		}
		return result
	}
	input, _ := domain.NewUserInput("01900000-0000-7000-8000-000000000001", "task")
	err := r.RunTurn(context.Background(), []domain.UserInput{input}, hookTrustEmitter(t, r, protocol.HookTrustSession, nil))
	if err == nil || requests.Load() != 3 || len(runner.inputs) != 2 {
		t.Fatal("execution quota reset", err, requests.Load(), len(runner.inputs))
	}
	last := j.snapshot()[len(j.snapshot())-1]
	decision, _ := domain.DecodeStopDecision(last[0].PayloadBytes())
	if decision.Code != domain.HookExecutionBudgetExhausted || decision.Processed != 0 || decision.Continuations != 2 {
		t.Fatal("wrong execution budget outcome")
	}
}

func TestStopCancelledTrustCannotInstallLateGrant(t *testing.T) {
	r, runner, _ := newStopRuntimeFixture(t, &fakeJournal{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	input, _ := domain.NewUserInput("01900000-0000-7000-8000-000000000001", "task")
	err := r.RunTurn(ctx, []domain.UserInput{input}, func(e protocol.Event) {
		if e.Kind != protocol.EventHookTrustRequested {
			return
		}
		p, _ := protocol.DecodeHookTrustRequested(e)
		response, _ := protocol.NewHookTrustResponse(r.sessionID, r.threadID, e.TurnID, p, protocol.HookTrustSession)
		cancel()
		if r.ResolveHookTrust(response) == nil {
			t.Error("late cancelled grant installed")
		}
	})
	if err == nil || len(runner.inputs) != 0 || r.HookEntries()[0].State != "untrusted" {
		t.Fatal("cancelled trust executed or persisted")
	}
}

func TestStopContinuationLimitUsesActualLastBlockingPolicy(t *testing.T) {
	for _, exit := range []int{0, 2} {
		t.Run(string(rune('0'+exit)), func(t *testing.T) {
			j := &fakeJournal{}
			r, runner, _ := newStopRuntimeFixture(t, j)
			ds := r.hooks.snapshot.Declarations()
			last, _ := domain.NewStopHookDeclaration(domain.HookSourceProject, domain.HookFeedbackNone, domain.HookDigest(nil), "last", "true", 5)
			ds = append(ds, last)
			r.hooks.snapshot, _ = domain.NewHookSnapshot(ds, nil)
			runner.run = func(_ context.Context, id domain.HookRunIdentity, d domain.HookDeclaration) domain.HookExecution {
				sample, _ := id.Target.AssistantSample()
				result := domain.HookRunResult{Identity: id, Status: domain.HookBlock, Code: domain.HookDenied, ExitKnown: true, CleanupConfirmed: true, Origin: "execution"}
				if sample.SampleIndex < 2 {
					candidate, _ := domain.NewHookFeedbackCandidate("same turn repair")
					return domain.HookExecution{Result: result, Feedback: candidate}
				}
				if d.Reference.Feedback == domain.HookFeedbackModel {
					result.Status, result.Code = domain.HookContinue, domain.HookOK
				} else {
					result.ExitCode = exit
				}
				return domain.HookExecution{Result: result}
			}
			input, _ := domain.NewUserInput("01900000-0000-7000-8000-000000000001", "task")
			if err := r.RunTurn(context.Background(), []domain.UserInput{input}, hookTrustEmitter(t, r, protocol.HookTrustSession, nil)); err == nil {
				t.Fatal("none blocker passed")
			}
			batch := j.snapshot()[len(j.snapshot())-1]
			decision, _ := domain.DecodeStopDecision(batch[1].PayloadBytes())
			ordinal, _ := decision.Source.HookOrdinal()
			if decision.Code != domain.HookDenied || decision.Disposition != domain.StopBlocked || decision.Processed != 2 || ordinal != 1 || decision.Continuations != 2 {
				t.Fatal("borrowed sibling model policy or misclassified exit2", decision)
			}
		})
	}
}
