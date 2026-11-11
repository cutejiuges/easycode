package domain

import (
	"bytes"
	"strings"
	"testing"

	"easycode/internal/codec"
)

func TestFailureHookInputStrictProjection(t *testing.T) {
	id := "018f1d8a-7b5c-7def-8abc-456789abcdef"
	patch, _ := NewHookPatchInput("a.go", []HookReplacement{{OldText: "old", NewText: "new"}})
	write, _ := NewHookWriteInput("a.go", "new")
	exec, _ := NewHookExecInput("true", ".", 1000)
	for _, test := range []struct {
		name  string
		input HookToolInput
	}{{"Edit", patch}, {"apply_patch", patch}, {"Write", write}, {"Bash", exec}, {"exec_command", exec}} {
		v, err := NewFailureHookInput(SessionID(id), ThreadID(id), TurnID(id), "call", test.name, "/workspace", test.input, "timed_out")
		if err != nil {
			t.Fatal(err)
		}
		b, err := v.Bytes()
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := DecodeHookInput(b)
		if err != nil || decoded.Event != HookPostToolUseFailure || decoded.Error != "Tool execution failed: timed_out" || decoded.IsInterrupt == nil || *decoded.IsInterrupt {
			t.Fatal("failure projection", err)
		}
		members, _ := codec.ObjectMembers(b)
		for key := range members {
			bad := bytes.Replace(b, []byte(`"`+key+`":`), []byte(`"unknown_`+key+`":`), 1)
			if _, err := DecodeHookInput(bad); err == nil {
				t.Fatal("missing field accepted", key)
			}
			bad = append([]byte(`{"`+key+`":null,`), b[1:]...)
			if _, err := DecodeHookInput(bad); err == nil {
				t.Fatal("duplicate field accepted", key)
			}
		}
		for _, bad := range [][]byte{
			bytes.Replace(b, []byte(`"is_interrupt":false`), []byte(`"is_interrupt":true`), 1),
			bytes.Replace(b, []byte(`"is_interrupt":false`), []byte(`"is_interrupt":null`), 1),
			bytes.Replace(b, []byte(`"is_interrupt":false`), []byte(`"tool_response":{}`), 1),
			append(bytes.Clone(b), []byte(` {}`)...),
		} {
			if _, err := DecodeHookInput(bad); err == nil {
				t.Fatal("invalid failure shape accepted")
			}
		}
	}
	v, err := NewFailureHookInput(SessionID(id), ThreadID(id), TurnID(id), "call", "Bash", "/workspace", exec, "secret-sentinel\n/private/path")
	if err != nil || v.Error != "Tool execution failed: tool_execution_failed" {
		t.Fatal("unsafe code projection", err)
	}
	b, _ := v.Bytes()
	if bytes.Contains(b, []byte("secret-sentinel")) {
		t.Fatal("raw error leaked")
	}
	write, _ = NewHookWriteInput("a.go", strings.Repeat("x", MaxHookInputBytes))
	if _, err := NewFailureHookInput(SessionID(id), ThreadID(id), TurnID(id), "call", "Write", "/workspace", write, "write_failed"); err != HookInputLimit {
		t.Fatal("oversized stdin accepted", err)
	}
}

func TestPostHookRoutesAndFailureDisclosure(t *testing.T) {
	for _, route := range []PostHookRoute{PostHookRouteSuccess, PostHookRouteFailure, PostHookRouteNone} {
		event, selected := route.Event()
		if !route.Valid() || selected != (route != PostHookRouteNone) || selected && !event.Post() {
			t.Fatal("invalid selection", route)
		}
	}
	if PostHookRoute("").Valid() || PostHookRoute("not_executed").Valid() {
		t.Fatal("unknown route accepted")
	}
	if !HookFeedbackModel.ValidFor(HookPostToolUseFailure) || HookFeedbackModel.ValidFor(HookPreToolUse) {
		t.Fatal("disclosure crossed event boundary")
	}
}
