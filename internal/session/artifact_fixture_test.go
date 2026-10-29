package session

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"easycode/internal/tool"
)

func TestArtifactCurrentFixturesReplayWithoutObjects(t *testing.T) {
	for _, state := range []string{"complete", "partial", "unavailable", "ready", "started"} {
		t.Run(state, func(t *testing.T) {
			fixture := readCurrentFixture(t, "artifact_"+state+"_tail.jsonl")
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
			for i, record := range loaded.Records {
				_, encoded, err := EncodeRecord(record)
				if err != nil || !bytes.Equal(encoded, bytes.Split(bytes.TrimSpace(fixture), []byte{'\n'})[i]) {
					t.Fatal("fixed fixture bytes changed")
				}
			}
			if state == "ready" || state == "started" {
				call := plan.ToolRecovery.Calls()[0]
				want := ReplayedToolCallReady
				if state == "started" {
					want = ReplayedToolCallStarted
				}
				if call.Invocation.Capability() != tool.CapabilityReadArtifact || call.State != want {
					t.Fatal("interrupted ReadArtifact facts lost")
				}
				return
			}
			if len(plan.Commands) != 1 || !plan.Commands[0].HasResult {
				t.Fatal("historical command missing")
			}
			result := plan.Commands[0].Result
			if result.Preview().Text() != "frozen Exec output" || string(result.ExecMetadata().StdoutSaving().Status()) != state {
				t.Fatal("storage state changed frozen preview")
			}
			refs := CommandArtifactReferences(plan.Commands)
			if state == "unavailable" {
				if len(refs) != 0 {
					t.Fatal("unavailable output granted reference")
				}
			} else if len(refs) != 1 || refs[0].ThreadID() != loaded.Identity.ThreadID {
				t.Fatal("artifact authorization projection lost")
			}
		})
	}
}

func TestReadArtifactCurrentNativeFixturesKeepCommittedPages(t *testing.T) {
	for _, family := range []string{"openai", "anthropic"} {
		fixture := readCurrentFixture(t, "artifact_result_"+family+".jsonl")
		repo, lease := installCurrentFixture(t, fixture)
		loaded, err := NewLoader().Load(context.Background(), lease)
		if err != nil {
			t.Fatal(err)
		}
		plan, err := NewReplayPlanner().Plan(loaded)
		if err != nil {
			t.Fatal(err)
		}
		if len(plan.Commands) != 2 || len(CommandArtifactReferences(plan.Commands)) != 1 {
			t.Fatal("artifact history or reference lost")
		}
		found := false
		for i, record := range loaded.Records {
			_, encoded, err := EncodeRecord(record)
			if err != nil || !bytes.Equal(encoded, bytes.Split(bytes.TrimSpace(fixture), []byte{'\n'})[i]) {
				t.Fatal("immutable native fixture changed")
			}
			if record.EventKind == EventToolCallResult {
				p, err := DecodeToolCallResultPayload(record)
				if err != nil {
					t.Fatal(err)
				}
				if p.Capability == tool.CapabilityReadArtifact {
					found = true
					if p.ReadArtifactMetadata.Page.Text() != "012" || p.ReadArtifactMetadata.Page.EOF() {
						t.Fatal("frozen artifact page changed")
					}
				}
			}
		}
		if !found {
			t.Fatal("ReadArtifact result missing")
		}
		_ = lease.Close()
		_ = repo.Close()
	}
}

func TestArtifactResultRejectsWrongIdentityAndOldShape(t *testing.T) {
	fixture := readCurrentFixture(t, "artifact_complete_tail.jsonl")
	lines := bytes.Split(bytes.TrimSpace(fixture), []byte{'\n'})
	record, err := DecodeRecord(lines[len(lines)-1])
	if err != nil {
		t.Fatal(err)
	}
	baseline := string(record.Payload)
	for _, pair := range [][2]string{
		{`"thread_id":"00000000-0001-7000-8000-000000000002"`, `"thread_id":"00000000-0001-7000-8000-000000000004"`},
		{`"stream":"stdout"`, `"stream":"stderr"`},
		{`"saved_bytes":10`, `"saved_bytes":11`},
		{`"observed_bytes":10`, `"observed_bytes":9`},
		{`"complete":true`, `"complete":false`},
		{`"status":"complete"`, `"status":"partial"`},
		{`"stdout_saving":`, `"legacy_stdout_saving":`},
	} {
		mutated := strings.Replace(baseline, pair[0], pair[1], 1)
		if mutated == baseline {
			t.Fatal("mutation did not target fixture")
		}
		record.Payload = []byte(mutated)
		if _, err := DecodeToolCallResultPayload(record); err == nil {
			t.Fatalf("invalid artifact record accepted: %s", pair[0])
		}
	}
	// 仅修改 descriptor 内的调用身份，外层账本身份保持原值。
	marker := `"invocation_id":"01890f3e-7bcd-7abc-8abc-0123456789ad"`
	last := strings.LastIndex(baseline, marker)
	record.Payload = []byte(baseline[:last] + strings.Replace(baseline[last:], marker, `"invocation_id":"01890f3e-7bcd-7abc-8abc-0123456789ae"`, 1))
	if _, err := DecodeToolCallResultPayload(record); err == nil {
		t.Fatal("descriptor crossed invocation boundary")
	}
}
