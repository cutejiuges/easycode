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
	"reflect"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"

	"easycode/internal/config"
	"easycode/internal/domain"
	"easycode/internal/protocol"
	"easycode/internal/secret"
	"easycode/internal/tool"
)

func TestSevenToolTranscriptBudgetAndRestoredRequestForBothProviders(t *testing.T) {
	for _, family := range []domain.ProviderFamily{domain.ProviderOpenAI, domain.ProviderAnthropic} {
		t.Run(string(family), func(t *testing.T) {
			ctx := context.Background()
			root, workspace := privateAppTempDir(t), privateAppTempDir(t)
			longLine := "needle " + strings.Repeat("中文", 400) + "\n"
			for name, text := range map[string]string{"seed.txt": "old seed\n", "main.go": "old\n", "search.txt": strings.Repeat(longLine, 200)} {
				if err := os.WriteFile(filepath.Join(workspace, name), []byte(text), 0600); err != nil {
					t.Fatal(err)
				}
			}
			requests := make(chan []byte, 12)
			var count atomic.Int32
			refs := regexp.MustCompile(`artifact_id: ([a-f0-9]{32})`)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
					return
				}
				requests <- body
				n := int(count.Add(1))
				w.Header().Set("Content-Type", "text/event-stream")
				var calls []writeScenarioCall
				switch n {
				case 1:
					calls = []writeScenarioCall{{"Read", `{"file_path":"seed.txt"}`}, {"Glob", `{"pattern":"*.txt"}`}}
					for range 8 {
						calls = append(calls, writeScenarioCall{"Grep", `{"pattern":"needle","glob":"search.txt","limit":200}`})
					}
				case 2:
					if family == domain.ProviderOpenAI {
						writeOpenAIPatchSample(w, 1)
					} else {
						writeAnthropicPatchSample(w, 1)
					}
					return
				case 3:
					matches := refs.FindSubmatch(body)
					if len(matches) != 2 {
						t.Error("search artifact missing")
						return
					}
					shell := "exec_command"
					if family == domain.ProviderAnthropic {
						shell = "Bash"
					}
					calls = []writeScenarioCall{{"Write", `{"file_path":"seed.txt","content":"new seed\n"}`}, {shell, `{"command":"printf checked; exit 7"}`}, {"ReadArtifact", fmt.Sprintf(`{"artifact_id":%q,"offset":0,"limit":100}`, matches[1])}}
				case 4:
					calls = []writeScenarioCall{{"Write", `{"file_path":"denied.txt","content":"must not exist"}`}}
				}
				serveWriteScenarioSample(w, family, n, calls)
			}))
			defer server.Close()
			model := "gpt-test"
			if family == domain.ProviderAnthropic {
				model = "claude-test"
			}
			cfg := config.Config{Provider: config.Provider{Family: family, Model: model, BaseURL: server.URL, APIKey: secret.New("fixture-private-key")}}
			resources, err := openChatResourcesWithHooks(ctx, cfg, dataPaths{sessionRoot: root}, "", false, workspace, chatResourceHooks{interactiveApprovals: true})
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = resources.close(ctx) }()
			control, err := startTUIControlSession(ctx, resources.runtime)
			if err != nil {
				t.Fatal(err)
			}
			resources.session = control
			events, err := submitInteractiveFixture(t, control, ctx, "inspect, search, edit, verify")
			if err != nil {
				t.Fatal(err)
			}
			complete, denied, exitSeven := false, false, false
			for event := range events {
				switch event.Kind {
				case protocol.EventToolApprovalRequested:
					p, err := protocol.DecodeToolApprovalRequested(event)
					if err != nil {
						t.Fatal(err)
					}
					decision := protocol.ApprovalAllowOnce
					if p.RelativePath == "denied.txt" {
						decision = protocol.ApprovalDeny
						denied = true
					}
					if err := control.ResolveApproval(ctx, event.TurnID, p, decision, ""); err != nil {
						t.Fatal(err)
					}
				case protocol.EventToolTerminal:
					p, err := protocol.DecodeToolTerminal(event)
					if err != nil {
						t.Fatal(err)
					}
					if p.Tool == protocol.ToolShell {
						exitSeven = p.ExitKnown && p.ExitCode == 7 && p.Status == "success"
					}
				case protocol.EventTurnCompleted:
					complete = true
				case protocol.EventTurnFailed:
					t.Fatalf("mixed workload failed: %s", event.Payload)
				}
			}
			if err := control.Shutdown(ctx); err != nil {
				t.Fatal(err)
			}
			if !complete || !denied || !exitSeven {
				t.Fatal("mixed workload lost terminal outcomes")
			}
			if content, _ := os.ReadFile(filepath.Join(workspace, "seed.txt")); string(content) != "new seed\n" {
				t.Fatal("durable Read did not authorize overwrite")
			}
			if content, _ := os.ReadFile(filepath.Join(workspace, "main.go")); string(content) != "new\n" {
				t.Fatal("Patch did not apply")
			}
			if _, err := os.Stat(filepath.Join(workspace, "denied.txt")); !os.IsNotExist(err) {
				t.Fatal("denied Write executed")
			}
			thread := resources.identity.ThreadID
			path, _ := resources.service.repository.JournalPath(thread)
			baseline, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			page, err := resources.transcriptQueries.ListTranscript(ctx, protocol.TranscriptListQuery{ThreadID: thread, Limit: 50})
			if err != nil {
				t.Fatal(err)
			}
			capabilities := make(map[protocol.ToolName]bool)
			pages, savedBytes, previewBytes := 0, int64(0), 0
			var searchItem protocol.TranscriptListItem
			for _, item := range page.Items {
				if item.Kind != domain.TranscriptCall {
					continue
				}
				capabilities[item.Activity.Tool] = true
				body, err := resources.transcriptQueries.ReadTranscriptBody(ctx, protocol.TranscriptBodyQuery{ThreadID: thread, Cursor: item.Cursor, Source: protocol.TranscriptResult, Limit: 65536})
				if err != nil {
					t.Fatal(err)
				}
				previewBytes += int(body.TotalBytes)
				if item.Activity.Tool == protocol.ToolGrep {
					searchItem = item
					if !item.Search.Readable || !item.Search.Complete || !item.ModelShortened || item.CoverageIncomplete {
						t.Fatal("search omission dimensions collapsed")
					}
					offset := uint64(0)
					var joined strings.Builder
					for {
						output, err := resources.transcriptQueries.ReadToolOutput(ctx, protocol.ToolOutputQuery{ThreadID: thread, InvocationID: item.Activity.InvocationID, Stream: protocol.ToolSearchResult, Offset: offset, Limit: 65536})
						if err != nil {
							t.Fatal(err)
						}
						pages++
						joined.WriteString(output.Text)
						offset = output.NextOffset
						if output.StoredEOF {
							break
						}
					}
					if strings.Count(joined.String(), "needle ") != 200 || !strings.Contains(joined.String(), strings.TrimSuffix(longLine, "\n")) {
						t.Fatal("search capture lost original lines")
					}
					savedBytes += item.Search.SavedBytes
				}
			}
			if len(capabilities) != 7 || previewBytes > tool.TurnPreviewBytes || pages <= 8 {
				t.Fatal("seven-tool bounded workload incomplete")
			}
			if after, _ := os.ReadFile(path); !bytes.Equal(after, baseline) || count.Load() != 5 {
				t.Fatal("browsing changed ledger or sampled provider")
			}
			requestBytes := 0
			for range 5 {
				requestBytes += len(<-requests)
			}
			t.Logf("preview_bytes=%d request_bytes=%d saved_bytes=%d saved_pages=%d additional_model_reads=1", previewBytes, requestBytes, savedBytes, pages)
			runArtifactTestTurn(t, resources, "continue")
			uninterrupted := <-requests
			if err := resources.close(ctx); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, baseline, 0600); err != nil {
				t.Fatal(err)
			}
			directory := filepath.Join(root, "artifacts", string(thread), searchItem.Activity.InvocationID)
			files, err := os.ReadDir(directory)
			if err != nil {
				t.Fatal(err)
			}
			for _, file := range files {
				if file.Name() != "reservation" {
					if err := os.Remove(filepath.Join(directory, file.Name())); err != nil {
						t.Fatal(err)
					}
				}
			}
			resumed, err := openChatResources(ctx, cfg, root, string(thread), workspace)
			if err != nil {
				t.Fatal(err)
			}
			defer resumed.close(ctx)
			restoredPage, err := resumed.transcriptQueries.ListTranscript(ctx, protocol.TranscriptListQuery{ThreadID: thread, Limit: 50})
			if err != nil || !reflect.DeepEqual(page, restoredPage) {
				t.Fatal("restored transcript changed")
			}
			if _, err := resumed.transcriptQueries.ReadToolOutput(ctx, protocol.ToolOutputQuery{ThreadID: thread, InvocationID: searchItem.Activity.InvocationID, Stream: protocol.ToolSearchResult, Limit: 64}); err != tool.ArtifactReadUnavailable {
				t.Fatalf("missing saved search: %v", err)
			}
			runArtifactTestTurn(t, resumed, "continue")
			restored := <-requests
			if !bytes.Equal(uninterrupted, restored) {
				t.Fatal("restored request bytes, native order or cache prefix changed")
			}
		})
	}
}
