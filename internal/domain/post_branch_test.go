package domain

import (
	"bytes"
	"strings"
	"testing"

	"easycode/internal/codec"
)

func TestPostBranchesSingleOwnershipAndStrictIdentity(t *testing.T) {
	id := "018f1d8a-7b5c-7def-8abc-456789abcdef"
	branches := make([]PostHookBranch, 0, 2)
	for i, event := range []HookEvent{HookPostToolUse, HookPostToolUseFailure} {
		d, err := NewHookDeclaration(HookSourceProject, event, HookFeedbackModel, strings.Repeat("a", 64), []string{"success", "failure"}[i], 7, "true", 5)
		if err != nil {
			t.Fatal(err)
		}
		b, err := NewPostHookBranch(event, []HookReference{d.Reference}, 16<<10)
		if err != nil {
			t.Fatal(err)
		}
		branches = append(branches, b)
	}
	p, err := NewPostHookPlan(id, HookExec, branches, 256<<10, 16<<10)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := codec.MarshalStable(p)
	if bytes.Count(encoded, []byte(`"invocation_id"`)) != 1 || bytes.Count(encoded, []byte(`"capability"`)) != 1 {
		t.Fatal("duplicated call ownership")
	}
	if _, err := DecodePostHookPlan(encoded); err != nil {
		t.Fatal(err)
	}
	a, _ := NewPostHookRunIdentity(p, HookPostToolUse, 0)
	b, _ := NewPostHookRunIdentity(p, HookPostToolUseFailure, 0)
	if a.RunID == b.RunID || a.PlanDigest != b.PlanDigest {
		t.Fatal("branch identity collision")
	}
	for _, mutate := range []func(*PostHookPlan){
		func(v *PostHookPlan) { v.Branches = nil },
		func(v *PostHookPlan) { v.Branches[1] = v.Branches[0].Clone() },
		func(v *PostHookPlan) { v.Branches[0], v.Branches[1] = v.Branches[1], v.Branches[0] },
		func(v *PostHookPlan) { v.Branches[1].Hooks[0].Revision = strings.Repeat("b", 64) },
		func(v *PostHookPlan) { v.Branches[1].Hooks[0].Event = HookPostToolUse },
		func(v *PostHookPlan) { v.Reservation = 32 << 10 },
	} {
		bad := p.Clone()
		mutate(&bad)
		if bad.Validate() == nil {
			t.Fatal("invalid branch state accepted")
		}
		if p.Validate() != nil {
			t.Fatal("clone changed source")
		}
	}
	for _, key := range []string{"invocation_id", "capability", "plan_digest"} {
		bad := bytes.Replace(encoded, []byte(`"event":"PostToolUseFailure"`), []byte(`"`+key+`":"`+id+`","event":"PostToolUseFailure"`), 1)
		if _, err := DecodePostHookPlan(bad); err == nil {
			t.Fatal("duplicated branch ownership accepted", key)
		}
	}
	branchBytes, _ := codec.MarshalStable(branches[1])
	if _, err := DecodePostHookBranch(branchBytes); err != nil {
		t.Fatal(err)
	}
	budget, _ := NewPostHookBudget(p.Allowance, p.Reservation, 10, 0)
	terminal, err := NewPostHookTerminal(p, PostHookRouteNone, "execution", PostHookSkipped, HookToolNotSuccessful, 0, nil, budget, HookDigest(nil))
	if err != nil {
		t.Fatal(err)
	}
	bts, _ := codec.MarshalStable(terminal)
	if _, err := DecodePostHookTerminal(bts); err != nil {
		t.Fatal(err)
	}
	if _, err := DecodePostHookTerminal(bytes.Replace(bts, []byte(`"route":"none"`), []byte(`"route":"not_executed"`), 1)); err == nil {
		t.Fatal("superseded route accepted")
	}
}

func TestAlternateBranchNoticeSlotsAreNeverAddedTogether(t *testing.T) {
	var branches []PostHookBranch
	for e, event := range []HookEvent{HookPostToolUse, HookPostToolUseFailure} {
		var refs []HookReference
		for i := 0; i < MaxHookMatches; i++ {
			d, err := NewHookDeclaration(HookSourceProject, event, HookFeedbackModel, strings.Repeat("a", 64), string(rune('a'+e))+string(rune('a'+i)), 7, "true", 5)
			if err != nil {
				t.Fatal(err)
			}
			refs = append(refs, d.Reference)
		}
		branch, err := NewPostHookBranch(event, refs, 5000)
		if err != nil || branch.Delivery != HookDeliveryAvailable {
			t.Fatal("alternate slots summed", err)
		}
		branches = append(branches, branch)
	}
	p, err := NewPostHookPlan("018f1d8a-7b5c-7def-8abc-456789abcdef", HookExec, branches, 6024, 5000)
	if err != nil || p.Reservation != 5000 {
		t.Fatal("reservation duplicated", err)
	}
	for _, mutate := range []func(*PostHookPlan){
		func(v *PostHookPlan) { v.Branches[1].Hooks[0].ID = v.Branches[0].Hooks[0].ID },
		func(v *PostHookPlan) { v.Branches[1].Hooks[0].Revision = strings.Repeat("b", 64) },
		func(v *PostHookPlan) { v.Branches[1].Hooks = nil },
	} {
		bad := p.Clone()
		mutate(&bad)
		if _, err := NewPostHookPlan(bad.InvocationID, bad.Capability, bad.Branches, bad.Allowance, bad.Reservation); err == nil {
			t.Fatal("invalid current shape accepted by constructor")
		}
	}
}

func TestUnselectedTerminalCannotInventFeedbackBudgetOrDigest(t *testing.T) {
	d, _ := NewHookDeclaration(HookSourceProject, HookPostToolUseFailure, HookFeedbackModel, strings.Repeat("a", 64), "diagnose", 7, "true", 5)
	branch, _ := NewPostHookBranch(d.Reference.Event, []HookReference{d.Reference}, 16<<10)
	p, err := NewPostHookPlan("018f1d8a-7b5c-7def-8abc-456789abcdef", HookExec, []PostHookBranch{branch}, 256<<10, 16<<10)
	if err != nil {
		t.Fatal(err)
	}
	for _, route := range []PostHookRoute{PostHookRouteNone, PostHookRouteSuccess} {
		code := HookToolNotSuccessful
		if route == PostHookRouteSuccess {
			code = HookEventUnmatched
		}
		for _, fakeBudget := range []bool{false, true} {
			actual, feedbackDigest := 0, HookDigest([]byte("invented feedback"))
			if fakeBudget {
				actual = 1
				feedbackDigest = HookDigest(nil)
			}
			budget, _ := NewPostHookBudget(p.Allowance, p.Reservation, 20, actual)
			if _, err := NewPostHookTerminal(p, route, "execution", PostHookSkipped, code, 0, nil, budget, feedbackDigest); err == nil {
				t.Fatal("unselected terminal invented feedback")
			}
		}
	}
}
