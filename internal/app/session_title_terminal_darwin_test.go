package app

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/headless"
)

// TestTitleTerminalHelper 由 Darwin PTY 验收在独立进程内调用真实宿主入口。
func TestTitleTerminalHelper(t *testing.T) {
	mode := os.Getenv("EASYCODE_TITLE_SMOKE_MODE")
	if mode == "" {
		return
	}
	root := os.Getenv("EASYCODE_TITLE_SMOKE_ROOT")
	data := filepath.Join(root, "sessions")
	snapshot := filepath.Join(root, "catalog.json")
	ctx := context.Background()
	if mode == "list" {
		outcome := ListSessions(ctx, SessionListOptions{All: true, Limit: 20, SessionDataRoot: data, SessionCatalogPath: snapshot, Output: os.Stdout, ErrorOutput: os.Stderr})
		if outcome.ExitCode() != 0 {
			t.Fatalf("offline list failed: %#v", outcome)
		}
		return
	}
	family := domain.ProviderFamily(os.Getenv("EASYCODE_TITLE_SMOKE_PROVIDER"))
	if !family.Valid() {
		t.Fatal("terminal fixture provider is invalid")
	}
	for _, key := range []string{"EASYCODE_PROVIDER", "EASYCODE_BASE_URL", "EASYCODE_API_KEY", "EASYCODE_MODEL"} {
		t.Setenv(key, "")
	}
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Error(err)
			return
		}
		calls.Add(1)
		if !bytes.Contains(body, []byte("Continue selected session")) || bytes.Contains(body, []byte("ReviewAlpha")) || bytes.Contains(body, []byte("Working draft")) {
			t.Error("provider did not receive only the resumed input and original context")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		serveWriteScenarioSample(w, family, 81, nil)
	}))
	defer server.Close()
	options := Options{Mode: headless.ModeInteractive, ConfigPath: writeAppConfig(t, root, family, server.URL), SessionDataRoot: data, SessionCatalogPath: snapshot, Input: os.Stdin, Output: os.Stdout}
	var outcome Outcome
	want := int32(0)
	switch mode {
	case "new":
		outcome = Run(ctx, options)
	case "picker":
		outcome = RunSessionPicker(ctx, options, true)
		want = 1
	default:
		t.Fatal("terminal fixture mode is invalid")
	}
	if outcome.ExitCode() != 0 || calls.Load() != want {
		t.Fatalf("terminal flow failed: exit=%d, requests=%d, result=%#v", outcome.ExitCode(), calls.Load(), outcome.Failure)
	}
	fmt.Fprintln(os.Stdout, "TITLE_SMOKE_DONE")
}
