package architecture_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

const modulePath = "easycode"

type repositorySource struct {
	root     string
	packages map[string]*sourcePackage
}

type sourcePackage struct {
	path    string
	files   map[string]*ast.File
	imports map[string]struct{}
}

func TestDependencyDirections(t *testing.T) {
	repository := loadRepositorySource(t)
	violations := dependencyViolations(repository.packages)
	violations = append(violations, importCycleViolations(repository.packages)...)
	assertNoViolations(t, violations)
}

func TestProductionPackageVariables(t *testing.T) {
	repository := loadRepositorySource(t)
	var violations []string
	for _, packagePath := range sortedPackagePaths(repository.packages) {
		violations = append(violations, packageVariableViolations(repository.packages[packagePath])...)
	}
	assertNoViolations(t, violations)
}

func loadRepositorySource(t *testing.T) repositorySource {
	t.Helper()
	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatalf("读取工作目录: %v", err)
	}
	root := filepath.Clean(filepath.Join(workingDirectory, "..", ".."))
	packages := make(map[string]*sourcePackage)
	for _, topLevel := range []string{"internal", "cmd"} {
		err = filepath.WalkDir(filepath.Join(root, topLevel), func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				if entry.Name() == "testdata" || entry.Name() == "testsupport" || entry.Name() == "testutil" {
					return filepath.SkipDir
				}
				return nil
			}
			if filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			relative, relErr := filepath.Rel(root, path)
			if relErr != nil {
				return relErr
			}
			directory := filepath.ToSlash(filepath.Dir(relative))
			packagePath := modulePath + "/" + directory
			parsed, parseErr := parser.ParseFile(token.NewFileSet(), path, nil, parser.ParseComments)
			if parseErr != nil {
				return parseErr
			}
			current := packages[packagePath]
			if current == nil {
				current = &sourcePackage{path: packagePath, files: make(map[string]*ast.File), imports: make(map[string]struct{})}
				packages[packagePath] = current
			}
			current.files[filepath.ToSlash(relative)] = parsed
			for _, imported := range parsed.Imports {
				value, unquoteErr := strconv.Unquote(imported.Path.Value)
				if unquoteErr != nil {
					return unquoteErr
				}
				if strings.HasPrefix(value, modulePath+"/") {
					current.imports[value] = struct{}{}
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("扫描 %s 源码: %v", topLevel, err)
		}
	}
	return repositorySource{root: root, packages: packages}
}

func dependencyViolations(packages map[string]*sourcePackage) []string {
	var violations []string
	for _, packagePath := range sortedPackagePaths(packages) {
		current := packages[packagePath]
		for imported := range current.imports {
			if reason := forbiddenDependencyReason(packagePath, imported); reason != "" {
				violations = append(violations, packagePath+" -> "+imported+": "+reason)
			}
		}
	}
	return violations
}

func forbiddenDependencyReason(packagePath string, imported string) string {
	if strings.Contains(imported+"/", "/testsupport/") || strings.Contains(imported+"/", "/testutil/") {
		return "生产代码不得依赖测试专属支持包"
	}
	if (strings.HasPrefix(packagePath, "easycode/internal/executor/process") || strings.HasPrefix(packagePath, "easycode/internal/extension/localfile")) && strings.HasPrefix(imported, "easycode/internal/") {
		return "共享进程与文件边界不得依赖业务层"
	}

	if packagePath == "easycode/internal/session/catalog" && strings.HasPrefix(imported, "easycode/internal/") &&
		imported != "easycode/internal/session" && imported != "easycode/internal/domain" && imported != "easycode/internal/codec" {
		return "session/catalog 只能依赖 session/domain/codec"
	}
	if imported == "easycode/internal/app" || strings.HasPrefix(imported, "easycode/cmd/") {
		if packagePath != "easycode/cmd/easycode" {
			return "下层包不得依赖 app/cmd"
		}
	}
	if packagePath == "easycode/internal/domain" && strings.HasPrefix(imported, "easycode/internal/") &&
		imported != "easycode/internal/codec" {
		return "domain 只能依赖 codec"
	}
	if packagePath == "easycode/internal/protocol" && imported != "easycode/internal/domain" && imported != "easycode/internal/codec" {
		if strings.HasPrefix(imported, "easycode/internal/") {
			return "protocol 只能依赖 domain/codec"
		}
	}
	if packagePath == "easycode/internal/runtime" {
		if imported == "easycode/internal/provider/openai" || imported == "easycode/internal/provider/anthropic" ||
			imported == "easycode/internal/app" || imported == "easycode/internal/tui" || imported == "easycode/internal/headless" {
			return "runtime 不得依赖具体 Provider wire 或宿主"
		}
	}
	if packagePath == "easycode/internal/headless" {
		if strings.HasPrefix(imported, "easycode/internal/provider") || imported == "easycode/internal/session" ||
			imported == "easycode/internal/tui" || imported == "easycode/internal/runtime" {
			return "headless 不得依赖具体业务实现或 TUI"
		}
	}
	if packagePath == "easycode/internal/tui" {
		if strings.HasPrefix(imported, "easycode/internal/provider") || imported == "easycode/internal/session" ||
			imported == "easycode/internal/headless" || imported == "easycode/internal/runtime" {
			return "TUI 不得依赖具体业务实现或 headless"
		}
	}
	if isMiddleLayerPackage(packagePath) {
		if imported == "easycode/internal/runtime" || imported == "easycode/internal/app" ||
			imported == "easycode/internal/tui" || imported == "easycode/internal/headless" {
			return "中间层不得反向依赖 runtime 或宿主"
		}
	}
	return ""
}

