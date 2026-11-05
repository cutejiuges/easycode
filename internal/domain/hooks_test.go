package domain

import (
	"bytes"
	"strings"
	"testing"

	"easycode/internal/codec"
)

func TestHookValuesAreBoundedImmutableAndStrict(t *testing.T) {
	m, err := ParseHookMatcher("Bash|exec_command|Write")
	if err != nil || !m.Matches(HookExec) || m.Matches(HookPatch) {
		t.Fatal("alias matching failed")
	}
	for _, s := range []string{"", "Bash|", "Read", "Bash|Bash", ".*", "write"} {
		if _, err := ParseHookMatcher(s); err == nil {
			t.Fatalf("accepted matcher %q", s)
		}
	}
	d, err := NewHookDeclaration(HookSourceUser, strings.Repeat("a", 64), "check", m, "printf '{}'", 5)
	if err != nil || d.Validate() != nil {
		t.Fatal(err)
	}
	s, _ := NewHookSnapshot([]HookDeclaration{d}, nil)
	out := s.Declarations()
	out[0].Command = "changed"
	if s.Declarations()[0] != d {
		t.Fatal("snapshot shared mutable data")
	}
	p, err := NewHookPlan("018f1d8a-7b5c-7def-8abc-456789abcdef", HookExec, []HookReference{d.Reference})
	if err != nil {
		t.Fatal(err)
	}
	q := p.Clone()
	q.Hooks[0].ID = "changed"
	if p.Validate() != nil || q.Validate() == nil {
		t.Fatal("plan alias or digest mismatch accepted")
	}
	b, _ := codec.MarshalStable(p)
	if _, err := DecodeHookPlan(b); err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeHookPlan(bytes.Replace(b, []byte(`"event":`), []byte(`"version":1,"event":`), 1)); err == nil {
		t.Fatal("unknown field accepted")
	}
	run, _ := NewHookRunIdentity(p, 0)
	r, err := NewHookRunResult(HookRunResult{Identity: run, Status: HookContinue, Code: HookOK, ExitKnown: true, CleanupConfirmed: true, Origin: "execution"})
	if err != nil {
		t.Fatal(err)
	}
	r.CleanupConfirmed = false
	if r.Validate() == nil {
		t.Fatal("unconfirmed success accepted")
	}
	r = HookRunResult{Identity: run, Status: HookUncertain, Code: HookOutcomeUncertain, Origin: "recovery"}
	if r.Validate() != nil {
		t.Fatal("recovery uncertainty rejected")
	}
	r.ExitKnown = true
	if r.Validate() == nil {
		t.Fatal("recovery invented exit")
	}
	g, _ := NewHookGateDecision(p, HookBlock, HookTrustDenied, 0, 0)
	gc := g.Clone()
	*gc.BlockingOrdinal = 7
	if *g.BlockingOrdinal != 0 {
		t.Fatal("decision shared ordinal")
	}
}

func TestHookInputDoesNotEvaluateOrTruncateArguments(t *testing.T) {
	v, err := NewHookExecInput("printf '$SECRET $(touch outside)'", ".", 1000)
	if err != nil {
		t.Fatal(err)
	}
	id := "018f1d8a-7b5c-7def-8abc-456789abcdef"
	i, err := NewHookInput(SessionID(id), ThreadID(id), TurnID(id), "call", "exec_command", "/workspace", v)
	if err != nil {
		t.Fatal(err)
	}
	b, err := i.Bytes()
	if err != nil || !bytes.Contains(b, []byte("$SECRET $(touch outside)")) {
		t.Fatal("input altered", err)
	}
	if _, err := NewHookWriteInput("file", strings.Repeat("x", MaxHookInputBytes)); err != HookInputLimit {
		t.Fatal("oversized input not rejected")
	}
	if _, err := NewHookWriteInput("../outside", "secret"); err == nil {
		t.Fatal("unsafe path accepted")
	}
	if _, err := DecodeHookToolInput(HookWrite, []byte(`{"file_path":"f","content":"x","extra":true}`)); err == nil {
		t.Fatal("unknown input field accepted")
	}
	p, _ := NewHookPatchInput("f", []HookReplacement{{OldText: "a", NewText: "b"}})
	copy := p.Clone()
	copy.patch.Replacements[0].OldText = "wrong"
	if p.patch.Replacements[0].OldText != "a" {
		t.Fatal("replacement shared")
	}
}

