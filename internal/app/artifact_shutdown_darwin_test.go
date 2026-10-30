//go:build darwin

package app

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"sync"
	"testing"

	"easycode/internal/config"
	"easycode/internal/domain"
	"easycode/internal/protocol"
	"easycode/internal/secret"
	"easycode/internal/tool"
)

type gateArtifactStore struct {
	delegate tool.CommandOutputStore
	capture  *gateArtifactCapture
}

func (s gateArtifactStore) StartCapture(ctx context.Context, id tool.InvocationID) (tool.CommandOutputCapture, error) {
	c, err := s.delegate.StartCapture(ctx, id)
	if err != nil {
		return nil, err
	}
	s.capture.delegate = c
	return s.capture, nil
}

type gateArtifactCapture struct {
	delegate                 tool.CommandOutputCapture
	mode                     string
	once                     sync.Once
	entered, release, closed chan struct{}
}

func (c *gateArtifactCapture) block() { c.once.Do(func() { close(c.entered) }); <-c.release }
func (c *gateArtifactCapture) WriteStdout(p []byte) (int, error) {
	if c.mode == "write" {
		c.block()
	}
	return c.delegate.WriteStdout(p)
}
func (c *gateArtifactCapture) WriteStderr(p []byte) (int, error) { return c.delegate.WriteStderr(p) }
func (c *gateArtifactCapture) Finish(out, stderr, needed bool) (tool.ArtifactSaving, tool.ArtifactSaving) {
	if c.mode == "seal" {
		c.block()
	}
	return c.delegate.Finish(out, stderr, needed)
}
func (c *gateArtifactCapture) Close() error { err := c.delegate.Close(); close(c.closed); return err }

func TestArtifactShutdownRetainsRealLeaseWhileDiskBlocked(t *testing.T) {
	for _, mode := range []string{"write", "seal"} {
		t.Run(mode, func(t *testing.T) {
			root, workspace := privateAppTempDir(t), privateAppTempDir(t)
			gate := &gateArtifactCapture{mode: mode, entered: make(chan struct{}), release: make(chan struct{}), closed: make(chan struct{})}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				serveWriteScenarioSample(w, domain.ProviderOpenAI, 1, []writeScenarioCall{{"exec_command", `{"command":"printf output"}`}})
			}))
			defer server.Close()
			cfg := config.Config{Provider: config.Provider{Family: domain.ProviderOpenAI, BaseURL: server.URL, Model: "gpt-test", APIKey: secret.New("artifact-test")}}
			resources, err := openChatResourcesWithHooks(context.Background(), cfg, dataPaths{sessionRoot: root}, "", false, workspace, chatResourceHooks{interactiveApprovals: true, wrapOutputStore: func(store tool.CommandOutputStore) tool.CommandOutputStore { return gateArtifactStore{store, gate} }})
			if err != nil {
				t.Fatal(err)
			}
			host, err := startTUIControlSession(context.Background(), resources.runtime)
			if err != nil {
				t.Fatal(err)
			}
			resources.session = host
			events, err := submitInteractiveFixture(t, host, context.Background(), "run")
			if err != nil {
				t.Fatal(err)
			}
			drained := make(chan struct{})
			go func() {
				defer close(drained)
				for event := range events {
					if event.Kind == protocol.EventToolApprovalRequested {
						p, e := protocol.DecodeToolApprovalRequested(event)
						if e == nil {
							_ = host.ResolveApproval(context.Background(), event.TurnID, p, protocol.ApprovalAllowOnce, "")
						}
					}
				}
			}()
			<-gate.entered
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if err := host.Shutdown(ctx); !errors.Is(err, context.Canceled) {
				t.Fatalf("shutdown did not retain blocked owner: %v", err)
			}
			if _, err := submitInteractiveFixture(t, host, context.Background(), "must not run"); err == nil {
				t.Fatal("blocked shutdown accepted a new turn")
			}
			probe := func(expected string) {
				command := exec.Command(os.Args[0], "-test.run=^TestHostLeaseProbe$")
				command.Env = append(os.Environ(), "EASYCODE_HOST_LEASE_ROOT="+root, "EASYCODE_HOST_LEASE_THREAD="+string(resources.identity.ThreadID), "EASYCODE_HOST_LEASE_EXPECT="+expected)
				if b, e := command.CombinedOutput(); e != nil {
					t.Fatalf("lease: %v %s", e, b)
				}
			}
			probe("busy")
			select {
			case <-gate.closed:
				t.Fatal("blocked capture released reservation")
			default:
			}
			select {
			case <-host.Done():
				t.Fatal("blocked capture released host")
			default:
			}
			close(gate.release)
			if err := resources.close(context.Background()); err != nil {
				t.Fatal(err)
			}
			<-drained
			select {
			case <-gate.closed:
			default:
				t.Fatal("cleanup omitted capture")
			}
			probe("free")
		})
	}
}
