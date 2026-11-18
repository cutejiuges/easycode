package provider

import (
	"easycode/internal/domain"
	"testing"
)

func testInputPreview(t *testing.T, family domain.ProviderFamily) InputPreview {
	t.Helper()
	estimate, err := domain.NewUnknownTokenEstimate("byte_heuristic")
	if err != nil {
		t.Fatal(err)
	}
	footprint, err := domain.NewNativeHistoryFootprint(family, 0, estimate)
	if err != nil {
		t.Fatal(err)
	}
	view, err := domain.NewActiveContextView(domain.SemanticHistoryView{Provider: family, Turns: []domain.SemanticTurn{}}, domain.ContextSummary{}, footprint.Revision())
	if err != nil {
		t.Fatal(err)
	}
	preview, err := NewInputPreview(view, footprint, func(TurnInput) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	return preview
}
