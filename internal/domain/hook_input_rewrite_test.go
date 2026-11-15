package domain

import (
	"bytes"
	"strings"
	"testing"

	"easycode/internal/codec"
)

func rewriteContext(t *testing.T, profile HookInputProfile) HookRewriteContext {
	t.Helper()
	var input HookToolInput
	switch profile {
	case HookWriteProfile:
		input, _ = NewHookWriteInput("original.txt", "original")
	case HookExecProfile:
		input, _ = NewHookExecInput("original", "", 1000)
	case HookSubstringPatch:
		input, _ = NewHookPatchInput("original.txt", []HookReplacement{{OldText: "old", NewText: "new"}})
	case HookLinePatch:
		input, _ = NewHookPatchInput("original.txt", []HookReplacement{{OldText: "old\n", NewText: "new\n", LineBased: true}})
	}
	context, err := NewHookRewriteContext(input, profile)
	if err != nil {
		t.Fatal(err)
	}
	return context
}

func TestHookReplacementRequiresCompleteCurrentShape(t *testing.T) {
	for _, test := range []struct {
		name    string
		profile HookInputProfile
		body    string
		valid   bool
	}{
		{"empty write", HookWriteProfile, `{"file_path":"f","content":""}`, true},
		{"root exec", HookExecProfile, `{"command":"new","workdir":"","timeout_ms":1000}`, true},
		{"dot is not normalized root", HookExecProfile, `{"command":"new","workdir":".","timeout_ms":1000}`, false},
		{"edit", HookSubstringPatch, `{"file_path":"f","replacements":[{"old_text":"a","new_text":"b","replace_all":true,"line_based":false}]}`, true},
		{"lines", HookLinePatch, `{"file_path":"f","replacements":[{"old_text":"a\n","new_text":"","replace_all":false,"line_based":true}]}`, true},
		{"missing content", HookWriteProfile, `{"file_path":"f"}`, false},
		{"null content", HookWriteProfile, `{"file_path":"f","content":null}`, false},
		{"duplicate", HookWriteProfile, `{"file_path":"f","content":"a","content":"b"}`, false},
		{"unknown", HookWriteProfile, `{"file_path":"f","content":"a","call_id":"secret"}`, false},
		{"case", HookWriteProfile, `{"File_path":"f","content":"a"}`, false},
		{"nul", HookWriteProfile, `{"file_path":"f","content":"\u0000"}`, false},
		{"bad utf8", HookWriteProfile, "{\"file_path\":\"f\",\"content\":\"\xff\"}", false},
		{"surrogate", HookWriteProfile, `{"file_path":"f","content":"\ud800"}`, false},
		{"trailing", HookWriteProfile, `{"file_path":"f","content":"a"}{}`, false},
		{"traversal", HookWriteProfile, `{"file_path":"../f","content":"a"}`, false},
		{"missing workdir", HookExecProfile, `{"command":"new","timeout_ms":1000}`, false},
		{"missing timeout", HookExecProfile, `{"command":"new","workdir":""}`, false},
		{"zero timeout", HookExecProfile, `{"command":"new","workdir":"","timeout_ms":0}`, false},
		{"wrong capability", HookExecProfile, `{"file_path":"f","content":"a"}`, false},
		{"missing bool", HookSubstringPatch, `{"file_path":"f","replacements":[{"old_text":"a","new_text":"b","line_based":false}]}`, false},
		{"nested duplicate", HookSubstringPatch, `{"file_path":"f","replacements":[{"old_text":"a","new_text":"b","replace_all":false,"line_based":false,"line_based":true}]}`, false},
		{"null replacements", HookSubstringPatch, `{"file_path":"f","replacements":null}`, false},
		{"empty replacements", HookSubstringPatch, `{"file_path":"f","replacements":[]}`, false},
		{"unchanged hunk", HookSubstringPatch, `{"file_path":"f","replacements":[{"old_text":"a","new_text":"a","replace_all":false,"line_based":false}]}`, false},
		{"edit mode switch", HookSubstringPatch, `{"file_path":"f","replacements":[{"old_text":"a\n","new_text":"b\n","replace_all":false,"line_based":true}]}`, false},
		{"line mode switch", HookLinePatch, `{"file_path":"f","replacements":[{"old_text":"a","new_text":"b","replace_all":false,"line_based":false}]}`, false},
		{"line replace all", HookLinePatch, `{"file_path":"f","replacements":[{"old_text":"a\n","new_text":"b\n","replace_all":true,"line_based":true}]}`, false},
		{"line cr", HookLinePatch, `{"file_path":"f","replacements":[{"old_text":"a\r\n","new_text":"b\n","replace_all":false,"line_based":true}]}`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			v, err := DecodeHookReplacement(rewriteContext(t, test.profile), []byte(test.body))
			if (err == nil) != test.valid {
				t.Fatalf("valid=%v err=%v", test.valid, err)
			}
			if err == nil && (v.Empty() || v.Validate() != nil || v.ValidateAgainst(rewriteContext(t, test.profile)) != nil) {
				t.Fatal("accepted invalid candidate")
			}
			if err != nil && err != HookOutputInvalid {
				t.Fatal("uncontrolled error", err)
			}
		})
	}
}

func TestHookReplacementCanonicalExpansionAndExactBoundary(t *testing.T) {
	context := rewriteContext(t, HookWriteProfile)
	raw := []byte(`{"file_path":"sample.txt","content":"` + strings.Repeat("<", 12000) + `"}`)
	original, _ := NewHookWriteInput("sample.txt", strings.Repeat("<", 12000))
	canonical, _ := original.MarshalJSON()
	if len(raw) != 12039 || len(canonical) != 72039 {
		t.Fatalf("probe changed: raw=%d canonical=%d", len(raw), len(canonical))
	}
	for _, c := range []HookRewriteContext{context, func() HookRewriteContext { c, _ := NewHookRewriteContext(original, HookWriteProfile); return c }()} {
		if _, err := DecodeHookReplacement(c, raw); err != HookOutputInvalid {
			t.Fatal("expanded or no-op input accepted", err)
		}
	}
	base, _ := NewHookWriteInput("f", "")
	empty, _ := base.MarshalJSON()
	for _, extra := range []int{0, 1} {
		body := []byte(`{"file_path":"f","content":"` + strings.Repeat("x", MaxHookRewriteBytes-len(empty)+extra) + `"}`)
		_, err := DecodeHookReplacement(context, body)
		if (err == nil) != (extra == 0) {
			t.Fatal("canonical boundary", len(body), err)
		}
	}
}

func TestHookRewriteGettersAndDigestDoNotAlias(t *testing.T) {
	context := rewriteContext(t, HookSubstringPatch)
	input := context.Input()
	_, rs, _ := input.Patch()
	rs[0].NewText = "wrong"
	_, actual, _ := context.Input().Patch()
	if actual[0].NewText != "new" {
		t.Fatal("getter shared replacements")
	}
	b, _ := input.MarshalJSON()
	decoded, err := DecodeHookToolInput(HookPatch, b)
	first, _ := input.Digest()
	second, _ := decoded.Digest()
	if err != nil || !input.Equal(decoded) || first != second || !ValidHookDigest(first) {
		t.Fatal("unstable normalized identity")
	}
	c, err := codec.MarshalStable(context.Input())
	if err != nil || !bytes.Equal(b, c) {
		t.Fatal("context input changed")
	}
}
