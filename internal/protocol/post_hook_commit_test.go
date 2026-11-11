package protocol

import (
	"bytes"
	"easycode/internal/domain"
	"strings"
	"testing"
)

func TestPostHookCommitScopeAndStrictPayload(t *testing.T) {
	const id = "01900000-0000-7000-8000-000000000001"
	payload := PostHookCommittedPayload{InvocationID: id, PlanDigest: strings.Repeat("a", 64), DetailSequence: 12, Phase: PostHookFinished}
	event, err := NewPostHookCommitted(payload)
	if err != nil {
		t.Fatal(err)
	}
	if event.Validate() == nil {
		t.Fatal("post commit accepted without scope")
	}
	event.SessionID = domain.SessionID(id)
	event.ThreadID = domain.ThreadID(id)
	event.TurnID = domain.TurnID(id)
	if event.Validate() != nil {
		t.Fatal("valid post commit rejected")
	}
	decoded, err := DecodePostHookCommitted(event)
	if err != nil || decoded != payload {
		t.Fatal(err)
	}
	for _, bad := range [][]byte{
		append(bytes.TrimSuffix(event.Payload, []byte("}")), []byte(`,"event":"PostToolUse"}`)...),
		bytes.Replace(event.Payload, []byte(`"detail_sequence":12`), []byte(`"detail_sequence":0`), 1),
		bytes.Replace(event.Payload, []byte(`"phase":"finished"`), []byte(`"phase":"future"`), 1),
		bytes.Replace(event.Payload, []byte(`"phase":"finished"`), []byte(`"phase":"finished","phase":"started"`), 1),
		bytes.Replace(event.Payload, []byte(`"invocation_id":`), []byte(`"unknown_id":`), 1),
		append(bytes.TrimSuffix(event.Payload, []byte("}")), []byte(`,"stdout":"secret-sentinel"}`)...),
	} {
		modified := event
		modified.Payload = bad
		if modified.Validate() == nil {
			t.Fatal("invalid or disclosing post event accepted")
		}
	}
}

func TestTranscriptPostSummaryRejectsCrossCallAndRawStatistics(t *testing.T) {
	const id = "01900000-0000-7000-8000-000000000001"
	d, _ := domain.NewHookDeclaration(domain.HookSourceProject, domain.HookPostToolUse, domain.HookFeedbackModel, strings.Repeat("a", 64), "check", 7, "true", 5)
	item := TranscriptListItem{Cursor: TranscriptCursor{Sequence: 1}, Kind: domain.TranscriptCall, TurnID: domain.TurnID(id), DetailSequence: 5, Activity: ToolActivityPayload{InvocationID: id, Tool: ToolWrite, Summary: "a", Status: "success", Code: "ok"}, PostHook: HookActivityPayload{InvocationID: id, Reference: d.Reference, State: HookActivityCompleted, Code: domain.HookOK}}
	if item.Validate() != nil {
		t.Fatal("valid post summary rejected")
	}
	for _, mode := range []string{"wrong-call", "pre-reference", "bytes", "no-sequence", "non-tool", "read-tool", "failed-tool", "per-item-outcome"} {
		bad := item
		switch mode {
		case "wrong-call":
			bad.PostHook.InvocationID = "01900000-0000-7000-8000-000000000002"
		case "pre-reference":
			bad.PostHook.Reference.Event = domain.HookPreToolUse
		case "bytes":
			bad.PostHook.StdoutBytes = 1
		case "no-sequence":
			bad.DetailSequence = 0
		case "non-tool":
			bad.Kind = domain.TranscriptAssistant
		case "read-tool":
			bad.Activity.Tool = ToolRead
		case "failed-tool":
			bad.Activity.Status, bad.Activity.Code = "error", "failed"
		case "per-item-outcome":
			bad.PostHook.State = HookActivityState(domain.HookContinue)
		}
		if bad.Validate() == nil {
			t.Fatal("invalid post summary accepted", mode)
		}
	}
}

func TestReasoningAndPostSummaryKeepSeparateTranscriptKinds(t *testing.T) {
	const id = "01900000-0000-7000-8000-000000000001"
	d, _ := domain.NewHookDeclaration(domain.HookSourceProject, domain.HookPostToolUse, domain.HookFeedbackModel, strings.Repeat("a", 64), "check", 7, "true", 5)
	post := HookActivityPayload{InvocationID: id, Reference: d.Reference, State: HookActivityCompleted, Code: domain.HookOK}
	reasoning := ReasoningSummaryPayload{Source: domain.ReasoningOpenAISummary, Availability: domain.ReasoningReadable, TotalBytes: 7}
	item := TranscriptListItem{Cursor: TranscriptCursor{Sequence: 1}, Kind: domain.TranscriptReasoning, TurnID: domain.TurnID(id), Text: "visible", Reasoning: &reasoning}
	if item.Validate() != nil {
		t.Fatal("valid reasoning list item rejected")
	}
	bad := item
	bad.PostHook, bad.DetailSequence = post, 2
	if bad.Validate() == nil {
		t.Fatal("reasoning item accepted post hook metadata")
	}
	bad = item
	bad.DetailSequence = 2
	if bad.Validate() == nil {
		t.Fatal("reasoning item accepted a post detail revision")
	}
	bad = TranscriptListItem{Cursor: item.Cursor, Kind: domain.TranscriptCall, TurnID: item.TurnID, DetailSequence: 2, PostHook: post, Activity: ToolActivityPayload{InvocationID: id, Tool: ToolWrite, Summary: "a", Status: "success", Code: "ok"}}
	if bad.Validate() != nil {
		t.Fatal("valid post tool item rejected")
	}
	bad.Reasoning = &reasoning
	if bad.Validate() == nil {
		t.Fatal("tool item accepted reasoning metadata")
	}
}