func isMiddleLayerPackage(packagePath string) bool {
	for _, prefix := range []string{
		"easycode/internal/provider",
		"easycode/internal/session",
		"easycode/internal/context",
		"easycode/internal/tool",
		"easycode/internal/extension",
		"easycode/internal/subagent",
	} {
		if packagePath == prefix || strings.HasPrefix(packagePath, prefix+"/") {
			return true
		}
	}
	return false
}

func importCycleViolations(packages map[string]*sourcePackage) []string {
	const (
		unvisited = iota
		visiting
		visited
	)
	state := make(map[string]int, len(packages))
	stack := make([]string, 0, len(packages))
	var violations []string
	var visit func(string)
	visit = func(packagePath string) {
		state[packagePath] = visiting
		stack = append(stack, packagePath)
		current := packages[packagePath]
		for imported := range current.imports {
			if packages[imported] == nil {
				continue
			}
			switch state[imported] {
			case unvisited:
				visit(imported)
			case visiting:
				violations = append(violations, "检测到内部包循环依赖: "+strings.Join(append(stack, imported), " -> "))
			}
		}
		stack = stack[:len(stack)-1]
		state[packagePath] = visited
	}
	for _, packagePath := range sortedPackagePaths(packages) {
		if state[packagePath] == unvisited {
			visit(packagePath)
		}
	}
	return violations
}

func packageVariableViolations(current *sourcePackage) []string {
	var violations []string
	for relative, file := range current.files {
		for _, declaration := range file.Decls {
			general, ok := declaration.(*ast.GenDecl)
			if !ok || general.Tok != token.VAR {
				continue
			}
			for _, spec := range general.Specs {
				valueSpec := spec.(*ast.ValueSpec)
				if isInterfaceAssertion(valueSpec) || isPrivateSentinel(valueSpec) {
					continue
				}
				names := make([]string, 0, len(valueSpec.Names))
				for _, name := range valueSpec.Names {
					names = append(names, name.Name)
				}
				violations = append(violations, relative+": 非法生产包级 var "+strings.Join(names, ","))
			}
		}
	}
	return violations
}

func isInterfaceAssertion(spec *ast.ValueSpec) bool {
	if spec.Type == nil || len(spec.Names) == 0 || len(spec.Names) != len(spec.Values) {
		return false
	}
	for index, name := range spec.Names {
		if name.Name != "_" || !isNilPointerConversion(spec.Values[index]) {
			return false
		}
	}
	return true
}

func isNilPointerConversion(expression ast.Expr) bool {
	call, ok := expression.(*ast.CallExpr)
	if !ok || len(call.Args) != 1 {
		return false
	}
	nilValue, ok := call.Args[0].(*ast.Ident)
	if !ok || nilValue.Name != "nil" {
		return false
	}
	parenthesized, ok := call.Fun.(*ast.ParenExpr)
	if !ok {
		return false
	}
	_, ok = parenthesized.X.(*ast.StarExpr)
	return ok
}

func isPrivateSentinel(spec *ast.ValueSpec) bool {
	if len(spec.Names) == 0 || len(spec.Names) != len(spec.Values) {
		return false
	}
	for index, name := range spec.Names {
		if name.Name == "_" || ast.IsExported(name.Name) || !isErrorsNew(spec.Values[index]) {
			return false
		}
	}
	return true
}

func isErrorsNew(expression ast.Expr) bool {
	call, ok := expression.(*ast.CallExpr)
	if !ok || len(call.Args) != 1 {
		return false
	}
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != "New" {
		return false
	}
	packageName, ok := selector.X.(*ast.Ident)
	return ok && packageName.Name == "errors"
}

func sortedPackagePaths(packages map[string]*sourcePackage) []string {
	paths := make([]string, 0, len(packages))
	for packagePath := range packages {
		paths = append(paths, packagePath)
	}
	sort.Strings(paths)
	return paths
}

func assertNoViolations(t *testing.T, violations []string) {
	t.Helper()
	if len(violations) == 0 {
		return
	}
	sort.Strings(violations)
	t.Fatalf("架构守卫发现 %d 个违规:\n%s", len(violations), strings.Join(violations, "\n"))
}
