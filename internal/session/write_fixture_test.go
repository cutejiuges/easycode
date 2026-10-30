package session

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"testing"

	"easycode/internal/codec"
	"easycode/internal/tool"
)

func TestCurrentWriteFixturesUseFrozenBytesAndTypedRecovery(t *testing.T) {
	for _, test := range []struct {
		name      string
		state     ReplayedToolCallState
		approval  tool.ApprovalOutcome
		status    tool.ResultStatus
		operation tool.WriteOperation
	}{
		{"write_before_approval.jsonl", ReplayedToolCallReady, "", "", ""},
		{"write_allowed_before_start.jsonl", ReplayedToolCallReady, tool.ApprovalOutcomeAllowOnce, "", ""},
		{"write_started_tail.jsonl", ReplayedToolCallStarted, tool.ApprovalOutcomeAllowOnce, "", ""},
		{"write_create_result_tail.jsonl", ReplayedToolCallResult, tool.ApprovalOutcomeAllowOnce, tool.ResultSuccess, tool.WriteCreate},
		{"write_overwrite_result_tail.jsonl", ReplayedToolCallResult, tool.ApprovalOutcomeAllowOnce, tool.ResultSuccess, tool.WriteOverwrite},
		{"write_denied_tail.jsonl", ReplayedToolCallResult, tool.ApprovalOutcomeDeny, tool.ResultDenied, tool.WriteOperationUnavailable},
		{"write_conflict_tail.jsonl", ReplayedToolCallResult, "", tool.ResultConflict, tool.WriteOperationUnavailable},
		{"write_no_changes_tail.jsonl", ReplayedToolCallResult, "", tool.ResultError, tool.WriteOperationUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := readCurrentFixture(t, test.name)
			for _, line := range bytes.Split(bytes.TrimSpace(fixture), []byte{'\n'}) {
				r, err := DecodeRecord(line)
				if err != nil {
					t.Fatal(err)
				}
				_, encoded, err := EncodeRecord(r)
				if err != nil || !bytes.Equal(encoded, line) {
					t.Fatal("固定 fixture bytes 非 canonical", err)
				}
			}
			repo, lease := installCurrentFixture(t, fixture)
			defer repo.Close()
			defer lease.Close()
			loaded, err := NewLoader().Load(context.Background(), lease)
			if err != nil {
				t.Fatal(err)
			}
			plan, err := NewReplayPlanner().Plan(loaded)
			if err != nil {
				t.Fatal(err)
			}
			calls := plan.ToolRecovery.Calls()
			if len(calls) != 1 || calls[0].Invocation.Capability() != tool.CapabilityWrite || calls[0].State != test.state || calls[0].ApprovalOutcome != test.approval {
				t.Fatal("Write recovery state 错误", calls)
			}
			v, _ := calls[0].Invocation.Write()
			if v.Input().Content() != "new" || v.Input().FilePath() != "main.go" {
				t.Fatal("必要模型参数丢失")
			}
			if test.state == ReplayedToolCallResult {
				r := calls[0].Result
				if r.Status() != test.status || r.WriteMetadata().Operation() != test.operation {
					t.Fatal("结果 metadata 错误")
				}
				if bytes.Contains([]byte(r.Preview().Text()), []byte("\"content\"")) {
					t.Fatal("结果泄漏正文")
				}
			}
		})
	}
}

func mutateWriteFixture(t *testing.T, name string, seq int, mutate func(*Record)) []byte {
	t.Helper()
	lines := bytes.Split(bytes.TrimSpace(readCurrentFixture(t, name)), []byte{'\n'})
	record, err := DecodeRecord(lines[seq-1])
	if err != nil {
		t.Fatal(err)
	}
	mutate(&record)
	record.Checksum, err = recordChecksum(record)
	if err != nil {
		t.Fatal(err)
	}
	lines[seq-1], err = codec.MarshalStable(record)
	if err != nil {
		t.Fatal(err)
	}
	return append(bytes.Join(lines, []byte{'\n'}), '\n')
}

