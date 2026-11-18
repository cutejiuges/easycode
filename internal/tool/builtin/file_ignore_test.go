package builtin

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestFileIgnorePatternMatrix(t *testing.T) {
	for _, test := range []struct {
		pattern, path   string
		directory, want bool
	}{
		{"*.go", "a.go", false, true}, {"*.go", "nested/a.go", false, true}, {"*.go", "a.go.bak", false, false},
		{"/a.go", "a.go", false, true}, {"/a.go", "nested/a.go", false, false},
		{"a/*.go", "a/x.go", false, true}, {"a/*.go", "a/nested/x.go", false, false},
		{"**/a.go", "a.go", false, true}, {"**/a.go", "x/y/a.go", false, true}, {"**/a.go", "xa.go", false, false},
		{"a/**/b.go", "a/b.go", false, true}, {"a/**/b.go", "a/x/y/b.go", false, true}, {"a/**/b.go", "a/xb.go", false, false},
		{"abc/**", "abc/file", false, true}, {"abc/**", "abc/nested/file", false, true}, {"abc/**", "abc", true, false},
		{"a**b", "axb", false, true}, {"a**b", "a/b", false, false},
		{"file[0-9]?.[ch]", "file2x.c", false, true}, {"file[0-9]?.[ch]", "fileax.c", false, false},
		{"[!a-c]", "d", false, true}, {"[!a-c]", "b", false, false}, {"[!a-c]", "/", false, false},
		{"[[:digit:]][[:upper:]]", "2A", false, true}, {"[[:alpha:]]", "z", false, true}, {"[[:alpha:]]", "3", false, false},
		{"[]a]", "]", false, true}, {"[-a]", "-", false, true}, {"[a-]", "-", false, true},
		{"build/", "build", true, true}, {"build/", "build", false, false}, {"build/", "nested/build", true, true},
		{"\\#name", "#name", false, true}, {"\\!name", "!name", false, true},
		{"name   ", "name", false, true}, {"name\\ ", "name ", false, true}, {"name\\ ", "name", false, false},
		{" #literal", " #literal", false, true}, {"A", "a", false, false}, {"中文?", "中文件", false, true},
	} {
		t.Run(test.pattern+":"+test.path, func(t *testing.T) {
			rule, present, err := compileIgnoreRule("", test.pattern)
			if err != nil || !present {
				t.Fatal("compile", err)
			}
			budget := fileWorkBudget{remaining: 32000000}
			got, err := matchFileIgnore(context.Background(), []fileIgnoreRule{rule}, test.path, test.directory, &budget)
			if err != nil || got != test.want {
				t.Fatalf("match %q %q = %v, expected %v: %v", test.pattern, test.path, got, test.want, err)
			}
		})
	}
	for _, pattern := range []string{"", "# comment", "   "} {
		if _, present, err := compileIgnoreRule("", pattern); err != nil || present {
			t.Fatal("blank/comment compiled", err)
		}
	}
	for _, pattern := range []string{"!", "/", "[abc", "[z-a]", "foo\\", "[[:future:]]", strings.Repeat("a", 4097)} {
		if _, _, err := compileIgnoreRule("", pattern); err == nil {
			t.Fatalf("invalid pattern accepted: %q", pattern)
		}
	}
}

func TestFileIgnoreNestedOrderingAndBoundedWork(t *testing.T) {
	var rules []fileIgnoreRule
	for _, source := range []struct{ scope, pattern string }{{"", "*.go"}, {"", "!keep.go"}, {"sub", "keep.go"}, {"sub", "!again.go"}} {
		rule, _, err := compileIgnoreRule(source.scope, source.pattern)
		if err != nil {
			t.Fatal(err)
		}
		rules = append(rules, rule)
	}
	for _, test := range []struct {
		path    string
		ignored bool
	}{{"a.go", true}, {"keep.go", false}, {"sub/keep.go", true}, {"sub/again.go", false}, {"other/again.go", true}} {
		budget := fileWorkBudget{remaining: 32000000}
		got, err := matchFileIgnore(context.Background(), rules, test.path, false, &budget)
		if err != nil || got != test.ignored {
			t.Fatal(test.path, got, err)
		}
	}
	rule, _, _ := compileIgnoreRule("", strings.Repeat("*a", 100)+"b")
	budget := fileWorkBudget{remaining: 400}
	if _, err := matchIgnoreTokens(context.Background(), rule.tokens, strings.Repeat("a", 100), &budget); !errors.Is(err, errFileDiscoveryWorkLimit) || budget.spent > 400 {
		t.Fatal("work budget bypassed", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	budget = fileWorkBudget{remaining: 32000000}
	if _, err := matchIgnoreTokens(ctx, rule.tokens, "abc", &budget); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation ignored", err)
	}
}

func BenchmarkFileIgnoreBoundedPatterns(b *testing.B) {
	rule, _, _ := compileIgnoreRule("", "src/**/[a-z]*.go")
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		budget := fileWorkBudget{remaining: 32000000}
		if _, err := matchIgnoreTokens(context.Background(), rule.tokens, "src/internal/file_mentions.go", &budget); err != nil {
			b.Fatal(err)
		}
	}
}

func TestFileIgnoreLargeRuleSetStopsAtActualStateBudget(t *testing.T) {
	rule, ok, err := compileIgnoreRule("", strings.Repeat("*a", 100)+"b")
	if err != nil || !ok {
		t.Fatal(err)
	}
	rules := make([]fileIgnoreRule, 4096)
	for i := range rules {
		rules[i] = rule
	}
	budget := fileWorkBudget{remaining: 32000000}
	if _, err := matchFileIgnore(context.Background(), rules, strings.Repeat("a", 100), false, &budget); !errors.Is(err, errFileDiscoveryWorkLimit) || budget.spent > 32000000 || budget.remaining != 0 {
		t.Fatal("actual large rule work escaped budget", budget, err)
	}
}
