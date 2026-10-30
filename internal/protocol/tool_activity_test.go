package protocol

import (
	"strings"
	"testing"
)

func commandActivityFixture() ToolActivityPayload {
	return ToolActivityPayload{Tool: ToolShell, InvocationID: "00000000-0013-7000-8000-000000000013", Summary: "printf 中文", Workdir: "src", Status: "running"}
}

func TestCommandActivityRoundTripAndIdentity(t *testing.T) {
	terminal := func(payload ToolActivityPayload) (Event, error) {
		return NewToolTerminal(ToolTerminalPayload{ToolActivityPayload: payload, ResultPreview: "frozen result"})
	}
	for index, constructor := range []func(ToolActivityPayload) (Event, error){NewToolStarted, NewToolProgress, terminal} {
		payload := commandActivityFixture()
		if index == 2 {
			payload.Status, payload.Code, payload.ExitKnown = "success", "ok", true
		}
		event, err := constructor(payload)
		if err != nil {
			t.Fatal(err)
		}
		if event.Validate() == nil {
			t.Fatal("unbound command event accepted")
		}
		event.SessionID, event.ThreadID, event.TurnID = testSessionID, testThreadID, testTurnID
		if err := event.Validate(); err != nil {
			t.Fatal(err)
		}
		decoded, err := DecodeToolActivity(event)
		if err != nil || decoded != payload {
			t.Fatalf("round trip = %#v, %v", decoded, err)
		}
		for _, suffix := range []string{`,"pid":1}`, `,"environment":{"API_KEY":"secret"}}`, `} {}`} {
			invalid := event
			invalid.Payload = []byte(strings.TrimSuffix(string(event.Payload), "}") + suffix)
			if invalid.Validate() == nil {
				t.Fatalf("accepted malformed payload: %s", suffix)
			}
		}
		event.ThreadID = "invalid"
		if event.Validate() == nil {
			t.Fatal("invalid identity accepted")
		}
	}
}

func TestCommandActivityRejectsInvalidBounds(t *testing.T) {
	cases := map[string]func(*ToolActivityPayload){
		"identity":             func(p *ToolActivityPayload) { p.InvocationID = "pid-1" },
		"empty command":        func(p *ToolActivityPayload) { p.Summary = "" },
		"long summary":         func(p *ToolActivityPayload) { p.Summary = strings.Repeat("字", 161) },
		"three lines":          func(p *ToolActivityPayload) { p.Summary = "a\nb\nc" },
		"invalid command utf8": func(p *ToolActivityPayload) { p.Summary = string([]byte{255}) },
		"absolute workdir":     func(p *ToolActivityPayload) { p.Workdir = "/tmp" },
		"traversal":            func(p *ToolActivityPayload) { p.Workdir = "../secret" },
		"negative elapsed":     func(p *ToolActivityPayload) { p.ElapsedMillis = -1 },
		"negative stdout":      func(p *ToolActivityPayload) { p.StdoutBytes = -1 },
		"negative stderr":      func(p *ToolActivityPayload) { p.StderrBytes = -1 },
		"stdout lines":         func(p *ToolActivityPayload) { p.StdoutLines = 1 },
		"stderr lines":         func(p *ToolActivityPayload) { p.StderrLines = 1 },
		"large recent":         func(p *ToolActivityPayload) { p.Recent = strings.Repeat("x", 4097) },
		"invalid recent utf8":  func(p *ToolActivityPayload) { p.Recent = string([]byte{255}) },
		"running exit":         func(p *ToolActivityPayload) { p.ExitKnown = true },
		"running code":         func(p *ToolActivityPayload) { p.Code = "ok" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			payload := commandActivityFixture()
			mutate(&payload)
			if _, err := NewToolProgress(payload); err == nil {
				t.Fatal("invalid progress accepted")
			}
		})
	}
	payload := commandActivityFixture()
	payload.Summary, payload.Recent = strings.Repeat("字", 160), strings.Repeat("x", 4096)
	if _, err := NewToolProgress(payload); err != nil {
		t.Fatal(err)
	}
}

