package main

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"easycode/internal/domain"
)

type failingBranchDisplay struct{}

func (failingBranchDisplay) Write(data []byte) (int, error) {
	if bytes.Contains(data, []byte("Branched session ")) {
		return 0, io.ErrClosedPipe
	}
	return len(data), nil
}

func TestRunReportsPublishedBranchWhenTerminalOutputFails(t *testing.T) {
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	for _, key := range []string{"EASYCODE_PROVIDER", "EASYCODE_BASE_URL", "EASYCODE_API_KEY", "EASYCODE_MODEL"} {
		t.Setenv(key, "")
	}
	t.Chdir(t.TempDir())
	var samples atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		samples.Add(1)
		writeCLISuccessResponse(w)
	}))
	defer server.Close()
	configPath := writeCLIConfig(t, server.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	var stdout, stderr bytes.Buffer
	if code := run(ctx, []string{"--config", configPath, "--print", "First question"}, strings.NewReader(""), &stdout, &stderr, false); code != 0 {
		t.Fatalf("could not seed source: exit=%d, stderr=%q", code, stderr.String())
	}
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	inputDone := make(chan struct{})
	go func() {
		defer close(inputDone)
		_, _ = io.WriteString(writer, "/branch Recovery proof\r")
	}()
	code := run(ctx, []string{"--config", configPath, "--continue"}, reader, failingBranchDisplay{}, &stderr, false)
	_ = reader.Close()
	<-inputDone
	_, notice, found := strings.Cut(stderr.String(), "Session branch saved. Resume with easycode --resume ")
	target, parseErr := domain.ParseThreadID(strings.TrimSpace(notice))
	if code != 1 || !strings.Contains(stderr.String(), "write session picker output failed") || !found || parseErr != nil || samples.Load() != 1 {
		t.Fatalf("CLI lost durable branch result after terminal failure: exit=%d, stderr=%q, samples=%d", code, stderr.String(), samples.Load())
	}

	stdout.Reset()
	stderr.Reset()
	if code := run(ctx, []string{"--config", configPath, "--print", "--resume", string(target), "Continue the branch"}, strings.NewReader(""), &stdout, &stderr, false); code != 0 ||
		strings.TrimSpace(stdout.String()) != "answer" || samples.Load() != 2 {
		t.Fatalf("reported branch could not be resumed: exit=%d, stdout=%q, stderr=%q, samples=%d", code, stdout.String(), stderr.String(), samples.Load())
	}
}
