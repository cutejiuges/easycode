package context

import (
	"bytes"
	"os"
	"reflect"
	"strings"
	"testing"

	"easycode/internal/codec"
	"easycode/internal/domain"
)

func TestFileAttachmentPlanningCountsOnceAndKeepsStablePrefix(t *testing.T) {
	data, err := os.ReadFile("../domain/testdata/file_attachment.json")
	if err != nil {
		t.Fatal(err)
	}
	a, err := domain.DecodeFileAttachment(data)
	if err != nil {
		t.Fatal(err)
	}
	files, err := domain.NewFileAttachments([]domain.FileAttachment{a})
	if err != nil {
		t.Fatal(err)
	}
	h := domain.SemanticHistoryView{Provider: domain.ProviderOpenAI, Turns: []domain.SemanticTurn{planningTurn("inspect @src/a.go#L2-8", "answer")}}
	h.Turns[0].Messages[0].Files = files
	makePlan := func(view domain.SemanticHistoryView, native uint64) (ContextPlan, CacheSources) {
		input := mustPlanningInput(t, view, estimatedFootprint(t, domain.ProviderOpenAI, 3, native), "next", DisabledBudget())
		p, err := NewPlanner().Plan(input)
		if err != nil {
			t.Fatal(err)
		}
		sources, err := NewCacheSources(p)
		if err != nil {
			t.Fatal(err)
		}
		return p, sources
	}
	p, before := makePlan(h, 10)
	encodedGolden, err := codec.MarshalStable(before)
	if err != nil {
		t.Fatal(err)
	}
	golden, err := os.ReadFile("testdata/file_cache_sources.golden.json")
	if err != nil || !bytes.Equal(bytes.TrimSpace(golden), encodedGolden) {
		t.Fatal("file cache source golden differs", err)
	}

	messages, err := semanticContent(h)
	if err != nil {
		t.Fatal(err)
	}
	content, err := codec.MarshalStable(messages)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Count(content, []byte("file_attachment")) != 1 || bytes.Contains(content, []byte(a.Digest())) || bytes.Contains(content, []byte("input_id")) {
		t.Fatalf("invalid canonical history: %s", content)
	}
	rendered, _ := a.RenderedText()
	if !bytes.Contains(content, []byte("second")) || strings.Count(rendered, "# Attached workspace file") != 1 {
		t.Fatal("file body missing")
	}
	h.Turns[0].Messages[0].InputID = "01900000-0000-7000-8000-000000000099"
	_, after := makePlan(h, 10)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("input identity polluted cache sources")
	}
	covered, _ := makePlan(h, 100000)
	for _, source := range covered.Sources() {
		if source.Kind() == SourceCommittedHistory {
			if count, ok := source.Estimate().Tokens(); !ok || count != 100000 {
				t.Fatal("native and visible history were summed", source)
			}
		}
	}
	h.Turns[0].Messages[0].Files = domain.FileAttachments{}
	plain, without := makePlan(h, 10)
	if before.StableFingerprint != without.StableFingerprint {
		t.Fatal("file body entered stable prefix")
	}
	var first, second uint64
	for _, s := range p.Sources() {
		if s.Kind() == SourceCommittedHistory {
			first, _ = s.Estimate().Tokens()
		}
	}
	for _, s := range plain.Sources() {
		if s.Kind() == SourceCommittedHistory {
			second, _ = s.Estimate().Tokens()
		}
	}
	if first <= second {
		t.Fatal("attachment was not included in effective estimate")
	}
}
