package headless

import (
	"easycode/internal/domain"
	"easycode/internal/protocol"
	"strings"
	"testing"
)

func TestPostHookUpdateIsValidatedAndNeverExternallyProjected(t *testing.T) {
	const id = "01900000-0000-7000-8000-000000000001"
	owner := newProjector(domain.SessionID(id), domain.ThreadID(id))
	started := protocol.NewTurnStarted()
	started.SessionID = domain.SessionID(id)
	started.ThreadID = domain.ThreadID(id)
	started.TurnID = domain.TurnID(id)
	if _, err := owner.project(started); err != nil {
		t.Fatal(err)
	}
	event, err := protocol.NewPostHookCommitted(protocol.PostHookCommittedPayload{InvocationID: id, Event: domain.HookPostToolUse, PlanDigest: strings.Repeat("a", 64), DetailSequence: 3, Phase: protocol.PostHookPlanned})
	if err != nil {
		t.Fatal(err)
	}
	event.SessionID = started.SessionID
	event.ThreadID = started.ThreadID
	event.TurnID = started.TurnID
	projected, err := owner.project(event)
	if err != nil || projected.text != "" || projected.terminal || projected.event != nil {
		t.Fatal("post update exposed external event", err)
	}
	event.Payload = []byte(`{"unknown":true}`)
	if _, err := owner.project(event); err == nil {
		t.Fatal("headless silently ignored malformed post update")
	}
}
