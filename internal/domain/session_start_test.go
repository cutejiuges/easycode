package domain

import (
	"bytes"
	"strings"
	"testing"

	"easycode/internal/codec"
)

func TestSessionStartHookInputKeepsSourceBranchesSeparate(t *testing.T) {
	const session = SessionID("018f1d8a-7b5c-7def-8abc-456789abcdef")
	const thread = ThreadID("018f1d8a-7b5c-7def-8abc-456789abcdee")
	const turn = TurnID("018f1d8a-7b5c-7def-8abc-456789abcdec")
	digest := HookDigest([]byte("installed"))
	cwd := t.TempDir()
	for _, anchor := range []SessionStartTarget{
		{Source: SessionStartStartup, TurnID: turn},
		{Source: SessionStartResume, TurnID: turn},
		{Source: SessionStartCompact, CompactionID: CompactionID(session), Scope: ManualCompactionScope(), CheckpointSequence: 9, CheckpointRevision: 4, CheckpointDigest: digest},
		{Source: SessionStartCompact, CompactionID: CompactionID(session), Scope: CompactionScope{Trigger: CompactionAuto, TurnID: turn}, CheckpointSequence: 9, CheckpointRevision: 4, CheckpointDigest: digest},
	} {
		target, err := NewSessionStartHookTarget(anchor)
		if err != nil {
			t.Fatal(err)
		}
		payload, err := NewSessionStartHookInput(session, thread, anchor, cwd)
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := payload.Bytes()
		if err != nil {
			t.Fatal(err)
		}
		fields, err := codec.ObjectMembers(encoded)
		if err != nil {
			t.Fatal(err)
		}
		compact, auto := anchor.Source == SessionStartCompact, anchor.Scope.Trigger == CompactionAuto
		if string(fields["source"]) != "\""+string(anchor.Source)+"\"" || (fields["turn_id"] != nil) != (!compact || auto) ||
			(fields["compaction_id"] != nil) != compact || (fields["trigger"] != nil) != compact || (fields["sample_index"] != nil) != auto ||
			auto && string(fields["sample_index"]) != "0" || bytes.Contains(encoded, []byte("checkpoint_")) {
			t.Fatalf("invalid fields for %v: %s", anchor.Source, encoded)
		}
		decoded, err := DecodeHookInput(encoded)
		if err != nil || !decoded.MatchesTarget(target) {
			t.Fatal("stdin failed to retain real target scope:", err)
		}
		targetBytes, err := codec.MarshalStable(target)
		if err != nil {
			t.Fatal(err)
		}
		restored, err := DecodeHookTarget(targetBytes)
		if err != nil || restored != target || restored.ValidFor(HookPostCompact) {
			t.Fatal("target failed to retain its sealed branch:", err)
		}
		if _, err = DecodeHookInput(append(encoded[:len(encoded)-1], []byte(`,"transcript_path":"private"}`)...)); err == nil {
			t.Fatal("accepted a transcript path")
		}
	}
	for _, invalid := range []string{
		`{"source":"startup","turn_id":"018f1d8a-7b5c-7def-8abc-456789abcdec","checkpoint_sequence":0}`,
		`{"source":"*","turn_id":"018f1d8a-7b5c-7def-8abc-456789abcdec"}`,
		`{"source":"compact","turn_id":"018f1d8a-7b5c-7def-8abc-456789abcdec"}`,
	} {
		var target SessionStartTarget
		if err := codec.UnmarshalRecord([]byte(invalid), &target); err == nil {
			t.Fatal("accepted mixed or unsupported target", invalid)
		}
	}
}

func TestSessionStartProofCountsCanonicalWrappedBytes(t *testing.T) {
	ref, err := NewHookReference(HookSourceProject, HookSessionStart, HookFeedbackModel, HookDigest([]byte("config")), "context", HookDigest([]byte("command")), 5)
	if err != nil {
		t.Fatal(err)
	}
	anchor := SessionStartTarget{Source: SessionStartResume, TurnID: TurnID("018f1d8a-7b5c-7def-8abc-456789abcdec")}
	body := "  保留换行\n</session_start_context>  "
	wrapped, err := WrapSessionStartContext(anchor.Source, body)
	if err != nil {
		t.Fatal(err)
	}
	value := SessionStartContext{Target: anchor, Items: []SessionStartContextItem{{Ordinal: 1, Reference: ref, Text: wrapped}}}
	proof := value.Items[0].Proof()
	if value.Validate() != nil || proof.Validate() != nil || proof.Bytes != len(wrapped) ||
		strings.Count(wrapped, "</session_start_context>") != 1 || !strings.Contains(wrapped, `  保留换行\n\u003c/session_start_context\u003e  `) {
		t.Fatal("wrapper failed to preserve context as escaped user data", wrapped)
	}
	mixed := value.Clone()
	mixed.Target.Source = SessionStartStartup
	if mixed.Validate() == nil {
		t.Fatal("accepted wrong-source context")
	}
	expanded, err := WrapSessionStartContext(anchor.Source, strings.Repeat("<", MaxHookFeedbackBytes))
	if err != nil {
		t.Fatal(err)
	}
	mixed.Target = anchor
	mixed.Items[0].Text = expanded
	if mixed.Items[0].Proof().Validate() != nil || mixed.Validate() == nil || value.Items[0].Text != wrapped {
		t.Fatal("candidate proof or final disclosure used the wrong size bound")
	}
}