func TestWriteInvalidCurrentRecordsFailBeforeRepairOrAppend(t *testing.T) {
	for _, test := range []struct {
		name, fixture string
		seq           int
		mutate        func(*Record)
	}{
		{"missing-content", "write_before_approval.jsonl", 8, func(r *Record) { r.Payload = bytes.Replace(r.Payload, []byte(`,"content":"new"`), nil, 1) }},
		{"null-content", "write_before_approval.jsonl", 8, func(r *Record) {
			r.Payload = bytes.Replace(r.Payload, []byte(`"content":"new"`), []byte(`"content":null`), 1)
		}},
		{"wrong-wire", "write_before_approval.jsonl", 8, func(r *Record) {
			r.Payload = bytes.Replace(r.Payload, []byte(`"wire_kind":"structured"`), []byte(`"wire_kind":"custom_freeform"`), 1)
		}},
		{"wrong-operation", "write_create_result_tail.jsonl", 11, func(r *Record) {
			r.Payload = bytes.Replace(r.Payload, []byte(`"operation":"create"`), []byte(`"operation":"unavailable"`), 1)
		}},
		{"wrong-count", "write_create_result_tail.jsonl", 11, func(r *Record) { r.Payload = bytes.Replace(r.Payload, []byte(`"bytes":3`), []byte(`"bytes":4`), 1) }},
		{"missing-count", "write_create_result_tail.jsonl", 11, func(r *Record) { r.Payload = bytes.Replace(r.Payload, []byte(`,"lines":1`), nil, 1) }},
		{"success-before-start", "write_no_changes_tail.jsonl", 9, func(r *Record) {
			r.Payload = bytes.Replace(r.Payload, []byte(`"status":"error"`), []byte(`"status":"success"`), 1)
		}},
		{"started-without-allow", "write_started_tail.jsonl", 9, func(r *Record) {
			r.Payload = bytes.Replace(r.Payload, []byte(`"outcome":"allow_once"`), []byte(`"outcome":"deny"`), 1)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := mutateWriteFixture(t, test.fixture, test.seq, test.mutate)
			repo, lease := installCurrentFixture(t, fixture)
			defer repo.Close()
			defer lease.Close()
			loaded, err := NewLoader().Load(context.Background(), lease)
			if err == nil {
				_, err = NewReplayPlanner().Plan(loaded)
			}
			if err == nil {
				t.Fatal("接受非法 Write 状态")
			}
			path, err := repo.JournalPath(testThreadID)
			if err != nil {
				t.Fatal(err)
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(after, fixture) {
				t.Fatal("非法 journal 被修复或改写", err)
			}
		})
	}
	old := readCurrentFixture(t, "superseded_write_baseline.jsonl")
	if _, err := DecodeRecord(bytes.TrimSpace(old)); err == nil {
		t.Fatal("接受旧开发期 canary")
	}
}

func TestWriteResultDecoderRejectsMixedMetadataAndUnknownFields(t *testing.T) {
	for _, raw := range []string{`{"relative_path":"a","operation":"create","bytes":0,"lines":0}`, `{"relative_path":"a","operation":"create","bytes":0,"lines":0,"created_directories":null}`, `{"relative_path":"a","operation":"create","bytes":0,"lines":0,"created_directories":0,"extra":1}`} {
		var m WriteResultMetadataPayload
		if err := json.Unmarshal([]byte(raw), &m); err == nil {
			t.Fatal("接受缺失或未知 metadata", raw)
		}
	}
}

func TestWriteMixedExclusiveFixturePreservesModelOrder(t *testing.T) {
	fixture := readCurrentFixture(t, "write_mixed_result_tail.jsonl")
	for _, line := range bytes.Split(bytes.TrimSpace(fixture), []byte{'\n'}) {
		r, err := DecodeRecord(line)
		if err != nil {
			t.Fatal(err)
		}
		_, encoded, err := EncodeRecord(r)
		if err != nil || !bytes.Equal(line, encoded) {
			t.Fatal("mixed fixture bytes changed", err)
		}
	}
	repo, lease := installCurrentFixture(t, fixture)
	defer repo.Close()
	defer lease.Close()
	loaded, err := NewLoader().Load(context.Background(), lease)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := NewReplayPlanner().Plan(loaded)
	if err != nil {
		t.Fatal(err)
	}
	calls := plan.ToolRecovery.Calls()
	if len(calls) != 4 {
		t.Fatal("mixed call count", len(calls))
	}
	for i, want := range []tool.CapabilityID{tool.CapabilityRead, tool.CapabilityWrite, tool.CapabilityPatch, tool.CapabilityGrep} {
		if calls[i].Invocation.Capability() != want || calls[i].State != ReplayedToolCallResult {
			t.Fatalf("call %d lost order/state", i)
		}
	}
}

func TestWriteMixedRecoveryRejectsCrossedExclusiveBarrier(t *testing.T) {
	repo, lease := installCurrentFixture(t, readCurrentFixture(t, "write_mixed_result_tail.jsonl"))
	defer repo.Close()
	defer lease.Close()
	loaded, err := NewLoader().Load(context.Background(), lease)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := NewReplayPlanner().Plan(loaded)
	if err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func([]ReplayedToolCall){
		func(c []ReplayedToolCall) { c[1].ApprovalSequence = c[0].ResultSequence },
		func(c []ReplayedToolCall) { c[2].StartedSequence = c[1].ResultSequence },
		func(c []ReplayedToolCall) {
			c[1].ApprovalSequence = 0
			c[1].ApprovalOutcome = ""
			c[1].CandidateFingerprint = ""
		},
		func(c []ReplayedToolCall) { c[3].StartedSequence = c[2].ResultSequence },
	} {
		calls := plan.ToolRecovery.Calls()
		mutate(calls)
		if _, err := NewToolRecoveryPlan(plan.ToolRecovery.TurnID(), 1, calls, false); err == nil {
			t.Fatal("恢复越过独占屏障")
		}
	}
}
