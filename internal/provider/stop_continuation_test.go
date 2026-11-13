package provider

import (
	"sync/atomic"
	"testing"

	"easycode/internal/domain"
)

func testStopFeedback(t *testing.T) domain.StopFeedback {
	t.Helper()
	const id = "01900000-0000-7000-8000-000000000001"
	d, err := domain.NewStopHookDeclaration(domain.HookSourceUser, domain.HookFeedbackModel, domain.HookDigest(nil), "completion", "true", 5)
	if err != nil {
		t.Fatal(err)
	}
	p, err := domain.NewStopChecksPlan(domain.AssistantSampleReference{TurnID: domain.TurnID(id)}, 0, []domain.HookReference{d.Reference})
	if err != nil {
		t.Fatal(err)
	}
	run, err := domain.NewStopHookRunIdentity(p, 0)
	if err != nil {
		t.Fatal(err)
	}
	candidate, _ := domain.NewHookFeedbackCandidate("  保留原始反馈\n")
	feedback, err := domain.NewStopFeedback(domain.HookRunResult{Identity: run, Status: domain.HookBlock, Code: domain.HookDenied, ExitKnown: true, CleanupConfirmed: true, Origin: "execution"}, d.Reference, candidate)
	if err != nil {
		t.Fatal(err)
	}
	return feedback
}

func TestPreparedStopContinuationHasOneResolutionForBothEntries(t *testing.T) {
	feedback := testStopFeedback(t)
	frozen, _ := domain.NewStopFeedbackOnly(feedback)
	sample, _ := feedback.Identity.Target.AssistantSample()
	user, _ := domain.NewUserInput("01900000-0000-7000-8000-000000000002", "用户纠正")
	association, _ := domain.NewInputAssociation(sample.TurnID, domain.InputSteering, 1, []domain.InputID{user.ID()})
	steered, err := domain.NewStopWithSteering(feedback, association, []domain.UserInput{user})
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range []domain.StopContinuationInput{frozen, steered} {
		t.Run(string(input.Kind()), func(t *testing.T) {
			entered, release := make(chan struct{}), make(chan struct{})
			var calls atomic.Int32
			finalize := func() { calls.Add(1); close(entered); <-release }
			discard := func() { calls.Add(1) }
			envelope, _ := NewNativeCommitEnvelope(domain.ProviderOpenAI, "responses", []byte(`{}`))
			var p *PreparedStopContinuation
			var err error
			if input.Kind() == domain.StopWithSteering {
				p, err = NewPreparedSteeredStopContinuation(input, envelope, envelope, finalize, discard, testInputPreview(t, domain.ProviderOpenAI))
			} else {
				p, err = NewPreparedStopContinuation(input, envelope, finalize, discard, testInputPreview(t, domain.ProviderOpenAI))
			}
			if err != nil {
				t.Fatal(err)
			}
			copy, err := p.Input()
			if err != nil {
				t.Fatal(err)
			}
			if a, ok := copy.Association(); ok {
				ids := a.IDs()
				ids[0] = "wrong"
				inputs := copy.Inputs()
				inputs[0] = domain.UserInput{}
				if copy.Validate() != nil {
					t.Fatal("candidate aliases getter")
				}
			}
			if _, ok, err := p.SteeringEnvelope(); err != nil || ok != (input.Kind() == domain.StopWithSteering) {
				t.Fatal("wrong envelope branch", err)
			}
			done := make(chan error, 1)
			go func() { done <- p.Finalize() }()
			<-entered
			if p.Finalized() {
				t.Fatal("finalized before atomic callback completed")
			}
			rejected := make(chan error, 1)
			go func() { rejected <- p.Discard() }()
			select {
			case <-rejected:
				t.Fatal("resolution passed incomplete callback")
			default:
			}
			close(release)
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			if err := <-rejected; err == nil {
				t.Fatal("double resolution accepted")
			}
			if calls.Load() != 1 || !p.Finalized() {
				t.Fatal("not exactly one complete resolution")
			}
			if _, err := p.FeedbackEnvelope(); err == nil {
				t.Fatal("resolved envelope available")
			}
			if _, _, err := p.Preview(); err == nil {
				t.Fatal("resolved preview available")
			}
		})
	}
}

func TestPreparedStopContinuationRejectsIncompleteOrMixedOwnership(t *testing.T) {
	frozen, _ := domain.NewStopFeedbackOnly(testStopFeedback(t))
	envelope, _ := NewNativeCommitEnvelope(domain.ProviderOpenAI, "responses", []byte(`{}`))
	if _, err := NewPreparedStopContinuation(frozen, envelope, func() {}, nil, testInputPreview(t, domain.ProviderOpenAI)); err == nil {
		t.Fatal("missing cleanup accepted")
	}
	if _, err := NewPreparedStopContinuation(frozen, envelope, func() {}, func() {}, InputPreview{}); err == nil {
		t.Fatal("missing preview accepted")
	}
	if _, err := NewPreparedSteeredStopContinuation(frozen, envelope, envelope, func() {}, func() {}, testInputPreview(t, domain.ProviderOpenAI)); err == nil {
		t.Fatal("fake user envelope accepted")
	}
	var zero PreparedStopContinuation
	if zero.Finalized() || zero.Finalize() == nil || zero.Discard() == nil {
		t.Fatal("zero commit accepted")
	}
	p, err := NewPreparedStopContinuation(frozen, envelope, func() { t.Fatal("discard finalized") }, func() {}, testInputPreview(t, domain.ProviderOpenAI))
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Discard(); err != nil {
		t.Fatal(err)
	}
	if p.Finalized() || p.Finalize() == nil {
		t.Fatal("discarded feedback committed")
	}
}
