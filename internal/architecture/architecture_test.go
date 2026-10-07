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

const sonicImportPath = "github.com/bytedance/sonic"

var placeholderFiles = map[string]string{
	"internal/protocol/event.go":          "P3",
	"internal/domain/types.go":            "P3",
	"internal/extension/extension.go":     "P6",
	"internal/extension/hook/hook.go":     "P6",
	"internal/extension/mcp/mcp.go":       "P6",
	"internal/extension/plugin/plugin.go": "P6",
	"internal/extension/skill/skill.go":   "P6",
	"internal/subagent/control.go":        "P7",
	"internal/telemetry/logger.go":        "P8",
}

var placeholderOnlyPackages = map[string]struct{}{
	"easycode/internal/extension":        {},
	"easycode/internal/extension/hook":   {},
	"easycode/internal/extension/mcp":    {},
	"easycode/internal/extension/plugin": {},
	"easycode/internal/extension/skill":  {},
	"easycode/internal/subagent":         {},
	"easycode/internal/telemetry":        {},
}

// context 是已有 OpenSpec 覆盖且具备完整行为测试的独立契约根，不属于占位能力。
var standaloneContractRoots = map[string]struct{}{
	"easycode/internal/context": {},
}

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

func TestJSONCodecOwnership(t *testing.T) {
	repository := loadRepositorySource(t)
	var violations []string
	for _, packagePath := range sortedPackagePaths(repository.packages) {
		current := repository.packages[packagePath]
		if packagePath == "easycode/internal/codec" {
			for imported := range current.imports {
				violations = append(violations, packagePath+" -> "+imported+": codec 不得依赖其他内部包")
			}
		}
		for relative, file := range current.files {
			for _, imported := range file.Imports {
				value, err := strconv.Unquote(imported.Path.Value)
				if err != nil {
					violations = append(violations, relative+": import 路径无效")
					continue
				}
				if value == sonicImportPath && packagePath != "easycode/internal/codec" {
					violations = append(violations, relative+": 只有 internal/codec 可以直接导入 Sonic")
				}
			}
		}
	}
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

func TestCoreExportedAPIsRejectDynamicTypes(t *testing.T) {
	repository := loadRepositorySource(t)
	var violations []string
	for _, packagePath := range sortedPackagePaths(repository.packages) {
		if !isCoreContractPackage(packagePath) {
			continue
		}
		violations = append(violations, exportedDynamicTypeViolations(repository.packages[packagePath])...)
	}
	assertNoViolations(t, violations)
}

func TestProtocolConstructorsDoNotStartGoroutinesOrAccessIO(t *testing.T) {
	repository := loadRepositorySource(t)
	current := repository.packages["easycode/internal/protocol"]
	if current == nil {
		t.Fatal("protocol 包不存在")
	}
	var violations []string
	for relative, file := range current.files {
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || !strings.HasPrefix(function.Name.Name, "New") || function.Body == nil {
				continue
			}
			ast.Inspect(function.Body, func(node ast.Node) bool {
				switch typed := node.(type) {
				case *ast.GoStmt:
					violations = append(violations, relative+":"+function.Name.Name+" 启动 goroutine")
				case *ast.SelectorExpr:
					identifier, ok := typed.X.(*ast.Ident)
					if ok && (identifier.Name == "os" || identifier.Name == "net" || identifier.Name == "http") {
						violations = append(violations, relative+":"+function.Name.Name+" 访问 I/O 包 "+identifier.Name)
					}
				}
				return true
			})
		}
	}
	assertNoViolations(t, violations)
}

