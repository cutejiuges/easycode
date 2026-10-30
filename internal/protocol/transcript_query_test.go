package protocol

import (
	"strings"
	"testing"

	"easycode/internal/codec"
	"easycode/internal/domain"
)

func TestTranscriptQueriesStrictCurrentShapes(t *testing.T) {
	thread := domain.ThreadID("018f1d8a-7b5c-7def-8123-456789abcdef")
	list := TranscriptListQuery{ThreadID: thread, Limit: 50, Through: 5}
	data, err := codec.MarshalStable(list)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = DecodeTranscriptListQuery(data); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []string{strings.Replace(string(data), `"limit":50`, `"limit":51`, 1), strings.Replace(string(data), `"through":5,`, "", 1), strings.TrimSuffix(string(data), "}") + `,"unknown":true}`} {
		if _, err := DecodeTranscriptListQuery([]byte(invalid)); err == nil {
			t.Fatalf("invalid query accepted: %s", invalid)
		}
	}
	body := TranscriptBodyQuery{ThreadID: thread, Cursor: TranscriptCursor{Sequence: 1}, Source: TranscriptText, Limit: 65536, Backward: true, Offset: 70000}
	encoded, _ := codec.MarshalStable(body)
	if got, err := DecodeTranscriptBodyQuery(encoded); err != nil || got != body {
		t.Fatal("body round trip")
	}
	for _, page := range []TranscriptBodyPage{{Offset: 1, NextOffset: 1, TotalBytes: 2}, {NextOffset: 3, TotalBytes: 3, Text: "ab"}, {NextOffset: 1, TotalBytes: 1, Text: string([]byte{0xff})}} {
		if page.Validate() == nil {
			t.Fatal("invalid body page accepted")
		}
	}
}

func TestSevenToolActivityVariantsAndTranscriptMetadata(t *testing.T) {
	invocation := "018f1d8a-7b5c-7def-8123-456789abcdef"
	turn := domain.TurnID("018f1d8a-7b5c-7def-8123-456789abcdee")
	for _, name := range []ToolName{ToolRead, ToolGlob, ToolGrep, ToolPatch, ToolWrite, ToolShell, ToolReadArtifact} {
		t.Run(string(name), func(t *testing.T) {
			p := ToolActivityPayload{Tool: name, InvocationID: invocation, Summary: "target", Status: "waiting"}
			ready, err := NewToolReady(p)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := DecodeToolActivity(ready); err != nil {
				t.Fatal(err)
			}
			p.Status = "running"
			started, err := NewToolStarted(p)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := DecodeToolActivity(started); err != nil {
				t.Fatal(err)
			}
			_, err = NewToolProgress(p)
			if (err == nil) != (name == ToolShell) {
				t.Fatal("progress capability boundary")
			}
			p.Status = "success"
			p.Code = "ok"
			p.ExitKnown = name == ToolShell
			terminal, err := NewToolTerminal(ToolTerminalPayload{ToolActivityPayload: p, ResultPreview: "result"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := DecodeToolTerminal(terminal); err != nil {
				t.Fatal(err)
			}
			item := TranscriptListItem{Kind: domain.TranscriptCall, Cursor: TranscriptCursor{Sequence: 1}, TurnID: turn, Activity: p, Preview: "result"}
			encoded, _ := codec.MarshalStable(item)
			if _, err := DecodeTranscriptListItem(encoded); err != nil {
				t.Fatal(err)
			}
		})
	}
	item := TranscriptListItem{Kind: domain.TranscriptAssistant, Cursor: TranscriptCursor{Sequence: 1}, TurnID: turn, Text: "text", SummaryOnly: true}
	if item.Validate() == nil {
		t.Fatal("text accepted tool metadata")
	}
}

func TestTranscriptCommitRequiresStrictSequenceAndIdentity(t *testing.T) {
	if _, err := NewTranscriptCommitted(0); err == nil {
		t.Fatal("zero sequence")
	}
	event, err := NewTranscriptCommitted(3)
	if err != nil {
		t.Fatal(err)
	}
	if event.Validate() == nil {
		t.Fatal("identity missing")
	}
	event.SessionID = domain.SessionID("018f1d8a-7b5c-7def-8123-456789abcdef")
	event.ThreadID = domain.ThreadID(event.SessionID)
	event.TurnID = domain.TurnID(event.SessionID)
	if event.Validate() != nil {
		t.Fatal("valid commit rejected")
	}
	event.Payload = []byte(`{"sequence":3,"unknown":true}`)
	if event.Validate() == nil {
		t.Fatal("unknown commit field accepted")
	}
}
