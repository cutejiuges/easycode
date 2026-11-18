package architecture_test

import (
	"go/ast"
	"path"
	"strconv"
	"strings"
	"testing"
)

func importsByName(file *ast.File) map[string]string {
	result := make(map[string]string)
	for _, item := range file.Imports {
		value, _ := strconv.Unquote(item.Path.Value)
		name := path.Base(value)
		if item.Name != nil {
			name = item.Name.Name
		}
		result[name] = value
	}
	return result
}

type constructorFunction struct {
	decl          *ast.FuncDecl
	file          *ast.File
	pkg, relative string
}

// constructorViolations 沿可解析的包函数调用图查找副作用；动态接口边界由生命周期测试补充。
func constructorViolations(packages map[string]*sourcePackage) []string {
	functions := make(map[string]constructorFunction)
	for pkg, current := range packages {
		for relative, file := range current.files {
			for _, decl := range file.Decls {
				function, ok := decl.(*ast.FuncDecl)
				if !ok || function.Body == nil {
					continue
				}
				key := pkg + "." + function.Name.Name
				if function.Recv != nil {
					key = constructorType(function.Recv.List[0].Type, pkg, importsByName(file)) + "." + function.Name.Name
				}
				functions[key] = constructorFunction{function, file, pkg, relative}
			}
		}
	}
	var violations []string
	for key, root := range functions {
		if !strings.HasPrefix(root.decl.Name.Name, "New") {
			continue
		}
		seen := make(map[string]bool)
		var visit func(string)
		visit = func(key string) {
			if seen[key] {
				return
			}
			seen[key] = true
			current, ok := functions[key]
			if !ok {
				return
			}
			imports := importsByName(current.file)
			environment := constructorLocals(current, functions)
			ast.Inspect(current.decl.Body, func(node ast.Node) bool {
				if _, ok := node.(*ast.GoStmt); ok {
					violations = append(violations, root.relative+":"+root.decl.Name.Name+" -> "+key+": goroutine")
				}
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				target := call.Fun
				if indexed, ok := target.(*ast.IndexExpr); ok {
					target = indexed.X
				}
				if indexed, ok := target.(*ast.IndexListExpr); ok {
					target = indexed.X
				}
				switch function := target.(type) {
				case *ast.Ident:
					visit(current.pkg + "." + function.Name)
				case *ast.SelectorExpr:
					dependency := ""
					if receiver, ok := function.X.(*ast.Ident); ok {
						dependency = imports[receiver.Name]
					}
					if dependency != "" {
						visit(dependency + "." + function.Sel.Name)
					} else {
						receiverType := constructorExpressionType(function.X, current, environment, functions)
						visit(receiverType + "." + function.Sel.Name)
						dependency = receiverType
					}
					if constructorIOCall(dependency, function.Sel.Name) {
						violations = append(violations, root.relative+":"+root.decl.Name.Name+" -> "+key+": I/O "+dependency+"."+function.Sel.Name)
					}
				}
				return true
			})
		}
		visit(key)
	}
	return violations
}

func constructorIOCall(pkg, name string) bool {
	switch pkg {
	case "os":
		switch name {
		case "Open", "OpenFile", "Create", "ReadFile", "WriteFile", "ReadDir", "Mkdir", "MkdirAll", "Remove", "RemoveAll", "Rename", "Stat", "Lstat", "Getwd", "Chdir", "Chmod", "Chown", "Symlink", "Readlink", "CreateTemp", "MkdirTemp":
			return true
		}
	case "os.File", "io.Reader", "io.Writer", "net.Conn", "database/sql.DB", "os/exec.Cmd":
		switch name {
		case "Read", "Write", "Sync", "Close", "Start", "Run", "Output", "CombinedOutput", "Query", "Exec":
			return true
		}
	case "net", "net/http", "database/sql":
		switch name {
		case "Dial", "DialTimeout", "Listen", "ListenPacket", "Get", "Post", "PostForm", "ListenAndServe", "Open":
			return true
		}
	}
	return false
}