func TestHookInputBranchesAndMatchLimit(t *testing.T) {
	id := "018f1d8a-7b5c-7def-8abc-456789abcdef"
	patch, _ := NewHookPatchInput("f", []HookReplacement{{OldText: "a", NewText: "b", LineBased: true}})
	write, _ := NewHookWriteInput("f", "body")
	exec, _ := NewHookExecInput("echo hello", ".", 1000)
	for _, test := range []struct {
		input HookToolInput
		names []string
	}{{patch, []string{"Edit", "apply_patch"}}, {write, []string{"Write"}}, {exec, []string{"Bash", "exec_command"}}} {
		b, err := test.input.MarshalJSON()
		if err != nil {
			t.Fatal(err)
		}
		restored, err := DecodeHookToolInput(test.input.Capability(), b)
		if err != nil {
			t.Fatal(err)
		}
		for _, name := range test.names {
			input, err := NewHookInput(SessionID(id), ThreadID(id), TurnID(id), "call", name, "/workspace", restored)
			if err != nil {
				t.Fatal(err)
			}
			data, err := input.Bytes()
			if err != nil || !bytes.Contains(data, []byte(`"tool_name":"`+name+`"`)) {
				t.Fatal("facade lost", err)
			}
		}
	}
	var declarations []HookDeclaration
	for n := range 9 {
		d, err := NewHookDeclaration(HookSourceUser, strings.Repeat("a", 64), "check-"+string(rune('a'+n)), 7, "true", 5)
		if err != nil {
			t.Fatal(err)
		}
		declarations = append(declarations, d)
	}
	s, _ := NewHookSnapshot(declarations, nil)
	if _, err := s.Match(HookExec); err != HookMatchLimit {
		t.Fatal("matched too many hooks", err)
	}
}

func TestHookExecutionStatisticsRejectContradictoryFlags(t *testing.T) {
	d, _ := NewHookDeclaration(HookSourceProject, strings.Repeat("a", 64), "check", 7, "true", 5)
	p, _ := NewHookPlan("018f1d8a-7b5c-7def-8123-456789abcdef", HookPatch, []HookReference{d.Reference})
	id, _ := NewHookRunIdentity(p, 0)
	base := HookRunResult{Identity: id, Status: HookContinue, Code: HookOK, ExitKnown: true, CleanupConfirmed: true, Origin: "execution"}
	for _, mode := range []string{"cancelled-continue", "timeout-continue", "denied-exit", "exit-failed-zero", "invalid-output-exit", "started-exit", "recovery-output"} {
		r := base
		switch mode {
		case "cancelled-continue":
			r.Cancelled = true
		case "timeout-continue":
			r.TimedOut = true
		case "denied-exit":
			r.Status = HookBlock
			r.Code = HookDenied
			r.ExitCode = 7
		case "exit-failed-zero":
			r.Status = HookError
			r.Code = HookExitFailed
		case "invalid-output-exit":
			r.Status = HookError
			r.Code = HookOutputInvalid
			r.ExitCode = 2
		case "started-exit":
			r.Status = HookError
			r.Code = HookStartFailed
		case "recovery-output":
			r.Status = HookUncertain
			r.Code = HookOutcomeUncertain
			r.Origin = "recovery"
			r.ExitKnown = false
			r.CleanupConfirmed = false
			r.StdoutBytes = 1
		}
		if r.Validate() == nil {
			t.Fatal("contradictory execution accepted", mode)
		}
	}
}

func TestHookInputUsesNativeAbsoluteWorkspacePath(t *testing.T) {
	id := "018f1d8a-7b5c-7def-8123-456789abcdef"
	input, _ := NewHookWriteInput("marker", "content")
	v, err := NewHookInput(SessionID(id), ThreadID(id), TurnID(id), "call", "Write", t.TempDir(), input)
	if err != nil || v.Validate() != nil {
		t.Fatal("native absolute workspace rejected", err)
	}
	v.CWD = "relative/workspace"
	if v.Validate() == nil {
		t.Fatal("relative workspace accepted")
	}
}
