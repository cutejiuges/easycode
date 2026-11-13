package app

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"easycode/internal/config"
	"easycode/internal/domain"
	"easycode/internal/protocol"
	chatRuntime "easycode/internal/runtime"
	"easycode/internal/secret"
	"easycode/internal/tool/builtin"
)

type blockingAppHook struct{ entered, cancelled, release chan struct{} }

func (*blockingAppHook) Preflight(context.Context) domain.HookCode { return domain.HookOK }
func (r *blockingAppHook) Run(ctx context.Context, id domain.HookRunIdentity, _ domain.HookDeclaration, _ domain.HookInput, _ time.Duration) domain.HookExecution {
	close(r.entered)
	<-ctx.Done()
	close(r.cancelled)
	<-r.release
	return domain.HookExecution{Result: domain.HookRunResult{Identity: id, Status: domain.HookCancel, Code: domain.HookCancelled, Cancelled: true, CleanupConfirmed: true, Origin: "execution"}}
}
func TestHookHostRetainsWorkspaceAndCrossProcessLeaseDuringCleanup(t *testing.T) {
	testHookHostRetainsLease(t, domain.HookPreToolUse)
}
func TestPostHookHostRetainsWorkspaceAndCrossProcessLeaseDuringCleanup(t *testing.T) {
	testHookHostRetainsLease(t, domain.HookPostToolUse)
}
func TestFailureHookHostRetainsWorkspaceAndCrossProcessLeaseDuringCleanup(t *testing.T) {
	testHookHostRetainsLease(t, domain.HookPostToolUseFailure)
}
func TestStopHookHostRetainsWorkspaceAndCrossProcessLeaseDuringCleanup(t *testing.T) {
	testHookHostRetainsLease(t, domain.HookStop)
}
func testHookHostRetainsLease(t *testing.T, event domain.HookEvent) {
	t.Helper()
	workspace := t.TempDir()
	if event == domain.HookStop {
		writeStopHookConfig(t, workspace, "never executed by fake runner", domain.HookFeedbackNone)
	} else if event == domain.HookPostToolUseFailure {
		writeFailureHookConfig(t, workspace, "never executed by fake runner", domain.HookFeedbackNone)
	} else if event == domain.HookPostToolUse {
		writePostHookConfig(t, workspace, "never executed by fake runner", domain.HookFeedbackNone)
	} else {
		writeHookConfig(t, workspace, "never executed by fake runner")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		sample := 1
		if event == domain.HookStop {
			serveWriteScenarioSample(w, domain.ProviderOpenAI, 2, nil)
			return
		}
		serveWriteScenarioSample(w, domain.ProviderOpenAI, sample, []writeScenarioCall{{"Write", `{"file_path":"marker","content":"must-not-write"}`}})
	}))
	defer server.Close()
	root := filepath.Join(t.TempDir(), "sessions")
	runner := &blockingAppHook{make(chan struct{}), make(chan struct{}), make(chan struct{})}
	cfg := config.Config{Provider: config.Provider{Family: domain.ProviderOpenAI, Model: "fixture-model", BaseURL: server.URL, APIKey: secret.New("fixture-key")}}
	resources, err := openChatResourcesWithHooks(context.Background(), cfg, dataPaths{sessionRoot: root}, "", false, workspace, chatResourceHooks{interactiveApprovals: true, newHookRunner: func(*builtin.Workspace, builtin.CommandEnvironment, builtin.HookOutputParser) (chatRuntime.HookRunner, error) {
		return runner, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := resources.session.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	events, err := resources.session.Submit(context.Background(), "run")
	if err != nil {
		t.Fatal(err)
	}
	for e := range events {
		if e.Kind == protocol.EventTurnFailed {
			t.Fatal("turn failed before blocked hook", string(e.Payload))
		}
		if e.Kind == protocol.EventHookTrustRequested {
			p, _ := protocol.DecodeHookTrustRequested(e)
			if err := resources.session.ResolveHookTrust(context.Background(), e.TurnID, p, protocol.HookTrustSession); err != nil {
				t.Fatal(err)
			}
		}
		if e.Kind == protocol.EventHookActivity {
			break
		}
		if e.Kind == protocol.EventToolApprovalRequested {
			if event == domain.HookPreToolUse {
				t.Fatal("tool escaped pre hook")
			}
			if event == domain.HookPostToolUseFailure {
				if err := os.WriteFile(filepath.Join(workspace, "marker"), []byte("external change"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			p, _ := protocol.DecodeToolApprovalRequested(e)
			if err := resources.session.ResolveApproval(context.Background(), e.TurnID, p, protocol.ApprovalAllowOnce, ""); err != nil {
				t.Fatal(err)
			}
		}
		if e.Kind == protocol.EventToolStarted && event == domain.HookPreToolUse {
			t.Fatal("tool escaped pre hook")
		}
	}
	<-runner.entered
	resources.session.Interrupt()
	<-runner.cancelled
	deadline, cancel := context.WithCancel(context.Background())
	cancel()
	if err := resources.session.Shutdown(deadline); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	probe := func(expected string) {
		cmd := exec.Command(os.Args[0], "-test.run=^TestHostLeaseProbe$")
		cmd.Env = append(os.Environ(), "EASYCODE_HOST_LEASE_ROOT="+root, "EASYCODE_HOST_LEASE_THREAD="+string(resources.identity.ThreadID), "EASYCODE_HOST_LEASE_EXPECT="+expected)
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("lease probe: %v %s", err, output)
		}
	}
	probe("busy")
	if _, err := resources.session.Submit(context.Background(), "too early"); err == nil {
		t.Fatal("new turn admitted")
	}
	select {
	case <-resources.session.Done():
		t.Fatal("early owner done")
	default:
	}
	close(runner.release)
	if err := resources.close(context.Background()); err != nil {
		t.Fatal(err)
	}
	probe("free")
	for range events {
	}
	content, err := os.ReadFile(filepath.Join(workspace, "marker"))
	if event == domain.HookPreToolUse || event == domain.HookStop {
		if !os.IsNotExist(err) {
			t.Fatal("target executed during pre cleanup")
		}
	} else if event == domain.HookPostToolUseFailure {
		if err != nil || string(content) != "external change" {
			t.Fatal("failure cleanup rewrote conflicting file", err)
		}
	} else if err != nil || string(content) != "must-not-write" {
		t.Fatal("post cleanup changed real successful effect", err)
	}
}
