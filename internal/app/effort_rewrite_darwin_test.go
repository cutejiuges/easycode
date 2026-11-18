//go:build darwin

package app

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"easycode/internal/config"
	"easycode/internal/domain"
	"easycode/internal/protocol"
	"easycode/internal/secret"
)

// TestRuntimeEffortInputRewriteKeepsCapturedPolicy 验证真实改写、冻结设置和原生恢复可以同时成立。
func TestRuntimeEffortInputRewriteKeepsCapturedPolicy(t *testing.T) {
	for _, family := range []domain.ProviderFamily{domain.ProviderOpenAI, domain.ProviderAnthropic} {
		t.Run(string(family), func(t *testing.T) {
			workspace := t.TempDir()
			selected, err := domain.NewHookWriteInput("rewritten.txt", "rewritten\n")
			if err != nil {
				t.Fatal(err)
			}
			writeInputRewriteConfig(t, workspace, selected, false)
			bodies := make(chan []byte, 8)
			var count atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				bodies <- body
				n := int(count.Add(1))
				var calls []writeScenarioCall
				if n == 1 {
					calls = []writeScenarioCall{{"Write", `{"file_path":"original.txt","content":"original\n"}`}}
				}
				w.Header().Set("Content-Type", "text/event-stream")
				serveWriteScenarioSample(w, family, n, calls)
			}))
			defer server.Close()
			cfg := reasoningFixtureConfig(t, config.Config{Provider: config.Provider{Family: family, BaseURL: server.URL, Model: "fixture-model", APIKey: secret.New("effort-rewrite-fixture")}})
			paths := dataPaths{sessionRoot: filepath.Join(t.TempDir(), "sessions")}
			hooks := chatResourceHooks{interactiveApprovals: true}
			r, err := openChatResourcesWithHooks(context.Background(), cfg, paths, "", false, workspace, hooks)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = r.close(context.Background()) }()
			if err := r.session.StartInteractive(context.Background()); err != nil {
				t.Fatal(err)
			}
			if _, err := appendInteractiveText(context.Background(), r.session, "apply one rewritten Write"); err != nil {
				t.Fatal(err)
			}
			trusts, approvals := 0, 0
			completed := false
			for item := range r.session.Outputs() {
				event, ok := item.TurnEvent()
				if !ok {
					continue
				}
				switch event.Kind {
				case protocol.EventHookTrustRequested:
					trusts++
					appEffortSet(t, r, "rewrite-next-low", appEffortChoice(t, "low"))
					p := appEffortSnapshot(t, r, "rewrite-policy")
					if p.Active == nil || p.Active.Effort().Value != "high" || p.Selected.Value != "low" || !p.Pending {
						t.Fatal("rewrite trust changed captured policy", p)
					}
					request, err := protocol.DecodeHookTrustRequested(event)
					if err != nil {
						t.Fatal(err)
					}
					if err := r.session.ResolveHookTrust(context.Background(), event.TurnID, request, protocol.HookTrustSession); err != nil {
						t.Fatal(err)
					}
				case protocol.EventToolApprovalRequested:
					approvals++
					request, err := protocol.DecodeToolApprovalRequested(event)
					if err != nil || request.RelativePath != "rewritten.txt" || request.InputRewrite.Kind() != protocol.PreInputSelected {
						t.Fatal("approval did not use selected tool input", request, err)
					}
					if err := r.session.ResolveApproval(context.Background(), event.TurnID, request, protocol.ApprovalAllowOnce, ""); err != nil {
						t.Fatal(err)
					}
				case protocol.EventTurnFailed:
					t.Fatalf("rewritten turn failed: %s", event.Payload)
				case protocol.EventTurnCompleted:
					completed = true
				}
				if completed {
					break
				}
			}
			if !completed || trusts != 1 || approvals != 1 || count.Load() != 2 {
				t.Fatal("rewrite repeated admission or sampling", completed, trusts, approvals, count.Load())
			}
			assertAppEffort(t, family, <-bodies, "high")
			continuation := <-bodies
			assertAppEffort(t, family, continuation, "high")
			if !bytes.Contains(continuation, []byte("original.txt")) || !bytes.Contains(continuation, []byte("rewritten.txt")) {
				t.Fatal("continuation lost original native call or effective result")
			}
			content, err := os.ReadFile(filepath.Join(workspace, "rewritten.txt"))
			if err != nil || string(content) != "rewritten\n" {
				t.Fatal("selected input was not executed", err)
			}
			if _, err := os.Stat(filepath.Join(workspace, "original.txt")); !os.IsNotExist(err) {
				t.Fatal("original input produced an effect", err)
			}
			appEffortIdle(t, r)
			appEffortTurn(t, r, "next operation", false)
			assertAppEffort(t, family, <-bodies, "low")
			appEffortIdle(t, r)
			thread := r.identity.ThreadID
			if err := r.close(context.Background()); err != nil {
				t.Fatal(err)
			}
			r, err = openChatResourcesWithHooks(context.Background(), cfg, paths, string(thread), false, workspace, hooks)
			if err != nil {
				t.Fatal(err)
			}
			if count.Load() != 3 {
				t.Fatal("restore performed network requests")
			}
			if err := r.session.StartInteractive(context.Background()); err != nil {
				t.Fatal(err)
			}
			p := appEffortSnapshot(t, r, "rewrite-restored-policy")
			if p.Selected.Value != "high" || p.SelectionRevision != 0 || p.Source != "startup" {
				t.Fatal("restore persisted the process selection", p)
			}
			appEffortSet(t, r, "rewrite-resume-low", appEffortChoice(t, "low"))
			appEffortTurn(t, r, "after native restore", false)
			restored := <-bodies
			assertAppEffort(t, family, restored, "low")
			if !bytes.Contains(restored, []byte("original.txt")) || !bytes.Contains(restored, []byte("rewritten.txt")) || count.Load() != 4 {
				t.Fatal("native restore lost original call or repeated work")
			}
		})
	}
}
