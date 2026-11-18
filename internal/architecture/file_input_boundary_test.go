package architecture_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path"
	"strings"
	"testing"
)

// fileInputBoundaryViolations 保证文件语法/交互只交换值，来源只有显式 Start 能启动工作。
func fileInputBoundaryViolations(pkg *sourcePackage) []string {
	var violations []string
	for relative, file := range pkg.files {
		if !strings.HasPrefix(path.Base(relative), "file_") {
			continue
		}
		imports := importsByName(file)
		if pkg.path == "easycode/internal/domain" || pkg.path == "easycode/internal/tui" {
			for _, dependency := range imports {
				switch dependency {
				case "os", "io/fs", "net", "net/http", "os/exec", "syscall", "database/sql", "easycode/internal/provider", "easycode/internal/runtime", "easycode/internal/tool/builtin":
					violations = append(violations, relative+": file values or UI own external effects")
				}
			}
		}
		if pkg.path != "easycode/internal/app" {
			continue
		}
		for _, decl := range file.Decls {
			function, ok := decl.(*ast.FuncDecl)
			if !ok || function.Body == nil {
				continue
			}
			ast.Inspect(function.Body, func(node ast.Node) bool {
				if _, ok := node.(*ast.GoStmt); ok && function.Name.Name != "Start" {
					violations = append(violations, relative+": file worker starts outside Start")
				}
				return true
			})
		}
		ast.Inspect(file, func(node ast.Node) bool {
			structure, ok := node.(*ast.StructType)
			if !ok {
				return true
			}
			for _, field := range structure.Fields.List {
				if selector, ok := field.Type.(*ast.SelectorExpr); ok {
					if name, ok := selector.X.(*ast.Ident); ok && imports[name.Name] == "context" && selector.Sel.Name == "Context" {
						violations = append(violations, relative+": file owner stores operation context")
					}
				}
			}
			return true
		})
	}
	return violations
}

func TestFileInputValuesUILifecycleAndDependencies(t *testing.T) {
	repository := loadRepositorySource(t)
	var violations []string
	for _, pkg := range repository.packages {
		violations = append(violations, fileInputBoundaryViolations(pkg)...)
	}
	assertNoViolations(t, violations)
}
func TestFileInputBoundaryGuardRejectsSourceReadsAndHiddenWorkers(t *testing.T) {
	for _, test := range []struct{ pkg, source string }{{"domain", `package domain; import "os";func Parse(){os.ReadFile("a")}`}, {"tui", `package tui;import "easycode/internal/tool/builtin";func Render(w *builtin.Workspace){w.DiscoverFileNames(nil,nil)}`}, {"app", `package app;func newSource(){go func(){}()}`}, {"app", `package app;import "context";type owner struct{ctx context.Context}`}} {
		file, err := parser.ParseFile(token.NewFileSet(), "file_boundary.go", test.source, 0)
		if err != nil {
			t.Fatal(err)
		}
		pkg := &sourcePackage{path: "easycode/internal/" + test.pkg, files: map[string]*ast.File{"file_boundary.go": file}}
		if len(fileInputBoundaryViolations(pkg)) == 0 {
			t.Fatal("file boundary guard missed regression", test.pkg)
		}
	}
}
