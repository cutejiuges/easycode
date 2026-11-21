package runtime

import (
	"context"
	"strings"
	"testing"

	contextplan "easycode/internal/context"
	"easycode/internal/domain"
	"easycode/internal/protocol"
	"easycode/internal/session"
)

func TestPostCompactPoisonStopsPendingStopDelivery(t *testing.T) {
	journal := &fakeJournal{}
	r, runner, requests := newStopRuntimeRequestFixture(t, journal, func(index int, body []byte) {
		if index == 1 && (!strings.Contains(string(body), `"tool_choice":"none"`) || strings.Contains(string(body), "check again")) {
			t.Error("summary request did not exclude pending Stop feedback")
		}
	})
	r.budget, _ = contextplan.NewBudget(22000, 0, 0)
	autoPolicy, _ := contextplan.NewAutoCompactionPolicy(true, r.budget, 4096)
	decls := r.hooks.snapshot.Declarations()
	scope, _ := domain.ParseCompactionHookScope("auto")
	post, err := domain.NewPostCompactHookDeclaration(domain.HookSourceProject, domain.HookFeedbackNone, decls[0].Reference.Revision, "observe", scope, "true", 5)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := domain.NewHookSnapshot(append(decls, post), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.InstallHooks(snapshot, runner, "/workspace", domain.HookOK); err != nil {
		t.Fatal(err)
	}
	stopCheck := runner.run
	runner.run = func(ctx context.Context, id domain.HookRunIdentity, d domain.HookDeclaration) domain.HookExecution {
		if id.Event == domain.HookPostCompact {
			return domain.HookExecution{Result: domain.HookRunResult{Identity: id, Origin: "execution", Status: domain.HookUncertain, Code: domain.HookOutcomeUncertain}}
		}
		r.autoCompactionPolicy = autoPolicy
		return stopCheck(ctx, id, d)
	}
	input, _ := domain.NewUserInput("01900000-0000-7000-8000-000000000001", strings.Repeat("historical work ", 3500))
	trust := hookTrustEmitter(t, r, protocol.HookTrustSession, nil)
	completed := 0
	err = r.runTurn(context.Background(), []domain.UserInput{input}, r.profile, nil, func(event protocol.Event) {
		trust(event)
		if event.Kind == protocol.EventCompactionCompleted {
			completed++
		}
	})
	if err == nil || !r.poisoned.Load() || completed != 1 || requests.Load() != 2 || r.autoCompactionFailures != 0 {
		t.Fatalf("continued after uncertain post hook: %v, completions=%d, requests=%d", err, completed, requests.Load())
	}
	for _, batch := range journal.snapshot() {
		for _, draft := range batch {
			if draft.EventKind() == session.EventCompactionFailed || draft.EventKind() == session.EventTurnFailed ||
				draft.EventKind() == session.EventProviderNativeCommit && strings.Contains(string(draft.PayloadBytes()), "check again") {
				t.Fatalf("advanced past poisoned checkpoint: %s", draft.EventKind())
			}
		}
	}
}

type stopInputFixture struct {
	inputs []domain.UserInput
	claims int
	states []protocol.InputState
}

func (f *stopInputFixture) claim(context.Context, domain.TurnID, uint32) ([]domain.UserInput, error) {
	f.claims++
	inputs := f.inputs
	f.inputs = nil
	return inputs, nil
}
func (f *stopInputFixture) resolve(_ domain.TurnID, _ []domain.UserInput, state protocol.InputState, _ protocol.InputReason) {
	f.states = append(f.states, state)
}

type stopFaultJournal struct {
	fakeJournal
	mode   string
	cancel context.CancelFunc
	fired  bool
}

func (j *stopFaultJournal) AppendBatch(ctx context.Context, drafts []session.RecordDraft) ([]session.Record, error) {
	stage := ""
	for _, d := range drafts {
		if d.EventKind() == session.EventTurnStopHookPlanned {
			stage = "sample"
		}
		if d.EventKind() == session.EventTurnStopHookDecided {
			value, _ := domain.DecodeStopDecision(d.PayloadBytes())
			if value.Disposition == domain.StopContinued {
				stage = "intent"
			}
		}
	}
	if len(drafts) == 1 && drafts[0].EventKind() == session.EventProviderNativeCommit {
		stage = "delivery"
	}
	if len(drafts) == 3 && drafts[1].EventKind() == session.EventTurnInputsCommitted {
		stage = "compound"
	}
	fire := !j.fired && (j.mode == "cancel_before_"+stage || j.mode == "cancel_after_"+stage || j.mode == "sync_"+stage)
	if fire {
		j.fired = true
		if j.mode == "cancel_before_"+stage {
			j.cancel()
		}
		if j.mode == "sync_"+stage {
			j.mu.Lock()
			j.poisoned = true
			j.mu.Unlock()
			return nil, context.DeadlineExceeded
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	records, err := j.fakeJournal.AppendBatch(ctx, drafts)
	for i := range records {
		records[i].EventKind = drafts[i].EventKind()
		records[i].Payload = drafts[i].PayloadBytes()
		records[i].TurnID = drafts[i].TurnID()
	}
	if fire && j.mode == "cancel_after_"+stage {
		j.cancel()
	}
	return records, err
}
func TestStopCancellationAndSyncLinearization(t *testing.T) {
	for _, mode := range []string{"cancel_before_intent", "cancel_after_intent", "cancel_before_compound", "cancel_after_compound", "cancel_after_delivery", "sync_sample", "sync_intent", "sync_delivery", "sync_compound"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			j := &stopFaultJournal{mode: mode, cancel: cancel}
			r, runner, requests := newStopRuntimeFixture(t, j)
			input, _ := domain.NewUserInput("01900000-0000-7000-8000-000000000001", "task")
			correction, _ := domain.NewUserInput("01900000-0000-7000-8000-000000000002", "  genuine correction\n")
			control := &stopInputFixture{}
			if mode == "cancel_before_compound" || mode == "cancel_after_compound" || mode == "sync_compound" {
				control.inputs = []domain.UserInput{correction}
			}
			var terminals int
			trust := hookTrustEmitter(t, r, protocol.HookTrustSession, nil)
			err := r.runTurn(ctx, []domain.UserInput{input}, r.profile, control, func(e protocol.Event) {
				trust(e)
				if e.Kind == protocol.EventTurnCompleted || e.Kind == protocol.EventTurnFailed {
					terminals++
				}
			})
			if err == nil || !j.fired || requests.Load() != 1 || terminals > 1 {
				t.Fatalf("bad fault boundary: %v fired=%t network=%d terminals=%d", err, j.fired, requests.Load(), terminals)
			}
			var continued, nativeFeedback, steering, actualDenied int
			for _, batch := range j.snapshot() {
				for _, draft := range batch {
					if draft.EventKind() == session.EventTurnStopHookDecided {
						d, _ := domain.DecodeStopDecision(draft.PayloadBytes())
						if d.Disposition == domain.StopContinued {
							continued++
						}
					}
					if draft.EventKind() == session.EventHookExecutionFinished {
						f, _ := session.DecodeHookExecutionFinished(session.Record{EventKind: draft.EventKind(), TurnID: draft.TurnID(), Payload: draft.PayloadBytes()})
						if f.Result.Status == domain.HookBlock && f.Result.ExitCode == 0 {
							actualDenied++
						}
					}
					if draft.EventKind() == session.EventTurnInputsCommitted {
						a, _ := session.DecodeTurnInputsPayload(session.Record{EventKind: draft.EventKind(), TurnID: draft.TurnID(), Payload: draft.PayloadBytes()})
						if a.Placement == domain.InputSteering {
							steering++
						}
					}
				}
				if len(batch) == 1 && batch[0].EventKind() == session.EventProviderNativeCommit || len(batch) == 3 && batch[1].EventKind() == session.EventTurnInputsCommitted {
					nativeFeedback++
				}
			}
			wantIntent := 1
			if mode == "cancel_before_intent" || mode == "sync_sample" || mode == "sync_intent" {
				wantIntent = 0
			}
			wantDelivery := wantIntent
			if mode == "sync_delivery" || mode == "sync_compound" {
				wantDelivery = 0
			}
			if continued != wantIntent || nativeFeedback != wantDelivery {
				t.Fatalf("lost/duplicated obligation %d/%d want %d/%d", continued, nativeFeedback, wantIntent, wantDelivery)
			}
			if mode == "cancel_after_compound" {
				if steering != 1 || control.states[len(control.states)-1] != protocol.InputCommitted {
					t.Fatal("accepted user input returned")
				}
			} else if steering != 0 {
				t.Fatal("unadmitted input committed")
			}
			if mode == "cancel_before_compound" && control.states[len(control.states)-1] != protocol.InputReturned {
				t.Fatal("cancelled claim not returned")
			}
			if mode == "sync_compound" && control.states[len(control.states)-1] != protocol.InputUncertain {
				t.Fatal("uncertain claim returned")
			}
			if mode != "sync_sample" && mode != "sync_intent" && actualDenied != 1 {
				t.Fatal("actual result rewritten")
			}
			if mode == "sync_sample" && len(runner.inputs) != 0 {
				t.Fatal("ran before sample Sync")
			}
		})
	}
}
