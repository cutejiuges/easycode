package domain

import (
	"strings"
	"testing"
)

func TestParsePlanProposalPreservesOnlyCompleteBody(t *testing.T) {
	const markdown = "# 方案\n\n```go\n\tfmt.Println(\"hello\")\n```\n"
	wrap := func(body string) string { return "<proposed_plan>\n" + body + "</proposed_plan>" }
	atLimit := strings.Repeat("好", (MaxPlanProposalBytes-1)/3) + "x\n"
	if len(atLimit) != MaxPlanProposalBytes {
		t.Fatal("fixture must meet the byte limit")
	}
	for _, tc := range []struct{ name, text, want string }{
		{"LF closing at EOF", wrap(markdown), markdown},
		{"CRLF and surrounding blank lines", "\r\n" + strings.ReplaceAll(wrap(markdown), "\n", "\r\n") + "\r\n \t\r\n", strings.ReplaceAll(markdown, "\n", "\r\n")},
		{"at byte limit", wrap(atLimit), atLimit},
		{"over byte limit", wrap("x" + atLimit), ""},
		{"no closing tag", "<proposed_plan>\n" + markdown, ""},
		{"bare carriage return after EOF tag", wrap(markdown) + "\r", ""},
		{"indented code block", "    <proposed_plan>\n    Plan\n    </proposed_plan>", ""},
		{"tag with trailing space", "<proposed_plan> \n" + markdown + "</proposed_plan>", ""},
		{"outside prose", "Introduction\n" + wrap(markdown), ""},
		{"following prose", wrap(markdown) + "\nMore text", ""},
		{"repeated block", wrap(markdown) + "\n" + wrap(markdown), ""},
		{"nested delimiter in example", wrap("```\n<proposed_plan>\n```\n"), ""},
		{"blank body", wrap(" \t\r\n"), ""},
		{"invalid UTF-8 body", wrap("\xff\n"), ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := ParsePlanProposal(tc.text)
			if ok != (tc.want != "") || got != tc.want {
				t.Fatalf("unexpected proposal validity or body: %v, got %d bytes, want %d", ok, len(got), len(tc.want))
			}
		})
	}
}
