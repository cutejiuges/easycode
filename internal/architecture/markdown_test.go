package architecture_test

import (
	"go/ast"
	"strconv"
	"strings"
	"testing"
)

func TestMarkdownScannerHasNoExternalLifecycle(t *testing.T) {
	repository := loadRepositorySource(t)
	found := 0
	for _, pkg := range repository.packages {
		for path, file := range pkg.files {
			if !strings.Contains(path, "internal/tui/markdown_highlight") || strings.HasSuffix(path, "_test.go") {
				continue
			}
			found++
			for _, entry := range file.Imports {
				value, err := strconv.Unquote(entry.Path.Value)
				if err != nil {
					t.Fatal(err)
				}
				switch value {
				case "strings", "unicode", "unicode/utf8":
				default:
					t.Errorf("scanner imports external lifecycle dependency: %s", value)
				}
			}
			ast.Inspect(file, func(node ast.Node) bool {
				if _, ok := node.(*ast.GoStmt); ok {
					t.Errorf("scanner starts goroutine: %s", path)
				}
				return true
			})
		}
	}
	if found == 0 {
		t.Fatal("scanner sources were not checked")
	}
}
