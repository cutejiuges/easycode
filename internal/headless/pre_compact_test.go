package headless

import (
	"bytes"
	"strings"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/protocol"
)

func TestStreamCompactionHookOutputsOnlyDurableFailure(t *testing.T) {
	id := domain.CompactionID("01900000-0000-7000-8000-000000000020")
	projectorValue, err := NewStreamProjector(testSessionID, testThreadID)
	if err != nil {
		t.Fatal(err)
	}
	manual := domain.ManualCompactionScope()
	phase, err := protocol.NewCompactionSummarizing(testSessionID, testThreadID, id, manual)
	if err != nil {
		t.Fatal(err)
	}
	item, err := protocol.NewCorrelatedContextItem("compact-1", phase)
	if err != nil {
		t.Fatal(err)
	}
	if wire, err := projectorValue.Project(item); err != nil || wire != nil {
		t.Fatal("private phase projected into stream v1", err)
	}
	for _, code := range []protocol.CompactionFailureCode{
		protocol.CompactionFailureHookConfigInvalid,
		protocol.CompactionFailureHookTrustRequired,
		protocol.CompactionFailureHookTrustDenied,
		protocol.CompactionFailureHookDenied,
		protocol.CompactionFailureHookFailed,
	} {
		t.Run(string(code), func(t *testing.T) {
			payload := protocol.CompactionFailedPayload{Scope: manual, Code: code, Durable: true, UsageState: "unavailable"}
			failed, err := protocol.NewCompactionFailed(testSessionID, testThreadID, id, payload)
			if err != nil {
				t.Fatal(err)
			}
			item, err := protocol.NewCorrelatedContextItem("compact-1", failed)
			if err != nil {
				t.Fatal(err)
			}
			var out bytes.Buffer
			if err := NewStreamEncoder(&out).Write(projectControlItem(t, projectorValue, item)); err != nil {
				t.Fatal(err)
			}
			want := `"code":"` + string(code) + `","durable":true,"usage_state":"unavailable"`
			if !bytes.Contains(out.Bytes(), []byte(want)) || bytes.Count(out.Bytes(), []byte("\n")) != 1 {
				t.Fatalf("invalid v1 failure %s", &out)
			}
			payload.Durable = false
			if _, err := protocol.NewCompactionFailed(testSessionID, testThreadID, id, payload); err == nil {
				t.Fatal("uncertain write claimed a hook result")
			}
		})
	}
}

func TestStreamIgnoresLocalPostCompactPresentation(t *testing.T) {
	id := domain.CompactionID("01900000-0000-7000-8000-000000000020")
	projectorValue, err := NewStreamProjector(testSessionID, testThreadID)
	if err != nil {
		t.Fatal(err)
	}
	scope := domain.ManualCompactionScope()
	digest := strings.Repeat("a", 64)
	assertPrivate := func(event protocol.Event, err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		item, err := protocol.NewCorrelatedContextItem("compact-1", event)
		if err != nil {
			t.Fatal(err)
		}
		if wire, err := projectorValue.Project(item); err != nil || wire != nil {
			t.Fatalf("private %s projected into stream v1: %v, %v", event.Kind, wire, err)
		}
	}
	assertPrivate(protocol.NewPostCompactInstalled(testSessionID, testThreadID, id, protocol.PostCompactInstalledPayload{
		Scope: scope, CheckpointSequence: 3, Revision: 2, PlanDigest: digest, Count: 1,
	}))
	assertPrivate(protocol.NewPostCompactClosed(testSessionID, testThreadID, id, protocol.PostCompactClosedPayload{
		Scope: scope, CheckpointSequence: 3, PlanDigest: digest, CloseSequence: 6,
		State: protocol.PostCompactSucceeded, Code: domain.HookOK, Finished: 1,
	}))
}
