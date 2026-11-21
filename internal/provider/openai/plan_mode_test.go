package openai

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"easycode/internal/codec"
	"easycode/internal/domain"
	"easycode/internal/provider"
	"easycode/internal/secret"
)

func testPlanModeInput(t *testing.T, input provider.TurnInput) provider.TurnInput {
	t.Helper()
	catalog, err := testOpenAIToolCatalog(t).ForPlanMode()
	if err != nil {
		t.Fatal(err)
	}
	planned, err := input.WithPlanMode(catalog)
	if err != nil {
		t.Fatal(err)
	}
	return planned
}

func assertPlanExecRestore(t *testing.T, live, restored *Conversation) {
	t.Helper()
	base, err := testSamplingInput(t, live.provider.config.Inference).WithToolCatalog(testOpenAIToolCatalog(t))
	if err != nil {
		t.Fatal(err)
	}
	plain, err := live.compileSamplingRequest(live.history.Entries(), base)
	if err != nil {
		t.Fatal(err)
	}
	var defaultWire responsesRequest
	if err := codec.Unmarshal(plain.Bytes(), &defaultWire); err != nil || defaultWire.Instructions != "" || len(defaultWire.Tools) != 10 {
		t.Fatal("default request changed planning instructions or ordinary tool catalog", err)
	}
	input := testPlanModeInput(t, base)
	before, err := live.compileSamplingRequest(live.history.Entries(), input)
	if err != nil {
		t.Fatal(err)
	}
	after, err := restored.compileSamplingRequest(restored.history.Entries(), input)
	if err != nil || !before.Equal(after) || before.SHA256() != after.SHA256() {
		t.Fatal("restored planning request bytes or fingerprint differ", err)
	}
	opened, err := live.requestEvidence(input)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := restored.requestEvidence(input)
	if err != nil || opened.NativeFingerprint() != replayed.NativeFingerprint() || !opened.Properties().Equal(replayed.Properties()) {
		t.Fatal("restored planning request evidence differs", err)
	}
	want, err := os.ReadFile("testdata/plan_mode_exec_continuation.golden.json")
	if err != nil || !bytes.Equal(before.Bytes(), bytes.TrimSpace(want)) {
		t.Fatalf("planning request golden mismatch: %v\n%s", err, before.Bytes())
	}
}

func TestPlanProposalSelectsOneCommittedResponsesMessage(t *testing.T) {
	const markdown = "# 方案\r\n\n步骤\r\n"
	const block = "<proposed_plan>\r\n" + markdown + "</proposed_plan>"
	message := func(id, phase, parts string) string {
		return fmt.Sprintf(`{"type":"response.output_item.done","item":{"type":"message","id":%q,"role":"assistant"%s,"content":[%s]}}`, id, phase, parts)
	}
	part := func(s string) string { return fmt.Sprintf(`{"type":"output_text","text":%q}`, s) }
	commentary := message("comment", `,"phase":"commentary"`, part("Interim finding.\n"))
	final := message("final", `,"phase":"final_answer"`, part("<proposed_")+","+part("plan>\r\n"+markdown+"</proposed_plan>"))
	for _, tc := range []struct {
		name, want, stopText, terminalFields string
		items                                []string
	}{
		{"commentary followed by final", markdown, "Interim finding.\n" + block, `,"status":"completed","incomplete_details":null`, []string{commentary, final}},
		{"single unphased message", markdown, block, "", []string{message("unphased", "", part(block))}},
		{"commentary with complete markers", "", block, "", []string{message("preliminary", `,"phase":"commentary"`, part(block))}},
		{"split across unphased messages", "", block, "", []string{message("first", "", part("<proposed_plan>\r\n"+markdown)), message("second", "", part("</proposed_plan>"))}},
		{"multiple final messages", "", block + "Another answer", "", []string{final, message("other", `,"phase":"final_answer"`, part("Another answer"))}},
		{"closed block with incomplete status", "", block, `,"status":"incomplete"`, []string{final}},
		{"closed block with incomplete details", "", block, `,"status":"completed","incomplete_details":{"reason":"max_output_tokens"}`, []string{final}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, "data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp-plan\"}}\n\n")
				for _, item := range tc.items {
					fmt.Fprintf(w, "data: %s\n\n", item)
				}
				fmt.Fprintf(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp-plan\"%s}}\n\n", tc.terminalFields)
			}))
			defer server.Close()
			instance, err := New(Config{BaseURL: server.URL, APIKey: secret.New("plan-fixture"), Model: "gpt-test"})
			if err != nil {
				t.Fatal(err)
			}
			defer closeProvider(t, instance)
			c := testConversation(t, instance)
			input := testPlanModeInput(t, testOpenAITurnInput(t, c, "Investigate"))
			stream, err := c.Stream(context.Background(), input)
			if err != nil {
				t.Fatal(err)
			}
			terminal := readOpenAITerminal(t, c, stream)
			if terminal.Kind() != provider.StreamEventCompleted {
				t.Fatal(terminal.Error())
			}
			ref := domain.AssistantSampleReference{TurnID: c.historySnapshot()[0].TurnID}
			if _, ok := c.ProjectPlanProposal(ref); ok {
				t.Fatal("uncommitted sample was reviewable")
			}
			if err := terminal.PreparedSample().Finalize(); err != nil {
				t.Fatal(err)
			}
			if got, ok := c.ProjectPlanProposal(ref); ok != (tc.want != "") || got != tc.want {
				t.Fatalf("unexpected proposal: %q, %v", got, ok)
			}
			stopText, err := c.ProjectAssistantSample(ref)
			if err != nil || stopText.Text() != tc.stopText {
				t.Fatalf("Stop lost its original text projection: %q, %v", stopText.Text(), err)
			}
		})
	}
}
