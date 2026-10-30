package session

import (
	"bytes"
	"strings"
	"testing"

	"easycode/internal/codec"
	"easycode/internal/tool"
)

func TestCurrentResultRequiresFrozenBudgetSavingAndVisibleMetadata(t *testing.T) {
	result := testGrepResult(t)
	draft, err := NewToolCallResultDraft(testTurnID, result)
	if err != nil {
		t.Fatal(err)
	}
	record := Record{PayloadVersion: EnvelopeVersion, EventKind: EventToolCallResult, Payload: draft.PayloadBytes()}
	payload, err := DecodeToolCallResultPayload(record)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		mutate func(*ToolCallResultPayload)
	}{
		{"wrong actual bytes", func(p *ToolCallResultPayload) {
			p.Budget, _ = tool.NewResultBudgetMetadata(65536, 0, tool.BudgetPreserved)
		}},
		{"invalid saving", func(p *ToolCallResultPayload) { p.GrepMetadata.Saving = tool.ArtifactSaving{} }},
		{"hidden metadata", func(p *ToolCallResultPayload) { p.GrepMetadata.Matches[0].Text = "hidden body" }},
		{"wrong model omission", func(p *ToolCallResultPayload) { p.GrepMetadata.ModelOmittedMatches = 1 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			fresh, err := DecodeToolCallResultPayload(record)
			if err != nil {
				t.Fatal(err)
			}
			test.mutate(&fresh)
			data, err := codec.MarshalStable(fresh)
			if err != nil {
				return
			}
			bad := record
			bad.Payload = data
			if _, err := DecodeToolCallResultPayload(bad); err == nil {
				t.Fatal("invalid current result accepted")
			}
		})
	}
	for _, field := range []string{"budget", "saving", "model_omitted_matches"} {
		data := bytes.Replace(record.Payload, []byte(`"`+field+`":`), []byte(`"unknown_`+field+`":`), 1)
		bad := record
		bad.Payload = data
		if _, err := DecodeToolCallResultPayload(bad); err == nil {
			t.Fatalf("missing %s accepted", field)
		}
	}
	if payload.Budget.ActualBytes() != len(result.Preview().Text()) {
		t.Fatal("budget lost bytes")
	}
	old := record
	old.PayloadVersion = 5
	if _, err := DecodeToolCallResultPayload(old); err == nil {
		t.Fatal("old canary accepted")
	}
}

func TestSearchResultJournalSizeExcludesHiddenCandidates(t *testing.T) {
	invocation := testGlobInvocation(t)
	glob, _ := invocation.Glob()
	paths := make([]string, 1000)
	for i := range paths {
		paths[i] = strings.Repeat("界", 400) + string(rune('a'+i/26)) + string(rune('a'+i%26))
	}
	metadata, err := tool.NewGlobResultMetadata(paths, false, 0, 1000, tool.SearchComplete, tool.SearchSkipCounts{})
	if err != nil {
		t.Fatal(err)
	}
	result, err := tool.NewGlobInvocationResult(glob, tool.ResultSuccess, "ok", tool.ModelPreview{}, metadata)
	if err != nil {
		t.Fatal(err)
	}
	execution, err := tool.NewSearchExecution(result, false)
	if err != nil {
		t.Fatal(err)
	}
	saving, _ := tool.NewArtifactSaving(tool.ArtifactUnavailable, "quota_exceeded", tool.ArtifactDescriptor{})
	frozen, err := execution.Finalize(1024, saving)
	if err != nil {
		t.Fatal(err)
	}
	draft, err := NewToolCallResultDraft(testTurnID, frozen)
	if err != nil {
		t.Fatal(err)
	}
	if len(draft.PayloadBytes()) > 4096 || frozen.GlobMetadata().ModelOmittedMatches() != 1000 {
		t.Fatal("journal retained hidden search bodies")
	}
}
