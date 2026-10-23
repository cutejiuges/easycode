package protocol

import (
	"strings"
	"testing"
)

func commandActivityFixture() CommandActivityPayload {
	return CommandActivityPayload{InvocationID: "00000000-0013-7000-8000-000000000013", Command: "printf 中文", Workdir: "src", Status: "running"}
}

func TestCommandActivityRoundTripAndIdentity(t *testing.T) {
	terminal := func(payload CommandActivityPayload) (Event, error) {
		return NewCommandTerminal(CommandTerminalPayload{CommandActivityPayload: payload, ResultPreview: "frozen result"})
	}
	for index, constructor := range []func(CommandActivityPayload) (Event, error){NewCommandStarted, NewCommandProgress, terminal} {
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
		decoded, err := DecodeCommandActivity(event)
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
	cases := map[string]func(*CommandActivityPayload){
		"identity":             func(p *CommandActivityPayload) { p.InvocationID = "pid-1" },
		"empty command":        func(p *CommandActivityPayload) { p.Command = "" },
		"long summary":         func(p *CommandActivityPayload) { p.Command = strings.Repeat("字", 161) },
		"three lines":          func(p *CommandActivityPayload) { p.Command = "a\nb\nc" },
		"invalid command utf8": func(p *CommandActivityPayload) { p.Command = string([]byte{255}) },
		"absolute workdir":     func(p *CommandActivityPayload) { p.Workdir = "/tmp" },
		"traversal":            func(p *CommandActivityPayload) { p.Workdir = "../secret" },
		"negative elapsed":     func(p *CommandActivityPayload) { p.ElapsedMillis = -1 },
		"negative stdout":      func(p *CommandActivityPayload) { p.StdoutBytes = -1 },
		"negative stderr":      func(p *CommandActivityPayload) { p.StderrBytes = -1 },
		"stdout lines":         func(p *CommandActivityPayload) { p.StdoutLines = 1 },
		"stderr lines":         func(p *CommandActivityPayload) { p.StderrLines = 1 },
		"large recent":         func(p *CommandActivityPayload) { p.Recent = strings.Repeat("x", 4097) },
		"invalid recent utf8":  func(p *CommandActivityPayload) { p.Recent = string([]byte{255}) },
		"running exit":         func(p *CommandActivityPayload) { p.ExitKnown = true },
		"running code":         func(p *CommandActivityPayload) { p.Code = "ok" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			payload := commandActivityFixture()
			mutate(&payload)
			if _, err := NewCommandProgress(payload); err == nil {
				t.Fatal("invalid progress accepted")
			}
		})
	}
	payload := commandActivityFixture()
	payload.Command, payload.Recent = strings.Repeat("字", 160), strings.Repeat("x", 4096)
	if _, err := NewCommandProgress(payload); err != nil {
		t.Fatal(err)
	}
}

func TestCommandTerminalStateConsistency(t *testing.T) {
	for _, status := range []string{"success", "denied", "error", "cancelled", "outcome_uncertain"} {
		payload := commandActivityFixture()
		payload.Status, payload.Code = status, "test_failure"
		if status == "success" {
			payload.Code, payload.ExitKnown, payload.ExitCode = "ok", true, 2
		}
		if _, err := NewCommandTerminal(CommandTerminalPayload{CommandActivityPayload: payload, ResultPreview: "frozen result"}); err != nil {
			t.Fatalf("%s: %v", status, err)
		}
	}
	for name, mutate := range map[string]func(*CommandActivityPayload){
		"unknown status":   func(p *CommandActivityPayload) { p.Status = "finished" },
		"empty code":       func(p *CommandActivityPayload) { p.Code = "" },
		"unsafe code":      func(p *CommandActivityPayload) { p.Code = "/secret/key" },
		"unknown exit":     func(p *CommandActivityPayload) { p.ExitKnown = false },
		"bad success code": func(p *CommandActivityPayload) { p.Code = "failed" },
		"exit range":       func(p *CommandActivityPayload) { p.ExitCode = 256 },
		"denied ran":       func(p *CommandActivityPayload) { p.Status = "denied" },
	} {
		t.Run(name, func(t *testing.T) {
			payload := commandActivityFixture()
			payload.Status, payload.Code, payload.ExitKnown = "success", "ok", true
			mutate(&payload)
			if _, err := NewCommandTerminal(CommandTerminalPayload{CommandActivityPayload: payload, ResultPreview: "frozen result"}); err == nil {
				t.Fatal("invalid terminal accepted")
			}
		})
	}
}

func TestCommandResultPreviewExistsOnlyAtTerminal(t *testing.T) {
	activity := commandActivityFixture()
	activity.Status, activity.Code, activity.ExitKnown = "success", "ok", true
	for _, preview := range []string{"", strings.Repeat("x", (256<<10)+1), string([]byte{255})} {
		if _, err := NewCommandTerminal(CommandTerminalPayload{CommandActivityPayload: activity, ResultPreview: preview}); err == nil {
			t.Fatal("invalid terminal preview accepted")
		}
	}
	want := strings.Repeat("字", (256<<10)/3) + "x"
	event, err := NewCommandTerminal(CommandTerminalPayload{CommandActivityPayload: activity, ResultPreview: want})
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeCommandTerminal(event)
	if err != nil || decoded.ResultPreview != want {
		t.Fatal("terminal preview bytes changed")
	}
	for _, constructor := range []func(CommandActivityPayload) (Event, error){NewCommandStarted, NewCommandProgress} {
		running, err := constructor(commandActivityFixture())
		if err != nil {
			t.Fatal(err)
		}
		for _, value := range []string{`"secret"`, `""`, `null`} {
			invalid := running
			invalid.Payload = []byte(strings.TrimSuffix(string(running.Payload), "}") + `,"result_preview":` + value + `}`)
			if _, err := DecodeCommandActivity(invalid); err == nil {
				t.Fatal("running event accepted terminal-only field")
			}
		}
	}
	legacy, err := newCommandActivity(activity, EventCommandTerminal)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeCommandTerminal(legacy); err == nil {
		t.Fatal("terminal without current preview accepted")
	}
}
