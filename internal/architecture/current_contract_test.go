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

func codecBoundaryViolations(current *sourcePackage) []string {
	var violations []string
	for relative, file := range current.files {
		imports := importsByName(file)
		ast.Inspect(file, func(node ast.Node) bool {
			selector, ok := node.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			receiver, ok := selector.X.(*ast.Ident)
			if !ok {
				return true
			}
			dependency := imports[receiver.Name]
			if dependency == "encoding/json" && current.path != "easycode/internal/codec" {
				switch selector.Sel.Name {
				case "Marshal", "Unmarshal", "NewDecoder", "NewEncoder", "Valid", "Compact", "Indent":
					violations = append(violations, relative+": JSON operation outside codec")
				}
			}
			if dependency == sonicImportPath {
				switch selector.Sel.Name {
				case "ConfigStd", "ConfigDefault", "ConfigFastest":
					violations = append(violations, relative+": mutable third-party codec")
				}
			}
			return true
		})
		for _, decl := range file.Decls {
			if function, ok := decl.(*ast.FuncDecl); ok {
				switch function.Name.Name {
				case "NewPreparedSample", "NewPreparedToolOutputs":
					callbacks := 0
					for _, field := range function.Type.Params.List {
						if _, ok := field.Type.(*ast.FuncType); ok {
							callbacks += len(field.Names)
						}
					}
					if callbacks != 2 {
						violations = append(violations, relative+": prepared lifecycle requires explicit finalize and discard")
					}
				case "NewPreparedSampleWithDiscard", "NewPreparedToolOutputsWithDiscard":
					violations = append(violations, relative+": obsolete prepared constructor")
				}
			}
		}
	}
	return violations
}

func TestCurrentCodecBoundaries(t *testing.T) {
	repository := loadRepositorySource(t)
	var violations []string
	for _, current := range repository.packages {
		violations = append(violations, codecBoundaryViolations(current)...)
	}
	assertNoViolations(t, violations)
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

func TestCurrentContractGuardNegativeAndPositiveCases(t *testing.T) {
	for _, source := range []string{
		`package fixture; import "encoding/json"; func decode(){json.Unmarshal(nil,nil)}`,
		`package fixture; import "github.com/bytedance/sonic"; func encode(){sonic.ConfigStd.Marshal(nil)}`,
		`package fixture; func NewPreparedSampleWithDiscard(){}`,
		`package fixture; func NewPreparedSample(envelope int, finalize func()) {}`,
	} {
		file, err := parser.ParseFile(token.NewFileSet(), "fixture.go", source, 0)
		if err != nil {
			t.Fatal(err)
		}
		current := &sourcePackage{path: "easycode/internal/tool", files: map[string]*ast.File{"fixture.go": file}}
		if len(codecBoundaryViolations(current)) == 0 {
			t.Fatal("codec violation escaped", source)
		}
	}
	for _, source := range []string{
		`package fixture; import "os"; func NewResource(){os.Open("x")}`,
		`package fixture; import "os"; func NewResource(){load()}; func load(){os.ReadFile("x")}`,
		`package fixture; func NewResource(){launch()}; func launch(){go func(){}()}`,
		`package fixture; import "os"; type resource struct{}; func NewResource(){ r:=&resource{}; r.load() }; func (*resource) load(){os.Open("x")}`,
		`package fixture; import "os"; type resource struct{}; func (*resource) NewChild(){os.Open("x")}`,
		`package fixture; import "os"; func NewResource(file *os.File){file.Read(nil)}`,
		`package fixture; import "os"; type resource struct{ child *child }; type child struct{}; func NewResource(){r:=&resource{};r.child.load()};func (*child) load(){os.Open("x")}`,
	} {
		file, err := parser.ParseFile(token.NewFileSet(), "fixture.go", source, 0)
		if err != nil {
			t.Fatal(err)
		}
		current := &sourcePackage{path: "fixture", files: map[string]*ast.File{"fixture.go": file}}
		if len(constructorViolations(map[string]*sourcePackage{"fixture": current})) == 0 {
			t.Fatal("constructor violation escaped", source)
		}
	}
	file, err := parser.ParseFile(token.NewFileSet(), "fixture.go", `package fixture; import "encoding/json"; const EnvelopeVersion=3; const nativeCommitPayloadRevision=1; type Opaque struct { Raw json.RawMessage }; func NewValue() Opaque { return Opaque{} }`, 0)
	if err != nil {
		t.Fatal(err)
	}
	current := &sourcePackage{path: "fixture", files: map[string]*ast.File{"fixture.go": file}}
	if len(codecBoundaryViolations(current))+len(constructorViolations(map[string]*sourcePackage{"fixture": current}))+len(singleCurrentDeclarationViolations(current)) != 0 {
		t.Fatal("current canary or opaque value rejected")
	}
	if !isCoreContractPackage("easycode/internal/tool") {
		t.Fatal("tool core contract excluded")
	}
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
