package provider_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSharedConsumersDoNotImportProviderWirePackages(t *testing.T) {
	t.Parallel()
	for _, packageDirectory := range []string{"../session", "../runtime", "../tui"} {
		entries, err := os.ReadDir(packageDirectory)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			if entry.IsDir() || filepath.Ext(entry.Name()) != ".go" || strings.HasSuffix(entry.Name(), "_test.go") {
				continue
			}
			path := filepath.Join(packageDirectory, entry.Name())
			content, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			for _, forbidden := range []string{
				`"easycode/internal/provider/openai"`,
				`"easycode/internal/provider/anthropic"`,
			} {
				if strings.Contains(string(content), forbidden) {
					t.Fatalf("shared consumer %s imports Provider wire package %s", path, forbidden)
				}
			}
		}
	}
}