func TestPlaceholderAllowlist(t *testing.T) {
	repository := loadRepositorySource(t)
	exitCondition := "由后续 OpenSpec 同时提供真实消费者、验证与测试时启用，否则删除"
	seen := make(map[string]struct{}, len(placeholderFiles))
	var violations []string
	err := filepath.WalkDir(repository.root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if entry.Name() == ".git" || entry.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		content, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		relative, relErr := filepath.Rel(repository.root, path)
		if relErr != nil {
			return relErr
		}
		relative = filepath.ToSlash(relative)
		text := string(content)
		hasStructuredTODO := strings.Contains(text, "TODO(P")
		stage, allowed := placeholderFiles[relative]
		if hasStructuredTODO && !allowed {
			violations = append(violations, relative+": 包含未登记的阶段占位 TODO")
		}
		if !allowed {
			return nil
		}
		seen[relative] = struct{}{}
		if !strings.Contains(text, "TODO("+stage+"):") {
			violations = append(violations, relative+": 缺少约定 Roadmap 阶段 "+stage)
		}
		if !strings.Contains(text, exitCondition) {
			violations = append(violations, relative+": 缺少统一启用或删除条件")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("扫描占位文件: %v", err)
	}
	for relative := range placeholderFiles {
		if _, ok := seen[relative]; !ok {
			violations = append(violations, relative+": 登记的占位文件不存在")
		}
	}
	assertNoViolations(t, violations)
}

func TestProductionReachability(t *testing.T) {
	repository := loadRepositorySource(t)
	reachable := reachablePackages(repository.packages, "easycode/cmd/easycode")
	var violations []string
	for _, packagePath := range sortedPackagePaths(repository.packages) {
		if _, ok := reachable[packagePath]; ok {
			continue
		}
		if _, ok := placeholderOnlyPackages[packagePath]; ok {
			continue
		}
		if _, ok := standaloneContractRoots[packagePath]; ok {
			continue
		}
		violations = append(violations, packagePath+": 生产包从 cmd/easycode 不可达且未登记")
	}
	assertNoViolations(t, violations)
}

func TestRuntimeAndAppDoNotImportPlaceholderCapabilities(t *testing.T) {
	repository := loadRepositorySource(t)
	var violations []string
	for _, packagePath := range []string{"easycode/internal/runtime", "easycode/internal/app"} {
		current := repository.packages[packagePath]
		if current == nil {
			violations = append(violations, packagePath+": 包不存在")
			continue
		}
		for imported := range current.imports {
			if _, forbidden := placeholderOnlyPackages[imported]; forbidden {
				violations = append(violations, packagePath+" -> "+imported+": Runtime/app 不得导入占位能力")
			}
		}
	}
	assertNoViolations(t, violations)
}

func TestArchitectureGuardRejectsViolationFixtures(t *testing.T) {
	tests := []struct {
		name  string
		file  string
		check func(*sourcePackage) []string
	}{
		{
			name:  "package variable",
			file:  "testdata/violations/package_variable.go.txt",
			check: packageVariableViolations,
		},
		{
			name:  "exported any",
			file:  "testdata/violations/exported_any.go.txt",
			check: exportedDynamicTypeViolations,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := parseFixturePackage(t, test.file)
			if violations := test.check(fixture); len(violations) == 0 {
				t.Fatalf("违规 fixture %s 未被架构守卫拒绝", test.file)
			}
		})
	}

	graph := map[string]*sourcePackage{
		"easycode/internal/domain": {
			path:    "easycode/internal/domain",
			imports: map[string]struct{}{"easycode/internal/app": {}},
		},
		"easycode/internal/app": {path: "easycode/internal/app", imports: map[string]struct{}{}},
	}
	if violations := dependencyViolations(graph); len(violations) == 0 {
		t.Fatal("反向依赖 fixture 未被架构守卫拒绝")
	}
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
				if entry.Name() == "testdata" {
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
	if packagePath == "easycode/internal/session/catalog" && strings.HasPrefix(imported, "easycode/internal/") &&
		imported != "easycode/internal/session" && imported != "easycode/internal/domain" {
		return "session/catalog 只能依赖 session/domain"
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

func exportedDynamicTypeViolations(current *sourcePackage) []string {
	var violations []string
	for relative, file := range current.files {
		for _, declaration := range file.Decls {
			switch typed := declaration.(type) {
			case *ast.FuncDecl:
				if ast.IsExported(typed.Name.Name) && containsUnboundedDynamicType(typed.Type) {
					violations = append(violations, relative+": 导出函数或方法 "+typed.Name.Name+" 暴露 any/interface{}")
				}
			case *ast.GenDecl:
				if typed.Tok != token.TYPE {
					continue
				}
				for _, spec := range typed.Specs {
					typeSpec := spec.(*ast.TypeSpec)
					if ast.IsExported(typeSpec.Name.Name) && containsUnboundedDynamicType(typeSpec.Type) {
						violations = append(violations, relative+": 导出类型 "+typeSpec.Name.Name+" 暴露 any/interface{}")
					}
				}
			}
		}
	}
	return violations
}

func containsUnboundedDynamicType(node ast.Node) bool {
	found := false
	ast.Inspect(node, func(current ast.Node) bool {
		if found || current == nil {
			return !found
		}
		switch typed := current.(type) {
		case *ast.Ident:
			if typed.Name == "any" {
				found = true
			}
		case *ast.InterfaceType:
			if typed.Methods == nil || len(typed.Methods.List) == 0 {
				found = true
			}
		}
		return !found
	})
	return found
}

func isCoreContractPackage(packagePath string) bool {
	for _, prefix := range []string{
		"easycode/internal/domain",
		"easycode/internal/protocol",
		"easycode/internal/provider",
		"easycode/internal/context",
		"easycode/internal/session",
	} {
		if packagePath == prefix || strings.HasPrefix(packagePath, prefix+"/") {
			return true
		}
	}
	return false
}

func reachablePackages(packages map[string]*sourcePackage, root string) map[string]struct{} {
	reachable := make(map[string]struct{}, len(packages))
	var visit func(string)
	visit = func(packagePath string) {
		if _, seen := reachable[packagePath]; seen {
			return
		}
		current := packages[packagePath]
		if current == nil {
			return
		}
		reachable[packagePath] = struct{}{}
		for imported := range current.imports {
			visit(imported)
		}
	}
	visit(root)
	return reachable
}

func parseFixturePackage(t *testing.T, relative string) *sourcePackage {
	t.Helper()
	parsed, err := parser.ParseFile(token.NewFileSet(), relative, nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("解析 fixture %s: %v", relative, err)
	}
	return &sourcePackage{
		path:    "easycode/internal/domain",
		files:   map[string]*ast.File{relative: parsed},
		imports: make(map[string]struct{}),
	}
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
