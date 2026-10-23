package builtin

import (
	"reflect"
	"strings"
	"testing"
)

func TestCommandEnvironmentUsesAnImmutableNameAllowlist(t *testing.T) {
	source := []string{"PATH=/usr/bin:/bin", "LANG=en_US.UTF-8", "LC_ALL=C", "NO_COLOR=0", "PAGER=unsafe", "TERM=xterm"}
	for _, name := range []string{"API_KEY", "OPENAI_API_KEY", "ANTHROPIC_API_KEY", "AUTHORIZATION", "COOKIE", "HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "EASYCODE_SESSION", "BASH_ENV", "ENV", "DYLD_INSERT_LIBRARIES", "HOME", "TMPDIR", "GOCACHE", "UNKNOWN"} {
		source = append(source, name+"=private-secret")
	}
	environment := filterCommandEnvironment(source)
	want := []string{"GIT_PAGER=cat", "GIT_TERMINAL_PROMPT=0", "LANG=en_US.UTF-8", "LC_ALL=C", "NO_COLOR=1", "PAGER=cat", "PATH=/usr/bin:/bin", "TERM=dumb"}
	if !reflect.DeepEqual(environment.entries, want) {
		t.Fatalf("unexpected allowlist snapshot: %v", environment.entries)
	}
	source[0] = "PATH=mutated"
	if !reflect.DeepEqual(environment.entries, want) {
		t.Fatal("environment aliases source")
	}
	for _, entry := range environment.entries {
		if strings.Contains(entry, "private-secret") {
			t.Fatal("secret entered command environment")
		}
	}
	fallback := filterCommandEnvironment([]string{"PATH=bad\x00path", "invalid"})
	if !strings.Contains(strings.Join(fallback.entries, "\n"), "PATH=/usr/bin:/bin:/usr/sbin:/sbin") {
		t.Fatal("invalid PATH did not use safe default")
	}
}
