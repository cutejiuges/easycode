package protocol

import (
	"bytes"
	"strings"
	"testing"

	"easycode/internal/codec"
	"easycode/internal/domain"
)

func TestStopTrustUsesActualSampleAndPlan(t *testing.T) {
	const uuid = "01900000-0000-7000-8000-000000000001"
	sample, _ := domain.NewAssistantSampleReference(domain.TurnID(uuid), 2)
	target, _ := domain.NewStopHookTarget(sample)
	for _, command := range []string{"true", strings.Repeat("<", domain.MaxHookCommandBytes)} {
		d, err := domain.NewStopHookDeclaration(domain.HookSourceProject, domain.HookFeedbackModel, strings.Repeat("a", 64), "stop", command, 30)
		if err != nil {
			t.Fatal(err)
		}
		p := HookTrustRequestedPayload{RequestID: uuid, Target: target, PlanDigest: strings.Repeat("b", 64), Declaration: d}
		e, err := NewHookTrustRequested(p)
		if err != nil {
			t.Fatal(err)
		}
		e.SessionID, e.ThreadID, e.TurnID = domain.SessionID(uuid), domain.ThreadID(uuid), domain.TurnID(uuid)
		if err := e.Validate(); err != nil {
			t.Fatal("full Stop review rejected", err)
		}
		got, err := DecodeHookTrustRequested(e)
		if err != nil || got != p || got.InvocationID() != "" {
			t.Fatal("Stop review acquired a tool identity", err)
		}
		for _, decision := range []HookTrustDecision{HookTrustDeny, HookTrustSession} {
			r, err := NewHookTrustResponse(domain.SessionID(uuid), domain.ThreadID(uuid), domain.TurnID(uuid), p, decision)
			if err != nil {
				t.Fatal(err)
			}
			b, err := codec.MarshalStable(r)
			if err != nil {
				t.Fatal(err)
			}
			cmd, err := DecodeResolveHookTrustCommand("resolve", b)
			if err != nil || cmd.HookTrustResponse() != r {
				t.Fatal("response round trip", err)
			}
			if _, err := DecodeResolveHookTrustCommand("resolve", bytes.Replace(b, []byte(`"target":`), []byte(`"invocation_id":"`+uuid+`","target":`), 1)); err == nil {
				t.Fatal("old identity field accepted")
			}
			other := r
			other.TurnID = "01900000-0000-7000-8000-000000000002"
			if other.Validate() == nil {
				t.Fatal("cross turn Stop response accepted")
			}
		}
		wrong := p
		wrong.Target = testHookTarget(t, uuid, domain.HookExec)
		if wrong.Validate() == nil {
			t.Fatal("tool target accepted for Stop")
		}
		wrong = p
		wrong.PlanDigest = ""
		if wrong.Validate() == nil {
			t.Fatal("unbound plan accepted")
		}
		wrong = p
		wrong.Declaration.Scope, _ = domain.NewToolHookScope(7)
		if wrong.Validate() == nil {
			t.Fatal("Stop matcher accepted")
		}
		e.TurnID = "01900000-0000-7000-8000-000000000002"
		if _, err := DecodeHookTrustRequested(e); err == nil {
			t.Fatal("outer scope mismatch accepted")
		}
	}
}

func TestStopActivityDoesNotPretendToBeATool(t *testing.T) {
	const uuid = "01900000-0000-7000-8000-000000000001"
	d, err := domain.NewStopHookDeclaration(domain.HookSourceUser, domain.HookFeedbackNone, strings.Repeat("a", 64), "check", "true", 5)
	if err != nil {
		t.Fatal(err)
	}
	sample, _ := domain.NewAssistantSampleReference(domain.TurnID(uuid), 0)
	target, _ := domain.NewStopHookTarget(sample)
	p := HookActivityPayload{Target: target, PlanDigest: strings.Repeat("b", 64), Reference: d.Reference, State: HookActivityChecking, Code: domain.HookOK}
	e, err := NewHookActivity(p)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeHookActivity(e)
	if err != nil || got != p || got.InvocationID() != "" {
		t.Fatal("activity changed identity", err)
	}
	for _, field := range []string{"invocation_id", "stdout", "schema_version"} {
		bad := e
		bad.Payload = append(bytes.TrimSuffix(bytes.Clone(e.Payload), []byte("}")), []byte(`,"`+field+`":"forged"}`)...)
		if _, err := DecodeHookActivity(bad); err == nil {
			t.Fatal("unexpected field accepted", field)
		}
	}
	e.TurnID = "01900000-0000-7000-8000-000000000002"
	if _, err := DecodeHookActivity(e); err == nil {
		t.Fatal("cross turn activity accepted")
	}
}
