package domain

import (
	"bytes"
	"easycode/internal/codec"
	"strings"
	"testing"
)

func TestPostCurrentCodecsRejectIncompleteAndAmbiguousObjects(t *testing.T) {
	declaration, err := NewHookDeclaration(HookSourceProject, HookPostToolUse, HookFeedbackModel, strings.Repeat("a", 64), "check", 7, "true", 5)
	if err != nil {
		t.Fatal(err)
	}
	plan, _ := NewHookPlan("018f1d8a-7b5c-7def-8abc-456789abcdef", HookExec, []HookReference{declaration.Reference})
	currentPostBranch1, _ := NewPostHookBranch(plan.Event, plan.Hooks, 16<<10)
	currentPostBranch1.Delivery = HookDeliveryAvailable
	post, _ := NewPostHookPlan(plan.InvocationID, plan.Capability, []PostHookBranch{currentPostBranch1}, 256<<10, 16<<10)
	id, _ := NewPostHookRunIdentity(post, HookPostToolUse, 0)
	stats := HookRunResult{Identity: id, Status: HookContinue, Code: HookOK, ExitKnown: true, CleanupConfirmed: true, Origin: "execution", StdoutBytes: 100}
	feedback, _ := NewFrozenHookFeedback(id, declaration.Reference, HookFeedbackContent, "explicit body", HookOK, 100)
	finished, _ := NewHookFinished(stats, feedback)
	budget, _ := NewPostHookBudget(post.Allowance, post.Reservation, 20, 100)
	terminal, _ := NewPostHookTerminal(post, PostHookRouteSuccess, "execution", PostHookCompleted, HookOK, 1, nil, budget, HookDigest([]byte("explicit body")))
	planBytes, _ := codec.MarshalStable(post)
	feedbackBytes, _ := codec.MarshalStable(feedback)
	finishedBytes, _ := codec.MarshalStable(finished)
	terminalBytes, _ := codec.MarshalStable(terminal)
	for _, test := range []struct {
		name   string
		data   []byte
		decode func([]byte) error
	}{
		{"plan", planBytes, func(b []byte) error { _, err := DecodePostHookPlan(b); return err }},
		{"feedback", feedbackBytes, func(b []byte) error { _, err := DecodeFrozenHookFeedback(b); return err }},
		{"finished", finishedBytes, func(b []byte) error { _, err := DecodeHookFinished(b); return err }},
		{"terminal", terminalBytes, func(b []byte) error { _, err := DecodePostHookTerminal(b); return err }},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := test.decode(test.data); err != nil {
				t.Fatal(err)
			}
			members, err := codec.ObjectMembers(test.data)
			if err != nil {
				t.Fatal(err)
			}
			for key := range members {
				missing := bytes.Replace(test.data, []byte(`"`+key+`":`), []byte(`"unknown_`+key+`":`), 1)
				if err := test.decode(missing); err == nil {
					t.Fatal("missing field accepted", key)
				}
				duplicate := append([]byte(`{"`+key+`":null,`), test.data[1:]...)
				if err := test.decode(duplicate); err == nil {
					t.Fatal("duplicate/null accepted", key)
				}
			}
			for _, invalid := range [][]byte{append(bytes.TrimSuffix(test.data, []byte("}")), []byte(`,"stdout":"secret-sentinel"}`)...), append(bytes.Clone(test.data), []byte(` {}`)...), []byte("null")} {
				if err := test.decode(invalid); err == nil {
					t.Fatal("unbounded current object accepted")
				}
			}
		})
	}
	mutated := post
	mutated.Allowance -= 1024
	if mutated.Validate() == nil {
		t.Fatal("budget mutation preserved plan identity")
	}
	if _, err := NewHookFeedbackCandidate(strings.Repeat("x", MaxHookFeedbackBytes+1)); err == nil {
		t.Fatal("oversized candidate accepted")
	}
	if _, err := NewHookFeedbackCandidate("secret\x00sentinel"); err == nil {
		t.Fatal("NUL candidate accepted")
	}
}
