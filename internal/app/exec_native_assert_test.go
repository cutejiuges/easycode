//go:build darwin

package app

import (
	"encoding/json"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/session"
)

func assertExecLoopNativeOrder(t *testing.T, plan session.ReplayPlan, family domain.ProviderFamily) {
	t.Helper()
	ids := []string{"call-1", "call-patch", "call-exec"}
	if family == domain.ProviderAnthropic {
		ids = []string{"toolu-1", "toolu-edit", "call-exec"}
	}
	if plan.NativeCommits[0].Role != domain.NativeCommitUserInputs {
		t.Fatal("initial input commit missing")
	}
	for index, native := range plan.NativeCommits[1:] {
		var entry struct {
			Kind    string `json:"kind"`
			Outputs []struct {
				CallID string `json:"call_id"`
				Phase  string `json:"phase"`
			} `json:"outputs"`
			Assistant struct {
				Content []struct {
					ID string `json:"id"`
				} `json:"content"`
			} `json:"assistant"`
			ToolOutputs json.RawMessage `json:"tool_outputs"`
		}
		if err := json.Unmarshal(native.Commit.Payload, &entry); err != nil {
			t.Fatal(err)
		}
		if index%2 == 0 {
			if entry.Kind != "sample" {
				t.Fatal("native sample ordering changed")
			}
			if index == 6 {
				if family == domain.ProviderOpenAI && (len(entry.Outputs) != 1 || entry.Outputs[0].Phase != "final") {
					t.Fatal("OpenAI final phase lost")
				}
				continue
			}
			got := ""
			if family == domain.ProviderOpenAI && len(entry.Outputs) == 1 {
				got = entry.Outputs[0].CallID
			}
			if family == domain.ProviderAnthropic && len(entry.Assistant.Content) == 1 {
				got = entry.Assistant.Content[0].ID
			}
			if got != ids[index/2] {
				t.Fatal("native call ordering changed")
			}
		} else {
			if entry.Kind != "tool_outputs" {
				t.Fatal("native output ordering changed")
			}
			got := ""
			if family == domain.ProviderOpenAI {
				var outputs []struct {
					CallID string `json:"call_id"`
					Type   string `json:"type"`
				}
				if err := json.Unmarshal(entry.ToolOutputs, &outputs); err != nil {
					t.Fatal(err)
				}
				if len(outputs) == 1 {
					got = outputs[0].CallID
				}
				if index == 3 && outputs[0].Type != "custom_tool_call_output" {
					t.Fatal("Patch freeform output lost")
				}
			} else {
				var message struct {
					Content []struct {
						ID string `json:"tool_use_id"`
					} `json:"content"`
				}
				if err := json.Unmarshal(entry.ToolOutputs, &message); err != nil {
					t.Fatal(err)
				}
				if len(message.Content) == 1 {
					got = message.Content[0].ID
				}
			}
			if got != ids[index/2] {
				t.Fatal("native output pairing changed")
			}
		}
	}
}
