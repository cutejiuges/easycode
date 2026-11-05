package tui

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func testHighlightLimits(n int) highlightLimits {
	return highlightLimits{128 * (n + 1), 32768, 2 << 20, 64}
}
func requireHighlightCoverage(t *testing.T, source string, result highlightResult) {
	t.Helper()
	if result.failure != highlightOK {
		t.Fatalf("scan failed: %v, steps=%d", result.failure, result.steps)
	}
	offset := 0
	for _, token := range result.tokens {
		if int(token.start) != offset || token.end <= token.start || !highlightRuneBoundary(source, int(token.start)) || !highlightRuneBoundary(source, int(token.end)) {
			t.Fatalf("invalid interval: %+v at %d", token, offset)
		}
		offset = int(token.end)
	}
	if offset != len(source) {
		t.Fatalf("coverage=%d want=%d", offset, len(source))
	}
}
func TestHighlightLexicalFixtures(t *testing.T) {
	tests := []struct {
		name, source, word string
		kind               highlightKind
	}{
		{"go", "package main\nvar 文 = `//raw\ntext`\n// func", "package", highlightKeyword},
		{"go", "x := \"// func\"", "\"// func\"", highlightString},
		{"json", "{\"key\" : \"value\", \"ok\": true}", "\"key\"", highlightProperty},
		{"python", "x = r'''# def\ntext'''\ndef f(): pass", "r'''# def\ntext'''", highlightString},
		{"python", "x = f\"{obj[\"key\"]}\"\nreturn x", "f\"{obj[\"key\"]}\"", highlightString},
		{"js", "const x = `a ${fn({x: `b ${1}`})}`;", "const", highlightKeyword},
		{"ts", "interface X { value: string }", "interface", highlightKeyword},
		{"js", "return /[/\\/]+/g", "/[/\\/]+/g", highlightString},
		{"bash", "echo ${x:-$(echo hi)} # comment", "${x:-$(echo hi)}", highlightVariable},
		{"bash", "echo \"hello $USER\"", "$USER", highlightVariable},
		{"shell", "cat <<'END'\nbody # literal\nEND\necho ok", "\nbody # literal\nEND\n", highlightString},
		{"go", "var 数 = 0x1_f.0p-2i+1", "0x1_f.0p-2i", highlightNumber},
		{"go", "n := 0b101+0o75", "0o75", highlightNumber},
		{"go", "/* func */ return", "/* func */", highlightComment},
		{"json", `{"a\"b":"\u001b"}`, `"a\"b"`, highlightProperty},
		{"python", "value = br'abc' # return", "br'abc'", highlightString},
		{"ts", "let answer = 123n+2", "123n", highlightNumber},
		{"js", "value / 2", "/", highlightOperator},
		{"js", "const x = `a ${fn({x: `b ${1}`})}`;", "`a ${fn({x: `b ${1}`})}`", highlightString},
		{"shell", "echo $'a\\nb'", "$'a\\nb'", highlightString},
		{"shell", "cat <<-END\n\tbody\n\tEND\nnext", "\n\tbody\n\tEND\n", highlightString},
	}
	for _, test := range tests {
		t.Run(test.name+test.word, func(t *testing.T) {
			result := scanHighlight(test.source, codeLanguage(test.name), testHighlightLimits(len(test.source)))
			requireHighlightCoverage(t, test.source, result)
			found := false
			for _, token := range result.tokens {
				if test.source[token.start:token.end] == test.word && token.kind == test.kind {
					found = true
				}
			}
			if !found {
				t.Fatalf("missing %q kind=%v tokens=%+v", test.word, test.kind, result.tokens)
			}
		})
	}
}
func TestHighlightAliasesAndUnknown(t *testing.T) {
	for _, alias := range []string{"go", "golang", "json", "py", "python", "python3", "js", "javascript", "mjs", "cjs", "ts", "typescript", "sh", "bash", "shell", "zsh"} {
		if codeLanguage(strings.ToUpper(alias)+" title") == highlightPlain {
			t.Fatalf("unsupported %s", alias)
		}
	}
	if codeLanguage("unknown") != highlightPlain {
		t.Fatal("unknown language guessed")
	}
}
func TestHighlightBudgetAndIsolation(t *testing.T) {
	source := "const x = `hello ${fn({a: 1})}`;\n"
	good := scanHighlight(source, highlightJS, testHighlightLimits(len(source)))
	requireHighlightCoverage(t, source, good)
	for _, field := range []string{"steps", "nodes", "bytes", "depth"} {
		t.Run(field, func(t *testing.T) {
			limit := testHighlightLimits(len(source))
			switch field {
			case "steps":
				limit.steps = good.steps - 1
			case "nodes":
				limit.nodes = 1
			case "bytes":
				limit.bytes = 1
			case "depth":
				limit.depth = 1
			}
			got := scanHighlight(source, highlightJS, limit)
			if got.failure != highlightBudgetExceeded || got.tokens != nil {
				t.Fatalf("budget bypass: %+v", got)
			}
		})
	}
	exact := testHighlightLimits(len(source))
	exact.steps = good.steps
	requireHighlightCoverage(t, source, scanHighlight(source, highlightJS, exact))
	for range 10 {
		scanHighlight("\"unterminated", highlightGo, testHighlightLimits(13))
		requireHighlightCoverage(t, source, scanHighlight(source, highlightJS, testHighlightLimits(len(source))))
	}
	for _, language := range []highlightLanguage{highlightGo, highlightJSON, highlightPython, highlightJS, highlightTS, highlightShell} {
		t.Run(string(rune('a'+language)), func(t *testing.T) {
			t.Parallel()
			for range 100 {
				requireHighlightCoverage(t, source, scanHighlight(source, language, testHighlightLimits(len(source))))
			}
		})
	}
}
func TestHighlightUnicodeAndBoundaries(t *testing.T) {
	for _, source := range []string{"", "\r\n\t", "变量 := \"👩‍💻é\"", strings.Repeat("x", 64<<10), strings.Repeat("`", 256<<10)} {
		result := scanHighlight(source, highlightGo, testHighlightLimits(len(source)))
		requireHighlightCoverage(t, source, result)
	}
	got := scanHighlight(strings.Repeat("x", (256<<10)+1), highlightGo, testHighlightLimits(256<<10))
	if got.failure != highlightBudgetExceeded {
		t.Fatal("input bound ignored")
	}
	got = scanHighlight(string([]byte{0xff}), highlightGo, testHighlightLimits(1))
	if got.failure != highlightScanFailed {
		t.Fatal("invalid UTF-8 accepted")
	}
}
func FuzzHighlightCoverage(f *testing.F) {
	for _, seed := range []string{"", "hello", "`a ${fn({x: 1})}`", "f\"{x[\"key\"]}\"", "cat <<EOF\na\nEOF\n", "👩‍💻é", strings.Repeat("${", 65)} {
		f.Add(seed, uint8(1))
	}
	f.Fuzz(func(t *testing.T, source string, language uint8) {
		if len(source) > 256<<10 || !utf8.ValidString(source) {
			t.Skip()
		}
		limits := testHighlightLimits(len(source))
		result := scanHighlight(source, highlightLanguage(language%7), limits)
		if result.steps > limits.steps {
			t.Fatal("work bound exceeded")
		}
		if result.failure == highlightOK {
			requireHighlightCoverage(t, source, result)
		} else if result.failure != highlightBudgetExceeded {
			t.Fatalf("valid UTF-8 scan failed: %q", source)
		}
	})
}

func TestHighlightDepthQueueAndAllocationBoundaries(t *testing.T) {
	for _, n := range []int{63, 64, 65} {
		source := strings.Repeat("cat <<X ", n) + "\n" + strings.Repeat("X\n", n)
		result := scanHighlight(source, highlightShell, testHighlightLimits(len(source)))
		if n == 65 {
			if result.failure != highlightBudgetExceeded || len(result.tokens) != 0 {
				t.Fatal("published overflowing here-document queue")
			}
		} else {
			requireHighlightCoverage(t, source, result)
		}
	}
	source := strings.Repeat("a;", 50)
	base := testHighlightLimits(len(source))
	full := scanHighlight(source, highlightGo, base)
	requireHighlightCoverage(t, source, full)
	for _, delta := range []int{-1, 0, 1} {
		limit := base
		limit.bytes = full.peak + delta
		result := scanHighlight(source, highlightGo, limit)
		if delta < 0 {
			if result.failure != highlightBudgetExceeded || len(result.tokens) != 0 {
				t.Fatal("old and new token capacities not reserved together")
			}
		} else {
			requireHighlightCoverage(t, source, result)
		}
	}
}