func TestToolTerminalStateConsistency(t *testing.T) {
	for _, status := range []string{"success", "denied", "error", "cancelled", "outcome_uncertain"} {
		payload := commandActivityFixture()
		payload.Status, payload.Code = status, "test_failure"
		if status == "success" {
			payload.Code, payload.ExitKnown, payload.ExitCode = "ok", true, 2
		}
		if _, err := NewToolTerminal(ToolTerminalPayload{ToolActivityPayload: payload, ResultPreview: "frozen result"}); err != nil {
			t.Fatalf("%s: %v", status, err)
		}
	}
	for name, mutate := range map[string]func(*ToolActivityPayload){
		"unknown status":   func(p *ToolActivityPayload) { p.Status = "finished" },
		"empty code":       func(p *ToolActivityPayload) { p.Code = "" },
		"unsafe code":      func(p *ToolActivityPayload) { p.Code = "/secret/key" },
		"unknown exit":     func(p *ToolActivityPayload) { p.ExitKnown = false },
		"bad success code": func(p *ToolActivityPayload) { p.Code = "failed" },
		"exit range":       func(p *ToolActivityPayload) { p.ExitCode = 256 },
		"denied ran":       func(p *ToolActivityPayload) { p.Status = "denied" },
	} {
		t.Run(name, func(t *testing.T) {
			payload := commandActivityFixture()
			payload.Status, payload.Code, payload.ExitKnown = "success", "ok", true
			mutate(&payload)
			if _, err := NewToolTerminal(ToolTerminalPayload{ToolActivityPayload: payload, ResultPreview: "frozen result"}); err == nil {
				t.Fatal("invalid terminal accepted")
			}
		})
	}
}

func TestCommandResultPreviewExistsOnlyAtTerminal(t *testing.T) {
	activity := commandActivityFixture()
	activity.Status, activity.Code, activity.ExitKnown = "success", "ok", true
	for _, preview := range []string{"", strings.Repeat("x", 4097), string([]byte{255})} {
		if _, err := NewToolTerminal(ToolTerminalPayload{ToolActivityPayload: activity, ResultPreview: preview}); err == nil {
			t.Fatal("invalid terminal preview accepted")
		}
	}
	want := strings.Repeat("字", 4096/3) + "x"
	event, err := NewToolTerminal(ToolTerminalPayload{ToolActivityPayload: activity, ResultPreview: want})
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeToolTerminal(event)
	if err != nil || decoded.ResultPreview != want {
		t.Fatal("terminal preview bytes changed")
	}
	for _, constructor := range []func(ToolActivityPayload) (Event, error){NewToolStarted, NewToolProgress} {
		running, err := constructor(commandActivityFixture())
		if err != nil {
			t.Fatal(err)
		}
		for _, value := range []string{`"secret"`, `""`, `null`} {
			invalid := running
			invalid.Payload = []byte(strings.TrimSuffix(string(running.Payload), "}") + `,"result_preview":` + value + `}`)
			if _, err := DecodeToolActivity(invalid); err == nil {
				t.Fatal("running event accepted terminal-only field")
			}
		}
	}
	legacy, err := newToolActivity(activity, EventToolTerminal)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeToolTerminal(legacy); err == nil {
		t.Fatal("terminal without current preview accepted")
	}
}

func TestTerminalSavedReferenceStrictIdentityAndIsolation(t *testing.T) {
	ref := ToolOutputReference{ID: strings.Repeat("a", 32), ThreadID: testThreadID, InvocationID: commandActivityFixture().InvocationID, Source: ToolStdout, SavedBytes: 3, ObservedBytes: 3, SHA256: strings.Repeat("b", 64), Complete: true}
	info, err := NewToolOutputInfo(ToolOutputInfo{Summary: ToolOutputSummary{Status: "complete", Readable: true, SavedBytes: 3, ObservedBytes: 3, Complete: true}, Reference: &ref})
	if err != nil {
		t.Fatal(err)
	}
	ref.SHA256 = "invalid"
	if info.Reference.SHA256 == ref.SHA256 {
		t.Fatal("constructor retained mutable reference")
	}
	activity := commandActivityFixture()
	activity.Status, activity.Code, activity.ExitKnown = "success", "ok", true
	event, err := NewToolTerminal(ToolTerminalPayload{ToolActivityPayload: activity, ResultPreview: "frozen", Stdout: info})
	if err != nil {
		t.Fatal(err)
	}
	event.SessionID, event.ThreadID, event.TurnID = testSessionID, testThreadID, testTurnID
	if err := event.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, pair := range [][2]string{{strings.Repeat("b", 64), "invalid"}, {`"source":"stdout"`, `"source":"search_result"`}, {`"saved_bytes":3`, `"saved_bytes":4`}} {
		invalid := event
		invalid.Payload = []byte(strings.Replace(string(event.Payload), pair[0], pair[1], 1))
		if invalid.Validate() == nil {
			t.Fatal("invalid saved reference accepted")
		}
	}
	event.ThreadID = "018f1d8a-7b5c-7def-8123-456789abcdee"
	if event.Validate() == nil {
		t.Fatal("cross-thread reference accepted")
	}
}
