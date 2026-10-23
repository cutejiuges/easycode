package session

import (
	"strings"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/tool"
)

func TestExecDraftRoundTripAndStrictMetadata(t *testing.T) {
	turnID, _ := domain.ParseTurnID("018f1d8a-7b5c-7def-8123-456789abcdef")
	id, _ := tool.ParseInvocationID("018f1d8a-7b5c-7def-8abc-456789abcdef")
	input, err := tool.NewExecInput("printf '你好'", "src", 1000)
	if err != nil {
		t.Fatal(err)
	}
	call, err := tool.NewExecReadyCall("call_exec", input)
	if err != nil {
		t.Fatal(err)
	}
	invocation, err := tool.NewInvocation(id, call)
	if err != nil {
		t.Fatal(err)
	}
	draft, err := NewToolCallReadyDraft(turnID, invocation, 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	ready, err := DecodeToolCallReadyPayload(Record{PayloadVersion: EnvelopeVersion, EventKind: EventToolCallReady, Payload: draft.PayloadBytes()})
	if err != nil {
		t.Fatal(err)
	}
	restored, err := ready.Domain()
	if err != nil {
		t.Fatal(err)
	}
	exec, ok := restored.Exec()
	if !ok || exec.Input().Command() != input.Command() || exec.Input().Workdir() != "src" || exec.Input().TimeoutMillis() != 1000 || ready.SampleIndex != 1 || ready.CallIndex != 2 {
		t.Fatal("Exec ready facts changed after restore")
	}
	for _, test := range []struct {
		name    string
		payload string
		version int
	}{
		{"unknown", strings.Replace(string(draft.PayloadBytes()), `"exec_input":{`, `"exec_input":{"future":true,`, 1), EnvelopeVersion},
		{"trailing", string(draft.PayloadBytes()) + `{}`, EnvelopeVersion},
		{"canary", string(draft.PayloadBytes()), EnvelopeVersion + 1},
		{"capability", strings.Replace(string(draft.PayloadBytes()), `"process.exec"`, `"fs.read"`, 1), EnvelopeVersion},
		{"wire", strings.Replace(string(draft.PayloadBytes()), string(tool.WireStructured), string(tool.WireCustomFreeform), 1), EnvelopeVersion},
		{"sample", strings.Replace(string(draft.PayloadBytes()), `"sample_index":1`, `"sample_index":-1`, 1), EnvelopeVersion},
		{"call", strings.Replace(string(draft.PayloadBytes()), `"call_index":2`, `"call_index":-1`, 1), EnvelopeVersion},
	} {
		t.Run(test.name, func(t *testing.T) {
			if test.version == EnvelopeVersion && test.payload == string(draft.PayloadBytes()) {
				t.Fatal("test did not mutate ready payload")
			}
			if _, err := DecodeToolCallReadyPayload(Record{PayloadVersion: test.version, EventKind: EventToolCallReady, Payload: []byte(test.payload)}); err == nil {
				t.Fatal("invalid Exec ready payload accepted")
			}
		})
	}
	for _, status := range []tool.ResultStatus{tool.ResultDenied, tool.ResultCancelled, tool.ResultError, tool.ResultOutcomeUncertain} {
		result := tool.NewExecErrorResult(exec, status, "execution_failed", "Exec failed")
		resultDraft, err := NewToolCallResultDraft(turnID, result)
		if err != nil {
			t.Fatal(err)
		}
		record := Record{PayloadVersion: EnvelopeVersion, EventKind: EventToolCallResult, Payload: resultDraft.PayloadBytes()}
		payload, err := DecodeToolCallResultPayload(record)
		if err != nil {
			t.Fatal(err)
		}
		value, err := payload.Domain(invocation)
		if err != nil || value.Status() != status || value.Preview().Text() != result.Preview().Text() || value.ExecMetadata().Workdir() != "src" {
			t.Fatalf("Exec result changed after restore: %v", err)
		}
		for _, field := range []string{`"pid":42,`, `"environment":{"API_KEY":"private-secret"},`, `"output_chunks":["private-secret"],`, `"cause":"private-secret",`, `"future":true,`} {
			mutated := strings.Replace(string(resultDraft.PayloadBytes()), `"exec_metadata":{`, `"exec_metadata":{`+field, 1)
			if mutated == string(resultDraft.PayloadBytes()) {
				t.Fatal("test did not mutate metadata")
			}
			record.Payload = []byte(mutated)
			if _, err := DecodeToolCallResultPayload(record); err == nil || strings.Contains(err.Error(), "private-secret") {
				t.Fatal("unknown or sensitive metadata accepted or leaked")
			}
		}
		record.Payload = []byte(strings.Replace(string(resultDraft.PayloadBytes()), `"workdir":"src"`, `"workdir":"/private/outside"`, 1))
		if _, err := DecodeToolCallResultPayload(record); err == nil {
			t.Fatal("absolute Exec workdir accepted")
		}
	}
}
