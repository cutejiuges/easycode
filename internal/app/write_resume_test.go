package app

import (
	"context"
	"easycode/internal/config"
	"easycode/internal/domain"
	"easycode/internal/protocol"
	"easycode/internal/secret"
	"easycode/internal/session"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestWriteRecoveryPairsEveryTailWithoutReplayingFilesOrGrants(t *testing.T) {
	for _, family := range []domain.ProviderFamily{domain.ProviderOpenAI, domain.ProviderAnthropic} {
		t.Run(string(family), func(t *testing.T) {
			root := t.TempDir()
			dataRoot := filepath.Join(t.TempDir(), "sessions")
			var mu sync.Mutex
			var requests [][]byte
			phase := 0
			recovering := false
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				mu.Lock()
				requests = append(requests, body)
				sample := len(requests)
				currentPhase, currentRecovering := phase, recovering
				phase++
				mu.Unlock()
				w.Header().Set("Content-Type", "text/event-stream")
				var calls []writeScenarioCall
				if currentPhase == 0 {
					if currentRecovering {
						calls = []writeScenarioCall{{"Write", `{"file_path":"new/a","content":"must-not-replay"}`}, {"Write", `{"file_path":"other/a","content":"must-ask-again"}`}}
					} else {
						calls = []writeScenarioCall{{"Write", `{"file_path":"new/a","content":"original"}`}}
					}
				}
				serveWriteScenarioSample(w, family, sample, calls)
			}))
			defer server.Close()
			model := "gpt-test"
			if family == domain.ProviderAnthropic {
				model = "claude-test"
			}
			cfg := config.Config{Provider: config.Provider{Family: family, BaseURL: server.URL, APIKey: secret.New("resume-fixture"), Model: model}}
			resources, err := openChatResourcesWithHooks(context.Background(), cfg, dataPaths{sessionRoot: dataRoot}, "", false, root, chatResourceHooks{interactiveApprovals: true})
			if err != nil {
				t.Fatal(err)
			}
			control, err := startTUIControlSession(context.Background(), resources.runtime)
			if err != nil {
				t.Fatal(err)
			}
			events, err := control.Submit(context.Background(), "create")
			if err != nil {
				t.Fatal(err)
			}
			completed := false
			for event := range events {
				if event.Kind == protocol.EventToolApprovalRequested {
					p, err := protocol.DecodeToolApprovalRequested(event)
					if err != nil {
						t.Fatal(err)
					}
					if err := control.ResolveApproval(context.Background(), event.TurnID, p, protocol.ApprovalAllowSession, ""); err != nil {
						t.Fatal(err)
					}
				}
				if event.Kind == protocol.EventTurnCompleted {
					completed = true
				}
				if event.Kind == protocol.EventTurnFailed {
					t.Fatal("seed turn failed")
				}
			}
			if !completed {
				t.Fatal("seed turn incomplete")
			}
			if err := control.stop(context.Background()); err != nil {
				t.Fatal(err)
			}
			thread := resources.identity.ThreadID
			journalPath, err := resources.service.repository.JournalPath(thread)
			if err != nil {
				t.Fatal(err)
			}
			if err := resources.close(context.Background()); err != nil {
				t.Fatal(err)
			}
			baseline, err := os.ReadFile(journalPath)
			if err != nil {
				t.Fatal(err)
			}
			loaded := loadHeadlessJournal(t, dataRoot, thread)
			for _, record := range loaded.Records {
				marker := ""
				switch record.EventKind {
				case session.EventToolCallReady:
					marker = "session interrupted before approval"
				case session.EventToolApprovalDecided:
					marker = "session interrupted before execution"
				case session.EventToolExecutionStarted:
					marker = "execution outcome is uncertain"
				case session.EventToolCallResult:
					marker = "Created new/a"
				default:
					continue
				}
				t.Run(string(record.EventKind), func(t *testing.T) {
					writeJournalPrefix(t, journalPath, baseline, record.Sequence)
					if err := os.WriteFile(filepath.Join(root, "new/a"), []byte("external-after-crash"), 0600); err != nil {
						t.Fatal(err)
					}
					orphan := filepath.Join(root, ".easycode-write-orphan")
					if err := os.WriteFile(orphan, []byte("preserve"), 0600); err != nil {
						t.Fatal(err)
					}
					mu.Lock()
					before := len(requests)
					phase = 0
					recovering = true
					mu.Unlock()
					resumed, err := openChatResources(context.Background(), cfg, dataRoot, string(thread), root)
					if err != nil {
						t.Fatal(err)
					}
					mu.Lock()
					after := len(requests)
					mu.Unlock()
					if after != before {
						t.Fatal("resume called Provider")
					}
					content, err := os.ReadFile(filepath.Join(root, "new/a"))
					if err != nil || string(content) != "external-after-crash" {
						t.Fatal("resume replayed Write", err)
					}
					if content, err := os.ReadFile(orphan); err != nil || string(content) != "preserve" {
						t.Fatal("resume scanned/cleaned orphan", err)
					}
					submitAppTurn(t, resumed, "try writes after recovery")
					if err := resumed.close(context.Background()); err != nil {
						t.Fatal(err)
					}
					mu.Lock()
					first := string(requests[before])
					last := string(requests[len(requests)-1])
					mu.Unlock()
					if !strings.Contains(first, marker) {
						t.Fatal("missing paired recovery output", marker)
					}
					if !strings.Contains(last, "write_requires_full_read") || !strings.Contains(last, "approval_required") {
						t.Fatal("resume restored Read observation or session grant", last)
					}
					if _, err := os.Stat(filepath.Join(root, "other")); !os.IsNotExist(err) {
						t.Fatal("resume grant created new directory")
					}
				})
			}
		})
	}
}
