package architecture_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

func TestHookSharedBoundariesRejectBusinessDependencies(t *testing.T) {
	for _, pkg := range []string{"easycode/internal/executor/process", "easycode/internal/extension/localfile"} {
		for _, dep := range []string{"easycode/internal/tool", "easycode/internal/domain", "easycode/internal/session", "easycode/internal/runtime", "easycode/internal/provider"} {
			if forbiddenDependencyReason(pkg, dep) == "" {
				t.Fatal("shared boundary accepted business dependency", pkg, dep)
			}
		}
		if forbiddenDependencyReason(pkg, "io") != "" {
			t.Fatal("shared boundary rejected standard library")
		}
	}
}
func TestHookSharedConstructorsRejectHiddenLifecycle(t *testing.T) {
	for _, pkg := range []string{"easycode/internal/executor/process", "easycode/internal/extension/localfile", "easycode/internal/extension/hook"} {
		file, err := parser.ParseFile(token.NewFileSet(), "hidden.go", `package boundary; import "os";func NewOwner(){os.Open("hidden")}`, 0)
		if err != nil {
			t.Fatal(err)
		}
		packages := map[string]*sourcePackage{pkg: {path: pkg, files: map[string]*ast.File{"hidden.go": file}}}
		if len(constructorViolations(packages)) == 0 {
			t.Fatal("hidden I/O constructor accepted", pkg)
		}
	}
}
