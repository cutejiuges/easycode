package tui

import (
	"testing"
)

func TestMarkdownViewDoesNotParse(t *testing.T) {
	model := snapshotCompletedModel(t)
	before := model.markdown.parser.parses
	for range 100 {
		model.View()
	}
	if model.markdown.parser.parses != before {
		t.Fatal("View parsed source")
	}
}
