package architecture_test

import (
	"go/ast"
	"strings"
	"testing"
)

func TestPluginContributionAndLifecycleBoundaries(t *testing.T) {
	repository := loadRepositorySource(t)
	var violations []string
	for name, pkg := range repository.packages {
		for imported := range pkg.imports {
			if imported == "easycode/internal/extension/plugin" && name != "easycode/internal/app" && name != "easycode/cmd/easycode" {
				violations = append(violations, name+": 插件 loader 只能由应用与启动参数边界消费")
			}
			if (name == "easycode/internal/extension/plugin" || name == "easycode/internal/extension/skill") && strings.HasPrefix(imported, "easycode/internal/session") {
				violations = append(violations, name+": 来源不得反向依赖 Session 私有存储")
			}
		}
		if name != "easycode/internal/extension/plugin" && name != "easycode/internal/extension/skill" {
			continue
		}
		for relative, file := range pkg.files {
			for _, decl := range file.Decls {
				function, ok := decl.(*ast.FuncDecl)
				if !ok || function.Body == nil || !strings.HasPrefix(function.Name.Name, "New") {
					continue
				}
				ast.Inspect(function.Body, func(node ast.Node) bool {
					switch v := node.(type) {
					case *ast.GoStmt:
						violations = append(violations, relative+": 来源构造不得启动 goroutine")
					case *ast.SelectorExpr:
						if id, ok := v.X.(*ast.Ident); ok && (id.Name == "os" || id.Name == "localfile" || id.Name == "unix") {
							violations = append(violations, relative+": 来源构造不得访问 I/O")
						}
					}
					return true
				})
			}
		}
	}
	for _, name := range []string{"easycode/internal/extension", "easycode/internal/extension/plugin"} {
		if _, old := placeholderOnlyPackages[name]; old {
			violations = append(violations, name+": 真实贡献必须退出占位 allowlist")
		}
	}
	assertNoViolations(t, violations)
}