func TestAllConstructorsHaveExplicitLifecycle(t *testing.T) {
	assertNoViolations(t, constructorViolations(loadRepositorySource(t).packages))
}

// constructorType 解析显式类型，不把动态接口猜测为某个具体实现。
func constructorType(expression ast.Expr, pkg string, imports map[string]string) string {
	switch value := expression.(type) {
	case *ast.StarExpr:
		return constructorType(value.X, pkg, imports)
	case *ast.Ident:
		return pkg + "." + value.Name
	case *ast.SelectorExpr:
		if owner, ok := value.X.(*ast.Ident); ok && imports[owner.Name] != "" {
			return imports[owner.Name] + "." + value.Sel.Name
		}
	}
	return ""
}

func constructorLocals(current constructorFunction, functions map[string]constructorFunction) map[string]string {
	locals := make(map[string]string)
	for _, fields := range []*ast.FieldList{current.decl.Recv, current.decl.Type.Params} {
		if fields == nil {
			continue
		}
		for _, field := range fields.List {
			for _, name := range field.Names {
				locals[name.Name] = constructorType(field.Type, current.pkg, importsByName(current.file))
			}
		}
	}
	ast.Inspect(current.decl.Body, func(node ast.Node) bool {
		switch value := node.(type) {
		case *ast.ValueSpec:
			for index, name := range value.Names {
				if value.Type != nil {
					locals[name.Name] = constructorType(value.Type, current.pkg, importsByName(current.file))
				} else if index < len(value.Values) {
					locals[name.Name] = constructorExpressionType(value.Values[index], current, locals, functions)
				}
			}
		case *ast.AssignStmt:
			for index, left := range value.Lhs {
				name, ok := left.(*ast.Ident)
				if ok && index < len(value.Rhs) {
					if inferred := constructorExpressionType(value.Rhs[index], current, locals, functions); inferred != "" {
						locals[name.Name] = inferred
					}
				}
			}
		}
		return true
	})
	return locals
}

func constructorExpressionType(expression ast.Expr, current constructorFunction, locals map[string]string, functions map[string]constructorFunction) string {
	imports := importsByName(current.file)
	switch value := expression.(type) {
	case *ast.Ident:
		return locals[value.Name]
	case *ast.UnaryExpr:
		return constructorExpressionType(value.X, current, locals, functions)
	case *ast.ParenExpr:
		return constructorExpressionType(value.X, current, locals, functions)
	case *ast.CompositeLit:
		return constructorType(value.Type, current.pkg, imports)
	case *ast.SelectorExpr:
		owner := constructorExpressionType(value.X, current, locals, functions)
		for _, definition := range functions {
			for _, decl := range definition.file.Decls {
				group, ok := decl.(*ast.GenDecl)
				if !ok {
					continue
				}
				for _, spec := range group.Specs {
					named, ok := spec.(*ast.TypeSpec)
					if !ok || definition.pkg+"."+named.Name.Name != owner {
						continue
					}
					structure, ok := named.Type.(*ast.StructType)
					if !ok {
						continue
					}
					for _, field := range structure.Fields.List {
						for _, name := range field.Names {
							if name.Name == value.Sel.Name {
								return constructorType(field.Type, definition.pkg, importsByName(definition.file))
							}
						}
					}
				}
			}
		}
	case *ast.CallExpr:
		key := ""
		switch target := value.Fun.(type) {
		case *ast.Ident:
			key = current.pkg + "." + target.Name
		case *ast.SelectorExpr:
			if owner, ok := target.X.(*ast.Ident); ok && imports[owner.Name] != "" {
				key = imports[owner.Name] + "." + target.Sel.Name
			}
		}
		if called, ok := functions[key]; ok && called.decl.Type.Results != nil && len(called.decl.Type.Results.List) > 0 {
			return constructorType(called.decl.Type.Results.List[0].Type, called.pkg, importsByName(called.file))
		}
	}
	return ""
}
