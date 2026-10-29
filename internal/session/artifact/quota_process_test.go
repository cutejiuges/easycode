//go:build darwin || linux

package artifact

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/tool"
)

func TestArtifactQuotaChild(t *testing.T) {
	if os.Getenv("EASYCODE_ARTIFACT_TEST_CHILD") != "1" {
		return
	}
	l, err := OpenLayout(context.Background(), os.Getenv("EASYCODE_ARTIFACT_TEST_ROOT"), os.Getenv("EASYCODE_ARTIFACT_TEST_WORKSPACE"), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	s, err := Open(context.Background(), l, domain.ThreadID("018f1d8a-7b5c-7def-8123-456789abcdee"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	s.rootBudget = tool.ArtifactInvocationBytes + 3
	id, _ := tool.GenerateInvocationID()
	c, err := s.Start(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	_, _ = c.WriteStdout([]byte("abc"))
	fmt.Println("reserved")
	_, _ = io.Copy(io.Discard, os.Stdin)
}

func TestQuotaCoordinatesRealProcessAndCrash(t *testing.T) {
	_, w, root := layoutDirectories(t)
	l, err := OpenLayout(context.Background(), root, w, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	s, err := Open(context.Background(), l, domain.ThreadID("018f1d8a-7b5c-7def-8123-456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	s.rootBudget = tool.ArtifactInvocationBytes + 3
	cmd := exec.Command(os.Args[0], "-test.run=^TestArtifactQuotaChild$")
	cmd.Env = append(os.Environ(), "EASYCODE_ARTIFACT_TEST_CHILD=1", "EASYCODE_ARTIFACT_TEST_ROOT="+root, "EASYCODE_ARTIFACT_TEST_WORKSPACE="+w)
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	in, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	waited := false
	defer func() {
		_ = in.Close()
		if !waited {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}()
	line, err := bufio.NewReader(out).ReadString('\n')
	if err != nil || line != "reserved\n" {
		t.Fatalf("child reservation: %q %v", line, err)
	}
	id, _ := tool.GenerateInvocationID()
	if c, err := s.Start(context.Background(), id); err == nil {
		_ = c.Close()
		t.Fatal("cross-process reservation exceeded quota")
	}
	if err = cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	waited = true
	total, _, err := s.usage(context.Background())
	if err != nil || total != 3 {
		t.Fatalf("crash accounting: %d %v", total, err)
	}
	c, err := s.Start(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if err = c.Close(); err != nil {
		t.Fatal(err)
	}
}
