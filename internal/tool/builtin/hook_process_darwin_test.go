//go:build darwin

package builtin

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"easycode/internal/domain"
)

func runHookFixture(t *testing.T, root, command string) domain.HookRunResult {
	t.Helper()
	w, err := OpenWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	env := filterCommandEnvironment([]string{"PATH=/usr/bin:/bin", "API_KEY=secret-sentinel", "HTTPS_PROXY=proxy-secret"})
	env.executable, _ = os.Executable()
	parse := func(exit int, b []byte) (domain.HookOutcome, domain.HookCode) {
		if exit == 2 {
			return domain.HookBlock, domain.HookDenied
		}
		if exit != 0 {
			return domain.HookError, domain.HookExitFailed
		}
		if !bytes.Equal(bytes.TrimSpace(b), []byte("{}")) {
			return domain.HookError, domain.HookOutputInvalid
		}
		return domain.HookContinue, domain.HookOK
	}
	r, err := NewHookRunner(w, env, parse)
	if err != nil {
		t.Fatal(err)
	}
	if code := r.Preflight(context.Background()); code != domain.HookOK {
		t.Fatal(code)
	}
	d, err := domain.NewHookDeclaration(domain.HookSourceProject, strings.Repeat("a", 64), "check", 7, command, 5)
	if err != nil {
		t.Fatal(err)
	}
	id := "018f1d8a-7b5c-7def-8abc-456789abcdef"
	p, _ := domain.NewHookPlan(id, domain.HookExec, []domain.HookReference{d.Reference})
	run, _ := domain.NewHookRunIdentity(p, 0)
	i, _ := domain.NewHookExecInput("echo model-command", ".", 1000)
	input, err := domain.NewHookInput(domain.SessionID(id), domain.ThreadID(id), domain.TurnID(id), "call", "exec_command", root, i)
	if err != nil {
		t.Fatal(err)
	}
	result := r.Run(context.Background(), run, d, input, 5*time.Second)
	if result.Validate() != nil {
		t.Fatal("invalid hook result")
	}
	return result
}
func TestHookProcessReceivesStdinWithoutGrantingWritesOrNetwork(t *testing.T) {
	root := t.TempDir()
	private := filepath.Join(t.TempDir(), "session-secret")
	if err := os.WriteFile(private, []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	port := listener.Addr().(*net.TCPAddr).Port
	command := fmt.Sprintf(`input=$(/bin/cat); case "$input" in *'"tool_name":"exec_command"'*) ;; *) exit 9;; esac; test -z "$API_KEY$HTTPS_PROXY" || exit 8; test -r . || exit 7; (printf bad > forbidden-file) 2>/dev/null && exit 6; /bin/cat %q >/dev/null 2>&1 && exit 5; /usr/bin/nc -z 127.0.0.1 %d >/dev/null 2>&1 && exit 4; printf '{}'`, private, port)
	result := runHookFixture(t, root, command)
	if result.Status != domain.HookContinue || !result.ExitKnown || result.ExitCode != 0 {
		t.Fatalf("restricted hook failed: %#v", result)
	}
	if _, err := os.Stat(filepath.Join(root, "forbidden-file")); !os.IsNotExist(err) {
		t.Fatal("hook wrote workspace")
	}
}
func TestHookProcessBoundsOutputAndPreservesExitClassification(t *testing.T) {
	for _, test := range []struct {
		command string
		code    domain.HookCode
	}{
		{"cat >/dev/null; printf '{}'", domain.HookOK},
		{"cat >/dev/null; printf secret-sentinel >&2; exit 2", domain.HookDenied},
		{"cat >/dev/null; exit 3", domain.HookExitFailed},
		{"cat >/dev/null; head -c 18000 /dev/zero", domain.HookOutputLimit},
	} {
		result := runHookFixture(t, t.TempDir(), test.command)
		if result.Code != test.code {
			t.Fatalf("want %s got %#v", test.code, result)
		}
	}
}
