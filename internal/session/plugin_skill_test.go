package session

import (
	"bytes"
	"context"
	"easycode/internal/tool"
	"os"
	"testing"
)

func TestPluginCurrentFrozenResultAndPolicyResourceFixtures(t *testing.T) {
	for _, resource := range []bool{false, true} {
		file := "plugin_skill_result.json"
		if resource {
			file = "plugin_skill_resource_result.json"
		}
		data := bytes.TrimSpace(readCurrentFixture(t, file))
		payload, err := DecodeToolCallResultPayload(Record{EventKind: EventToolCallResult, Payload: data})
		if err != nil {
			t.Fatal(err)
		}
		input, _ := tool.NewSkillLoadInput("qa:demo", "args")
		if resource {
			input, _ = tool.NewSkillResourceInput("qa:demo", "agents/openai.yaml")
		}
		ready, _ := tool.NewSkillReadyCall("call-skill", input)
		invocation, _ := tool.NewInvocation("01900000-0000-7000-8000-000000000001", ready)
		result, err := payload.Domain(invocation)
		if err != nil {
			t.Fatal(err)
		}
		a, ok := result.SkillAttachment()
		if !ok || a.Source.Scope != "plugin" || a.Name != "qa:demo" {
			t.Fatal("lost plugin result")
		}
		if resource && !bytes.Contains([]byte(a.Content), []byte("allow_implicit_invocation: false")) {
			t.Fatal("policy resource not frozen")
		}
		draft, err := NewToolCallResultDraft(testTurnID, result)
		if err != nil || !bytes.Equal(data, draft.PayloadBytes()) {
			t.Fatalf("fixed plugin result drifted: %v\n%s", err, draft.PayloadBytes())
		}
		forged := bytes.Replace(data, []byte(`"directory":"qa"`), []byte(`"directory":"docs"`), 1)
		if _, err := DecodeToolCallResultPayload(Record{EventKind: EventToolCallResult, Payload: forged}); err == nil {
			t.Fatal("forged namespace restored")
		}
	}
}

func TestPluginCurrentInputJournalsReplayWithoutRewriting(t *testing.T) {
	for _, family := range []string{"openai", "anthropic"} {
		data := readCurrentFixture(t, "plugin_input_"+family+".jsonl")
		repository, lease := installCurrentFixture(t, data)
		loaded, err := NewLoader().Load(context.Background(), lease)
		if err != nil {
			t.Fatal(err)
		}
		plan, err := NewReplayPlanner().Plan(loaded)
		if err != nil {
			t.Fatal(err)
		}
		if len(plan.NativeCommits) != 1 || !bytes.Contains(plan.NativeCommits[0].Commit.Payload, []byte(`"name":"qa:demo"`)) {
			t.Fatal("plugin native input missing")
		}
		for _, line := range bytes.Split(bytes.TrimSpace(data), []byte("\n")) {
			record, err := DecodeRecord(line)
			if err != nil {
				t.Fatal(err)
			}
			_, encoded, err := EncodeRecord(record)
			if err != nil || !bytes.Equal(encoded, line) {
				t.Fatal("plugin fixed record or checksum drifted", err)
			}
		}
		path, _ := repository.JournalPath(testThreadID)
		after, _ := os.ReadFile(path)
		if !bytes.Equal(after, data) {
			t.Fatal("replay rewrote fixture")
		}
		_ = lease.Close()
		_ = repository.Close()
	}
}
