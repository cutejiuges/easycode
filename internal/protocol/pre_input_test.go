package protocol

import (
	"bytes"
	"strings"
	"testing"
	"unicode/utf8"

	"easycode/internal/codec"
	"easycode/internal/domain"
)

func TestPreInputActivityStrictBranchesAndEncodedBounds(t *testing.T) {
	const id = "01900000-0000-7000-8000-000000000001"
	d, _ := domain.NewHookDeclaration(domain.HookSourceProject, domain.HookPreToolUse, domain.HookFeedbackNone, strings.Repeat("a", 64), "rewrite", 7, "true", 5)
	for _, kind := range []PreInputKind{PreInputNone, PreInputCandidate, PreInputSelected, PreInputRejected} {
		projection := PreInputProjection{}
		if kind != PreInputNone {
			var err error
			projection, err = NewPreInputProjection(kind, 999, strings.Repeat("b", 64), strings.Repeat("\x01", 159)+"…")
			if err != nil {
				t.Fatal(err)
			}
		}
		state, code := HookActivityChecking, domain.HookOK
		if kind == PreInputSelected {
			state = HookActivityState(domain.HookContinue)
		}
		if kind == PreInputRejected {
			state, code = HookActivityState(domain.HookBlock), domain.HookTrustDenied
		}
		activity := HookActivityPayload{Target: testHookTarget(t, id, domain.HookWrite), PlanDigest: strings.Repeat("b", 64), Reference: d.Reference, State: state, Code: code, PreInput: projection}
		e, err := NewHookActivity(activity)
		if err != nil {
			t.Fatal(err)
		}
		p, err := DecodeHookActivity(e)
		if err != nil || p != activity {
			t.Fatal("projection lost current typed value", err)
		}
		block, err := codec.MarshalStable(projection)
		if err != nil || len(block) > 4<<10 || len(e.Payload) > 16<<10 || !utf8.Valid(e.Payload) {
			t.Fatal("encoded projection exceeded bound", err)
		}
		for _, bad := range [][]byte{
			bytes.Replace(e.Payload, []byte(`"pre_input":`), []byte(`"unknown_pre_input":`), 1),
			bytes.Replace(e.Payload, []byte(`"pre_input":`+string(block)+`,`), nil, 1),
			bytes.Replace(e.Payload, []byte(`"pre_input":`+string(block)), []byte(`"pre_input":null`), 1),
		} {
			if bytes.Equal(bad, e.Payload) {
				continue
			}
			copy := e
			copy.Payload = bad
			if _, err := DecodeHookActivity(copy); err == nil {
				t.Fatal("old/null/unknown branch accepted")
			}
		}
		if kind != PreInputNone {
			for _, event := range []domain.HookEvent{domain.HookPostToolUse, domain.HookPostToolUseFailure, domain.HookStop} {
				copy := activity
				copy.Reference.Event = event
				if _, err := NewHookActivity(copy); err == nil {
					t.Fatal("pre input branch crossed events")
				}
			}
			copy := activity
			copy.PreInput.sequence = 0
			if _, err := NewHookActivity(copy); err == nil {
				t.Fatal("fake sequence accepted")
			}
			copy = activity
			copy.PreInput.summary = strings.Repeat("界", 161)
			if _, err := NewHookActivity(copy); err == nil {
				t.Fatal("oversized summary accepted")
			}
			copy = activity
			copy.PreInput.summary = "line\nbreak"
			if _, err := NewHookActivity(copy); err == nil {
				t.Fatal("multiline projection accepted")
			}
		}
	}
	for _, raw := range []string{`{"kind":"none","detail_sequence":1}`, `{"kind":"selected","detail_sequence":1,"input_digest":"` + strings.Repeat("a", 64) + `"}`, `{"kind":"future"}`, `{"kind":"candidate","detail_sequence":1,"input_digest":"` + strings.Repeat("a", 64) + `","summary":"ok","input":{}}`} {
		var p PreInputProjection
		if codec.UnmarshalRecord([]byte(raw), &p) == nil {
			t.Fatal("open projection shape accepted", raw)
		}
	}
}

func TestPreInputCannotHideInOtherEventsOrNullTranscriptMembers(t *testing.T) {
	const id = "01900000-0000-7000-8000-000000000001"
	d, _ := domain.NewHookDeclaration(domain.HookSourceProject, domain.HookPostToolUse, domain.HookFeedbackNone, strings.Repeat("a", 64), "check", 7, "true", 5)
	p := HookActivityPayload{Target: testHookTarget(t, id, domain.HookWrite), PlanDigest: strings.Repeat("b", 64), Reference: d.Reference, State: HookActivityCompleted, Code: domain.HookOK}
	e, err := NewHookActivity(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"null", `{"kind":"none"}`} {
		copy := e
		copy.Payload = append([]byte(`{"pre_input":`+value+`,`), e.Payload[1:]...)
		if _, err := DecodeHookActivity(copy); err == nil {
			t.Fatal("post activity accepted an unrelated pre branch")
		}
	}
	item := TranscriptListItem{Cursor: TranscriptCursor{Sequence: 1}, Kind: domain.TranscriptCall, TurnID: domain.TurnID(id), Activity: ToolActivityPayload{Tool: ToolWrite, InvocationID: id, Summary: "target", Status: "waiting"}}
	encoded, err := codec.MarshalStable(item)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"pre_hook", "post_hook"} {
		bad := append([]byte(`{"`+name+`":null,`), encoded[1:]...)
		var decoded TranscriptListItem
		if codec.UnmarshalRecord(bad, &decoded) == nil {
			t.Fatal("null activity impersonated an absent transcript branch")
		}
	}
}
