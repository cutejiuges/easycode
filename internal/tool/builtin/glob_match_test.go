package builtin

import (
	"strings"
	"testing"
)

func TestGlobMatcherSegmentSemantics(t *testing.T) {
	t.Parallel()
	tests := []struct {
		pattern   string
		candidate string
		want      bool
	}{
		{pattern: "internal/**/*.go", candidate: "internal/main.go", want: true},
		{pattern: "internal/**/*.go", candidate: "internal/tool/builtin/glob.go", want: true},
		{pattern: "internal/**/*.go", candidate: "cmd/main.go", want: false},
		{pattern: "src/?ain.[ch]", candidate: "src/main.c", want: true},
		{pattern: "src/?ain.[ch]", candidate: "src/main.go", want: false},
		{pattern: "**", candidate: ".config/tool.yaml", want: true},
		{pattern: "*.go", candidate: "nested/main.go", want: false},
	}
	for _, test := range tests {
		matcher, err := compileGlobMatcher(test.pattern)
		if err != nil {
			t.Fatalf("编译 %q: %v", test.pattern, err)
		}
		if got := matcher.Match(test.candidate); got != test.want {
			t.Fatalf("%q match %q = %t, want %t", test.pattern, test.candidate, got, test.want)
		}
	}
}

func TestGlobMatcherRejectsInvalidPatternsAndPaths(t *testing.T) {
	t.Parallel()
	for _, pattern := range []string{"", "/tmp/*", "../*", "a//b", "[", strings.Repeat("x", 4097), strings.Repeat("a/", 256) + "z"} {
		if _, err := compileGlobMatcher(pattern); err == nil {
			t.Fatalf("非法 pattern %q 未被拒绝", pattern)
		}
	}
	matcher, err := compileGlobMatcher("**/*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, candidate := range []string{"/absolute/main.go", "../main.go", "a//main.go", `a\main.go`} {
		if matcher.Match(candidate) {
			t.Fatalf("非法候选路径 %q 被接受", candidate)
		}
	}
}

func TestGlobMatcherUsesSlashIndependentOfHostSeparator(t *testing.T) {
	t.Parallel()
	matcher, err := compileGlobMatcher("src/**/*.go")
	if err != nil {
		t.Fatal(err)
	}
	if !matcher.Match("src/nested/main.go") || matcher.Match(`src\nested\main.go`) {
		t.Fatal("matcher 未坚持规范 / 分隔符")
	}
}
