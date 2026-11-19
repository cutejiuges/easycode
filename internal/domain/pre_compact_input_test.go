package domain

import (
	"bytes"
	"strings"
	"testing"

	"easycode/internal/codec"
)

func TestPreCompactInputPreservesScopedWire(t *testing.T) {
	const session = SessionID("018f1d8a-7b5c-7def-8abc-456789abcdef")
	const thread = ThreadID("018f1d8a-7b5c-7def-8abc-456789abcdee")
	const compaction = CompactionID("018f1d8a-7b5c-7def-8abc-456789abcded")
	const turn = TurnID("018f1d8a-7b5c-7def-8abc-456789abcdec")
	auto, err := NewAutoCompactionScope(turn, 0)
	if err != nil {
		t.Fatal(err)
	}
	workspace := t.TempDir()
	encodedWorkspace, err := codec.MarshalStable(workspace)
	if err != nil {
		t.Fatal(err)
	}
	const prefix = `{"hook_event_name":"PreCompact","session_id":"018f1d8a-7b5c-7def-8abc-456789abcdef","thread_id":"018f1d8a-7b5c-7def-8abc-456789abcdee","compaction_id":"018f1d8a-7b5c-7def-8abc-456789abcded","cwd":`
	for _, tc := range []struct {
		name  string
		scope CompactionScope
		focus string
		want  string
	}{
		{"manual", ManualCompactionScope(), "  保留测试\n", prefix + string(encodedWorkspace) + `,"trigger":"manual","custom_instructions":"保留测试"}`},
		{"automatic", auto, "", prefix + string(encodedWorkspace) + `,"trigger":"auto","custom_instructions":null,"turn_id":"018f1d8a-7b5c-7def-8abc-456789abcdec","sample_index":0}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input, err := NewPreCompactHookInput(session, thread, compaction, tc.scope, workspace, tc.focus)
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := input.Bytes()
			if err != nil || string(encoded) != tc.want {
				t.Fatalf("stdin = %q, error: %v", encoded, err)
			}
			decoded, err := DecodeHookInput(encoded)
			if err != nil {
				t.Fatal(err)
			}
			target, err := NewPreCompactHookTarget(compaction, tc.scope)
			if err != nil {
				t.Fatal(err)
			}
			targetBytes, err := codec.MarshalStable(target)
			if err != nil {
				t.Fatal(err)
			}
			target, err = DecodeHookTarget(targetBytes)
			if err != nil || !decoded.MatchesTarget(target) || target.ValidFor(HookStop) {
				t.Fatalf("target round trip lost scope: %v", err)
			}
			another, err := NewPreCompactHookTarget(CompactionID(thread), tc.scope)
			if err != nil || decoded.MatchesTarget(another) {
				t.Fatal("accepted stdin for another compaction", err)
			}
			for _, illegal := range [][]byte{
				bytes.Replace(encoded, []byte(`"custom_instructions":`), []byte(`"instructions":`), 1),
				append(append([]byte(nil), encoded[:len(encoded)-1]...), []byte(`,"transcript_path":"private"}`)...),
			} {
				if _, err := DecodeHookInput(illegal); err == nil {
					t.Fatal("accepted fields from another stdin protocol")
				}
			}
		})
	}
	if _, err := NewPreCompactHookInput(session, thread, compaction, auto, workspace, "pending input"); err != HookConfigInvalid {
		t.Fatalf("automatic compaction admitted user focus: %v", err)
	}
	if _, err := NewPreCompactHookInput(session, thread, compaction, ManualCompactionScope(), workspace, strings.Repeat("a", MaxCompactionInstructionsBytes+1)); err != HookInputLimit {
		t.Fatalf("oversized focus error = %v", err)
	}
}
