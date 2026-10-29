//go:build darwin

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
	"regexp"
	"sync/atomic"
	"testing"

	"easycode/internal/config"
	"easycode/internal/domain"
	"easycode/internal/protocol"
	"easycode/internal/secret"
	"easycode/internal/tool"
)

func runArtifactTestTurn(t *testing.T, resources *chatResources, prompt string) {
	t.Helper()
	control, err := startTUIControlSession(context.Background(), resources.runtime)
	if err != nil {
		t.Fatal(err)
	}
	resources.session = control
	events, err := control.Submit(context.Background(), prompt)
	if err != nil {
		t.Fatal(err)
	}
	completed := false
	for event := range events {
		switch event.Kind {
		case protocol.EventToolApprovalRequested:
			p, err := protocol.DecodeToolApprovalRequested(event)
			if err != nil {
				t.Fatal(err)
			}
			if err := control.ResolveApproval(context.Background(), event.TurnID, p, protocol.ApprovalAllowOnce, ""); err != nil {
				t.Fatal(err)
			}
		case protocol.EventTurnCompleted:
			completed = true
		case protocol.EventTurnFailed:
			t.Fatalf("artifact turn failed: %s", event.Payload)
		}
	}
	if !completed {
		t.Fatal("artifact turn did not complete")
	}
	if err := control.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestArtifactResumeKeepsNativeRequestAndReadOnlyHistory(t *testing.T) {
	for _, family := range []domain.ProviderFamily{domain.ProviderOpenAI, domain.ProviderAnthropic} {
		t.Run(string(family), func(t *testing.T) {
			ctx := context.Background()
			root, workspace := privateAppTempDir(t), privateAppTempDir(t)
			if err := os.WriteFile(filepath.Join(workspace, "README.md"), []byte("workspace read"), 0o600); err != nil {
				t.Fatal(err)
			}
			var count atomic.Int32
			requests := make(chan []byte, 10)
			pattern := regexp.MustCompile(`artifact_id: ([a-f0-9]{32})`)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				requests <- body
				n := int(count.Add(1))
				w.Header().Set("Content-Type", "text/event-stream")
				var calls []writeScenarioCall
				switch n {
				case 1:
					name := "exec_command"
					if family == domain.ProviderAnthropic {
						name = "Bash"
					}
					calls = []writeScenarioCall{{name, `{"command":"/usr/bin/awk 'BEGIN {for (i=0;i<1700000;i++) printf \"0123456789\"}'; printf warning >&2"}`}}
				case 2:
					match := pattern.FindSubmatch(body)
					if len(match) != 2 {
						t.Error("saved output reference missing")
						return
					}
					calls = []writeScenarioCall{{"ReadArtifact", fmt.Sprintf(`{"artifact_id":%q,"offset":300000,"limit":20}`, match[1])}, {"Read", `{"file_path":"README.md"}`}, {"ReadArtifact", `{"artifact_id":"ffffffffffffffffffffffffffffffff","limit":20}`}}
				}
				serveWriteScenarioSample(w, family, n, calls)
			}))
			defer server.Close()
			model := "gpt-test"
			if family == domain.ProviderAnthropic {
				model = "claude-test"
			}
			cfg := config.Config{Provider: config.Provider{Family: family, Model: model, BaseURL: server.URL, APIKey: secret.New("artifact-fixture")}}
			resources, err := openChatResourcesWithHooks(ctx, cfg, dataPaths{sessionRoot: root}, "", false, workspace, chatResourceHooks{interactiveApprovals: true})
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = resources.close(ctx) }()
			runArtifactTestTurn(t, resources, "run and inspect")
			<-requests
			<-requests
			finalRequest := <-requests
			if !bytes.Contains(finalRequest, []byte("01234567890123456789")) || !bytes.Contains(finalRequest, []byte("unauthorized")) {
				t.Fatal("ReadArtifact native outputs lost ordered results")
			}
			thread := resources.identity.ThreadID
			journalPath, _ := resources.service.repository.JournalPath(thread)
			baseline, err := os.ReadFile(journalPath)
			if err != nil {
				t.Fatal(err)
			}
			list, err := resources.commandQueries.ListCommands(ctx, protocol.CommandListQuery{ThreadID: thread, Limit: 50})
			if err != nil || len(list.Items) != 1 {
				t.Fatalf("history: %v", err)
			}
			item := list.Items[0]
			if item.Stdout.Status != "partial" || item.Stdout.SavedBytes != tool.ArtifactStreamBytes || item.Stdout.ObservedBytes != 17000000 || item.Stderr.Status != "complete" {
				t.Fatal("history lost separate storage states")
			}
			prefixEnd, err := resources.commandQueries.ReadCommandOutput(ctx, protocol.CommandOutputQuery{ThreadID: thread, InvocationID: item.InvocationID, Stream: protocol.CommandStdout, Offset: tool.ArtifactStreamBytes - 3, Limit: 65536})
			if err != nil || !prefixEnd.StoredEOF || prefixEnd.SourceComplete || len(prefixEnd.Text) != 3 {
				t.Fatal("partial saved EOF was reported as complete command output")
			}
			query := protocol.CommandOutputQuery{ThreadID: thread, InvocationID: item.InvocationID, Stream: protocol.CommandStderr, Limit: 65536}
			page, err := resources.commandQueries.ReadCommandOutput(ctx, query)
			if err != nil || page.Text != "warning" || !page.SourceComplete {
				t.Fatalf("stderr: %+v %v", page, err)
			}
			if after, _ := os.ReadFile(journalPath); !bytes.Equal(after, baseline) || count.Load() != 3 {
				t.Fatal("host query changed journal or invoked Provider")
			}
			invocationDir := filepath.Join(root, "artifacts", string(thread), item.InvocationID)
			objects, err := os.ReadDir(invocationDir)
			if err != nil {
				t.Fatal(err)
			}
			for _, object := range objects {
				info, err := object.Info()
				if err != nil {
					t.Fatal(err)
				}
				if object.Name() != "reservation" && info.Size() == 7 {
					path := filepath.Join(invocationDir, object.Name())
					if err := os.WriteFile(path, []byte("changed"), 0o600); err != nil {
						t.Fatal(err)
					}
					if _, err := resources.commandQueries.ReadCommandOutput(ctx, query); err != tool.ArtifactReadCorrupt {
						t.Fatalf("tampered output: %v", err)
					}
					if err := os.WriteFile(path, []byte("warning"), 0o600); err != nil {
						t.Fatal(err)
					}
				}
			}
			runArtifactTestTurn(t, resources, "next")
			uninterrupted := <-requests
			if err := resources.close(ctx); err != nil {
				t.Fatal(err)
			}
			orphanID, _ := tool.GenerateInvocationID()
			orphan := filepath.Join(root, "artifacts", string(thread), string(orphanID), "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.stage")
			if err := os.MkdirAll(filepath.Dir(orphan), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(orphan, []byte("orphan evidence"), 0o600); err != nil {
				t.Fatal(err)
			}
			corrupt := append([]byte(nil), baseline...)
			corrupt[0] = '!'
			if err := os.WriteFile(journalPath, corrupt, 0o600); err != nil {
				t.Fatal(err)
			}
			if rejected, err := openChatResources(ctx, cfg, root, string(thread), workspace); err == nil {
				_ = rejected.close(ctx)
				t.Fatal("corrupt journal accepted")
			}
			if b, err := os.ReadFile(orphan); err != nil || string(b) != "orphan evidence" {
				t.Fatal("corrupt journal triggered orphan cleanup")
			}
			if b, err := os.ReadFile(journalPath); err != nil || !bytes.Equal(b, corrupt) {
				t.Fatal("corrupt journal was repaired")
			}
			if err := os.WriteFile(journalPath, baseline, 0o600); err != nil {
				t.Fatal(err)
			}
			entries, err := os.ReadDir(invocationDir)
			if err != nil {
				t.Fatal(err)
			}
			for _, entry := range entries {
				if entry.Name() != "reservation" {
					if err := os.Remove(filepath.Join(invocationDir, entry.Name())); err != nil {
						t.Fatal(err)
					}
				}
			}
			resumed, err := openChatResources(ctx, cfg, root, string(thread), workspace)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = resumed.close(ctx) }()
			if _, err := os.Stat(orphan); !os.IsNotExist(err) {
				t.Fatal("validated resume did not remove orphan")
			}
			list, err = resumed.commandQueries.ListCommands(ctx, protocol.CommandListQuery{ThreadID: thread, Limit: 50})
			if err != nil || len(list.Items) != 1 || list.Items[0].Preview != item.Preview {
				t.Fatal("missing objects changed restored command history")
			}
			if _, err := resumed.commandQueries.ReadCommandOutput(ctx, query); err != tool.ArtifactReadUnavailable {
				t.Fatalf("missing object: %v", err)
			}
			if after, _ := os.ReadFile(journalPath); !bytes.Equal(after, baseline) || count.Load() != 4 {
				t.Fatal("resume/query rewrote completed journal or called Provider")
			}
			runArtifactTestTurn(t, resumed, "next")
			restored := <-requests
			if !bytes.Equal(restored, uninterrupted) {
				t.Fatal("artifact deletion changed canonical restored request")
			}
		})
	}
}
