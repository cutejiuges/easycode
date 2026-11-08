package domain

import (
	"bytes"
	"easycode/internal/codec"
	"testing"
)

func TestPostHookInputExactMetadataOnlyBranches(t *testing.T) {
	id := "018f1d8a-7b5c-7def-8abc-456789abcdef"
	patch, _ := NewHookPatchInput("a.go", []HookReplacement{{OldText: "old", NewText: "new"}})
	patchResult, _ := NewHookPatchResponse(HookPatchMetadata{"a.go", 1, 2, 3})
	write, _ := NewHookWriteInput("b.go", "new\n")
	writeResult, _ := NewHookWriteResponse(HookWriteMetadata{"b.go", "create", 4, 1, 0})
	exec, _ := NewHookExecInput("printf check", ".", 1000)
	execResult, _ := NewHookExecResponse(HookExecMetadata{Workdir: ".", ExitKnown: true, ExitCode: 7, StdoutBytes: 123, StderrBytes: 45, OutputTruncated: true})
	for _, test := range []struct {
		name                    string
		input                   HookToolInput
		response                HookToolResponse
		inputJSON, responseJSON string
	}{
		{"Edit", patch, patchResult, `{"file_path":"a.go","replacements":[{"old_text":"old","new_text":"new","replace_all":false,"line_based":false}]}`, `{"capability":"fs.patch","status":"success","code":"ok","metadata":{"file_path":"a.go","replacement_count":1,"added_lines":2,"removed_lines":3}}`},
		{"Write", write, writeResult, `{"file_path":"b.go","content":"new\n"}`, `{"capability":"fs.write","status":"success","code":"ok","metadata":{"file_path":"b.go","operation":"create","bytes":4,"lines":1,"created_directories":0}}`},
		{"exec_command", exec, execResult, `{"command":"printf check","workdir":".","timeout_ms":1000}`, `{"capability":"process.exec","status":"success","code":"ok","metadata":{"workdir":".","exit_known":true,"exit_code":7,"stdout_bytes":123,"stderr_bytes":45,"output_truncated":true,"timed_out":false,"cancelled":false}}`},
	} {
		v, err := NewPostHookInput(SessionID(id), ThreadID(id), TurnID(id), "provider-call", test.name, "/workspace", test.input, test.response)
		if err != nil {
			t.Fatal(err)
		}
		actual, err := v.Bytes()
		if err != nil {
			t.Fatal(err)
		}
		want := `{"hook_event_name":"PostToolUse","session_id":"` + id + `","thread_id":"` + id + `","turn_id":"` + id + `","tool_use_id":"provider-call","tool_name":"` + test.name + `","cwd":"/workspace","tool_input":` + test.inputJSON + `,"tool_response":` + test.responseJSON + `}`
		if string(actual) != want {
			t.Fatalf("metadata fixture changed for %s: %s", test.name, actual)
		}
		for _, key := range []string{"artifact_id", "preview", "pid", "session_root", "stdout_content", "stderr_content", "Authorization"} {
			if bytes.Contains(actual, []byte(`"`+key+`"`)) {
				t.Fatal("private response projection", key)
			}
		}
		decoded, err := DecodeHookToolResponse([]byte(test.responseJSON))
		if err != nil || decoded != test.response {
			t.Fatal("response strict fixture", err)
		}
		if codec.Valid(actual) != true {
			t.Fatal("invalid stdin")
		}
	}
	if _, err := NewPostHookInput(SessionID(id), ThreadID(id), TurnID(id), "call", "Write", "/workspace", write, execResult); err == nil {
		t.Fatal("cross-capability response accepted")
	}
}
