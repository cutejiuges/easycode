package domain

import (
	"bytes"
	"testing"
)

func TestHookResponseStrictProjection(t *testing.T) {
	patch, err := NewHookPatchResponse(HookPatchMetadata{"a.go", 1, 2, 3})
	if err != nil {
		t.Fatal(err)
	}
	write, err := NewHookWriteResponse(HookWriteMetadata{"b.go", "create", 10, 2, 1})
	if err != nil {
		t.Fatal(err)
	}
	exec, err := NewHookExecResponse(HookExecMetadata{Workdir: ".", ExitKnown: true, ExitCode: 17, StdoutBytes: 45, StderrBytes: 9, OutputTruncated: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range []HookToolResponse{patch, write, exec} {
		b, err := v.MarshalJSON()
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := DecodeHookToolResponse(b)
		if err != nil || decoded != v {
			t.Fatal("projection roundtrip", err)
		}
		for _, bad := range [][]byte{
			bytes.Replace(b, []byte(`"code":"ok"`), []byte(`"code":"error"`), 1),
			bytes.Replace(b, []byte(`"status":"success"`), []byte(`"status":"error"`), 1),
			bytes.Replace(b, []byte(`"metadata":{`), []byte(`"metadata":{"preview":"secret",`), 1),
			bytes.Replace(b, []byte(`"metadata":{`), []byte(`"metadata":{"artifact_id":"secret",`), 1),
			bytes.Replace(b, []byte(`"code":"ok"`), []byte(`"code":null`), 1),
		} {
			if _, err := DecodeHookToolResponse(bad); err == nil {
				t.Fatalf("unsafe response accepted: %s", bad)
			}
		}
	}
	for _, bad := range []string{
		`{"capability":"process.exec","status":"success","code":"ok","metadata":{"workdir":".","exit_known":true,"exit_code":0,"stdout_bytes":0,"stderr_bytes":0,"output_truncated":false,"timed_out":false}}`,
		`{"capability":"fs.write","status":"success","code":"ok","metadata":{"file_path":"../private","operation":"create","bytes":0,"lines":0,"created_directories":0}}`,
		`{"capability":"fs.patch","status":"success","code":"ok","metadata":null}`,
	} {
		if _, err := DecodeHookToolResponse([]byte(bad)); err == nil {
			t.Fatal("invalid projection accepted", bad)
		}
	}
}

func TestFeedbackCandidateAuthorization(t *testing.T) {
	feedback, err := NewHookFeedbackCandidate("safe explicit feedback")
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range []HookEvent{HookPreToolUse, HookPostToolUse} {
		for _, policy := range []HookFeedbackPolicy{HookFeedbackNone, HookFeedbackModel} {
			_, err := NewHookParsedOutput(event, policy, HookContinue, HookOK, feedback)
			if (err == nil) != (event == HookPostToolUse && policy == HookFeedbackModel) {
				t.Fatal("wrong disclosure admission", event, policy)
			}
		}
	}
}

func TestPostResponseRequiresActualSuccessfulMetadata(t *testing.T) {
	for _, metadata := range []HookExecMetadata{
		{}, {ExitKnown: true, ExitCode: -1}, {ExitKnown: true, TimedOut: true}, {ExitKnown: true, Cancelled: true},
	} {
		if _, err := NewHookExecResponse(metadata); err == nil {
			t.Fatal("non-successful process projection accepted", metadata)
		}
	}
	for _, metadata := range []HookWriteMetadata{
		{"a", "create", 0, 1, 0}, {"a", "create", 1, 0, 0}, {"a", "create", 1, 2, 0}, {"a", "overwrite", 1, 1, 1},
	} {
		if _, err := NewHookWriteResponse(metadata); err == nil {
			t.Fatal("inconsistent write projection accepted", metadata)
		}
	}
}
