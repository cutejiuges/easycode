package architecture_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path"
	"strconv"
	"strings"
	"testing"
)

func TestProviderFlowDependsOnBoundStrategies(t *testing.T) {
	repository := loadRepositorySource(t)
	var violations []string
	for _, name := range []string{"app", "runtime", "context", "provider"} {
		pkg := repository.packages[modulePath+"/internal/"+name]
		for fileName, file := range pkg.files {
			for _, violation := range providerFlowViolations(name, file) {
				violations = append(violations, fileName+": "+violation)
			}
		}
	}
	assertNoViolations(t, violations)
}

func providerFlowViolations(pkg string, file *ast.File) []string {
	domain, provider := "", ""
	var violations []string
	for _, entry := range file.Imports {
		imported, _ := strconv.Unquote(entry.Path.Value)
		name := path.Base(imported)
		if entry.Name != nil {
			name = entry.Name.Name
		}
		switch imported {
		case modulePath + "/internal/domain":
			domain = name
		case modulePath + "/internal/provider":
			provider = name
		default:
			if pkg != "app" && strings.HasPrefix(imported, modulePath+"/internal/provider/") && imported != modulePath+"/internal/provider/transport" {
				violations = append(violations, "共享流程导入具体 Provider 适配器 "+imported)
			}
		}
	}
	isFamily := func(node ast.Node) bool {
		expr, ok := node.(*ast.SelectorExpr)
		return ok && namedSelector(expr, domain) && (expr.Sel.Name == "ProviderAnthropic" || expr.Sel.Name == "ProviderOpenAI")
	}
	for _, decl := range file.Decls {
		fn, _ := decl.(*ast.FuncDecl)
		binding := fn != nil && isProviderBinding(pkg, fn.Name.Name)
		ast.Inspect(decl, func(node ast.Node) bool {
			if pkg != "app" && isFamily(node) {
				violations = append(violations, "共享流程直接引用具体 Provider 家族")
			}
			if expr, ok := node.(*ast.TypeAssertExpr); ok && !binding && (pkg == "app" || pkg == "runtime") {
				if selector, ok := expr.Type.(*ast.SelectorExpr); ok && namedSelector(selector, provider) {
					violations = append(violations, "装配下游再次探测 Provider 操作 "+selector.Sel.Name)
				}
			}
			return true
		})
	}
	return violations
}

func namedSelector(selector *ast.SelectorExpr, name string) bool {
	qualifier, ok := selector.X.(*ast.Ident)
	return ok && qualifier.Name == name
}

func isProviderBinding(pkg, name string) bool {
	switch pkg {
	case "app":
		return name == "bindSession" || name == "bindTranscriptProjector"
	case "runtime":
		return name == "bindProviderOperations"
	default:
		return false
	}
}

func TestProviderFlowCheckDistinguishesIdentityAndDispatch(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "", `package runtime
import (
	d "easycode/internal/domain"
	p "easycode/internal/provider"
)
func same(left, right d.ProviderFamily) bool { return left == right }
func bindProviderOperations(c p.Conversation) { _, _ = c.(p.ToolResultPreparer) }
func choose(c p.Conversation) bool { return c.Family() == d.ProviderOpenAI }
func run(c p.Conversation) { _, _ = c.(p.ToolResultPreparer) }
`, 0)
	if err != nil {
		t.Fatal(err)
	}
	violations := providerFlowViolations("runtime", file)
	if len(violations) != 2 {
		t.Fatalf("预期只报告具体家族分派和装配下游探测，得到 %v", violations)
	}
}
