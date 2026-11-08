package protocol

import (
	"bytes"
	"strings"
	"testing"

	"easycode/internal/codec"
	"easycode/internal/domain"
)

func TestHookTrustContractsAreStrictAndIdentityBound(t *testing.T) {
	const id = "01900000-0000-7000-8000-000000000001"
	d, err := domain.NewHookDeclaration(domain.HookSourceProject, domain.HookPreToolUse, domain.HookFeedbackNone, strings.Repeat("a", 64), "check", 7, "cat >/dev/null", 5)
	if err != nil {
		t.Fatal(err)
	}
	p := HookTrustRequestedPayload{RequestID: id, InvocationID: id, Capability: domain.HookPatch, Declaration: d}
	requested, err := NewHookTrustRequested(p)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := DecodeHookTrustRequested(requested); err != nil || got != p {
		t.Fatal(got, err)
	}
	for _, decision := range []HookTrustDecision{HookTrustDeny, HookTrustSession} {
		response, err := NewHookTrustResponse(domain.SessionID(id), domain.ThreadID(id), domain.TurnID(id), p, decision)
		if err != nil {
			t.Fatal(err)
		}
		command, err := NewResolveHookTrustCommand("resolve", response)
		if err != nil {
			t.Fatal(err)
		}
		encoded, _ := codec.MarshalStable(response)
		decoded, err := DecodeResolveHookTrustCommand("resolve", encoded)
		if err != nil || decoded.HookTrustResponse() != command.HookTrustResponse() {
			t.Fatal(err)
		}
		event, err := NewHookTrustResolved(response)
		if err != nil {
			t.Fatal(err)
		}
		if got, err := DecodeHookTrustResolved(event); err != nil || got != response {
			t.Fatal(err)
		}
		for _, bad := range [][]byte{[]byte(`{}`), bytes.Replace(encoded, []byte(`"decision":"`+string(decision)+`"`), []byte(`"decision":"allow"`), 1), append(append([]byte{}, encoded[:len(encoded)-1]...), []byte(`,"feedback":"secret"}`)...)} {
			if _, err := DecodeResolveHookTrustCommand("resolve", bad); err == nil {
				t.Fatal("invalid trust accepted")
			}
		}
	}
	target, err := NewHookTrustTarget(domain.SessionID(id), domain.ThreadID(id), d.Reference)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := codec.MarshalStable(target)
	if c, err := DecodeResetHookTrustCommand("reset", b); err != nil || c.HookTrustTarget() != target {
		t.Fatal(err)
	}
	if _, err := NewHookTrustResponse(domain.SessionID(id), domain.ThreadID(id), domain.TurnID(id), p, ""); err == nil {
		t.Fatal("implicit grant accepted")
	}
	for _, state := range []string{"checking", "continue", "denied"} {
		code := domain.HookOK
		if state == "denied" {
			code = domain.HookDenied
		}
		p := HookActivityPayload{InvocationID: id, Reference: d.Reference, State: HookActivityState(state), Code: code}
		e, err := NewHookActivity(p)
		if err != nil {
			t.Fatal(err)
		}
		if got, err := DecodeHookActivity(e); err != nil || got != p {
			t.Fatal(err)
		}
		e.Payload = append(append([]byte{}, e.Payload[:len(e.Payload)-1]...), []byte(`,"stdout":"secret"}`)...)
		if _, err := DecodeHookActivity(e); err == nil {
			t.Fatal("raw activity output accepted")
		}
	}
}
