package headless

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHeadlessDoesNotImportConcreteRuntimeBoundaries(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".go" || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		content, err := os.ReadFile(entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		for _, forbidden := range []string{
			`"easycode/internal/provider`,
			`"easycode/internal/session`,
			`"easycode/internal/tui`,
		} {
			if strings.Contains(string(content), forbidden) {
				t.Fatalf("headless production file %s imports %s", entry.Name(), forbidden)
			}
		}
	}
}
