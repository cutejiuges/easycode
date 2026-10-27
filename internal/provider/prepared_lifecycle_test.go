package provider

import (
	"sync"
	"sync/atomic"
	"testing"

	"easycode/internal/domain"
)

func TestPreparedResolutionCompletesCallbackBeforeFinalized(t *testing.T) {
	envelope, err := NewNativeCommitEnvelope(domain.ProviderOpenAI, "responses", 1, []byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, outputs := range []bool{false, true} {
		entered, release := make(chan struct{}), make(chan struct{})
		var calls atomic.Int32
		finalize := func() { calls.Add(1); close(entered); <-release }
		discard := func() { calls.Add(1) }
		var resolve func() error
		var reject func() error
		var finalized func() bool
		if outputs {
			value, err := NewPreparedToolOutputs(envelope, finalize, discard)
			if err != nil {
				t.Fatal(err)
			}
			resolve = value.Finalize
			reject = value.Discard
			finalized = value.Finalized
		} else {
			value, err := NewPreparedSample(envelope, testSampleUsage(t), finalize, discard)
			if err != nil {
				t.Fatal(err)
			}
			resolve = value.Finalize
			reject = value.Discard
			finalized = value.Finalized
		}
		done := make(chan error, 1)
		go func() { done <- resolve() }()
		<-entered
		if finalized() {
			t.Fatal("published completion before callback finished")
		}
		rejected := make(chan error, 1)
		go func() { rejected <- reject() }()
		select {
		case <-rejected:
			t.Fatal("competing resolution completed before callback")
		default:
		}
		close(release)
		if err := <-rejected; err == nil {
			t.Fatal("discard raced with owned finalize")
		}
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		if !finalized() || calls.Load() != 1 {
			t.Fatal("callback did not complete exactly once")
		}
	}
}

func TestPreparedFinalizeDiscardCompetition(t *testing.T) {
	envelope, err := NewNativeCommitEnvelope(domain.ProviderOpenAI, "responses", 1, []byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, outputs := range []bool{false, true} {
		var calls, success atomic.Int32
		callback := func() { calls.Add(1) }
		var finalize, discard func() error
		if outputs {
			value, err := NewPreparedToolOutputs(envelope, callback, callback)
			if err != nil {
				t.Fatal(err)
			}
			finalize = value.Finalize
			discard = value.Discard
		} else {
			value, err := NewPreparedSample(envelope, testSampleUsage(t), callback, callback)
			if err != nil {
				t.Fatal(err)
			}
			finalize = value.Finalize
			discard = value.Discard
		}
		barrier := make(chan struct{})
		var wait sync.WaitGroup
		for i := range 32 {
			wait.Add(1)
			go func() {
				defer wait.Done()
				<-barrier
				operation := finalize
				if i%2 == 0 {
					operation = discard
				}
				if operation() == nil {
					success.Add(1)
				}
			}()
		}
		close(barrier)
		wait.Wait()
		if calls.Load() != 1 || success.Load() != 1 {
			t.Fatal("lifecycle resolved more than once")
		}
	}
	if _, err := NewPreparedSample(envelope, testSampleUsage(t), func() {}, nil); err == nil {
		t.Fatal("missing discard accepted")
	}
	if _, err := NewPreparedToolOutputs(envelope, func() {}, nil); err == nil {
		t.Fatal("missing discard accepted")
	}
	var sample PreparedSample
	var outputs PreparedToolOutputs
	if sample.Discard() == nil || outputs.Discard() == nil {
		t.Fatal("zero value discarded")
	}
}
