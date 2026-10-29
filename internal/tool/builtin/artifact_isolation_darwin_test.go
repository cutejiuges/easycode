//go:build darwin

package builtin

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"easycode/internal/session/artifact"
)

func TestArtifactLayoutMatchesActualSandboxAccess(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	w, s := filepath.Join(root, "workspace"), filepath.Join(root, "storage")
	for _, p := range []string{w, s} {
		if err = os.Mkdir(p, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	plan := sandboxTestPlan(t, w)
	l, err := artifact.OpenLayout(context.Background(), s, w, plan.ReadRoots(), plan.WriteRoots())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	path := filepath.Join(s, "output")
	if err = os.WriteFile(path, []byte("synthetic-private-output"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, script := range []string{`cat "$1"`, `printf modified > "$1"`} {
		cmd := exec.Command("/usr/bin/sandbox-exec", "-p", commandSandboxProfile(plan), "/bin/sh", "-c", script, "isolation-test", path)
		if output, err := cmd.CombinedOutput(); err == nil {
			t.Fatalf("sandbox accessed private output: %q", output)
		}
	}
	b, err := os.ReadFile(path)
	if err != nil || string(b) != "synthetic-private-output" {
		t.Fatal("private object changed")
	}
	inside := filepath.Join(w, "storage")
	if err = os.Mkdir(inside, 0o700); err != nil {
		t.Fatal(err)
	}
	if bad, err := artifact.OpenLayout(context.Background(), inside, w, plan.ReadRoots(), plan.WriteRoots()); err == nil {
		_ = bad.Close()
		t.Fatal("sandbox-writable storage accepted")
	}
}
