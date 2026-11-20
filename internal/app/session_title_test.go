package app

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"easycode/internal/config"
	"easycode/internal/domain"
	"easycode/internal/headless"
	"easycode/internal/protocol"
	chatRuntime "easycode/internal/runtime"
	"easycode/internal/secret"
	"easycode/internal/session"
	"easycode/internal/session/catalog"
)

func TestSessionTitlePreservesNativeRequestsAndCache(t *testing.T) {
	for _, key := range []string{"EASYCODE_PROVIDER", "EASYCODE_BASE_URL", "EASYCODE_API_KEY", "EASYCODE_MODEL"} {
		t.Setenv(key, "")
	}
	const title = domain.SessionTitle("Private title @workspace 👩‍💻")
	const literal = "/rename this is literal headless input"
	for _, family := range []domain.ProviderFamily{domain.ProviderOpenAI, domain.ProviderAnthropic} {
		t.Run(string(family), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			var mu sync.Mutex
			var requests [][]byte
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
					return
				}
				mu.Lock()
				requests = append(requests, body)
				mu.Unlock()
				w.Header().Set("Content-Type", "text/event-stream")
				serveWriteScenarioSample(w, family, 91, nil)
			}))
			defer server.Close()
			model := "gpt-test"
			if family == domain.ProviderAnthropic {
				model = "claude-test"
			}
			cfg := cacheFixtureConfig(t, config.Config{Provider: config.Provider{Family: family, Model: model, BaseURL: server.URL, APIKey: secret.New("fixture-key")}}, true)
			data := readTitleFixture(t, "task_"+string(family)+"_checkpoints.jsonl")
			workspace := privateAppTempDir(t)
			var baseline protocol.CacheSnapshotPayload
			for _, branch := range []string{"unnamed", "live", "restored"} {
				root, path, thread := installPreviewFixture(t, data)
				open := func() *chatResources {
					resources, err := openChatResources(ctx, cfg, root, string(thread), workspace)
					if err != nil {
						t.Fatal(err)
					}
					t.Cleanup(func() { _ = resources.close(context.Background()) })
					if err := resources.session.StartInteractive(ctx); err != nil {
						t.Fatal(err)
					}
					return resources
				}
				resources := open()
				if branch != "unnamed" {
					confirmTitle(t, ctx, resources.session, "rename", title)
					if branch == "restored" {
						if err := resources.close(context.Background()); err != nil {
							t.Fatal(err)
						}
						resources = open()
						if resources.title != title || resources.identity.ThreadID != thread {
							t.Fatal("online resume did not initialize the verified title")
						}
					}
					confirmTitle(t, ctx, resources.session, "same-title", title)
					written, err := os.ReadFile(path)
					if err != nil || !bytes.HasPrefix(written, data) || bytes.Count(written[len(data):], []byte{'\n'}) != 1 {
						t.Fatal("rename changed history or the same title appended a second fact", err)
					}
				}
				events, err := submitInteractiveFixture(t, resources.session, ctx, "continue after checkpoints")
				if err != nil {
					t.Fatal(err)
				}
				var completed bool
				for event := range events {
					if bytes.Contains(event.Payload, []byte(title)) {
						t.Fatal("title leaked into a turn event")
					}
					completed = event.Kind == protocol.EventTurnCompleted
				}
				if !completed {
					t.Fatal("continuation failed")
				}
				snapshot := completionCacheSnapshot(t, ctx, resources)
				if snapshot.LastSample == nil {
					t.Fatal("continuation did not publish cache evidence")
				}
				if branch == "unnamed" {
					baseline = snapshot
				} else if snapshot.StableFingerprint != baseline.StableFingerprint || snapshot.ContentFingerprint != baseline.ContentFingerprint || !reflect.DeepEqual(snapshot.Sources, baseline.Sources) || snapshot.LastSample.NativeFingerprint != baseline.LastSample.NativeFingerprint || snapshot.LastSample.RequestFingerprint != baseline.LastSample.RequestFingerprint {
					t.Fatal("title changed cache or native fingerprints")
				}
				if err := resources.close(context.Background()); err != nil {
					t.Fatal(err)
				}
				if branch == "restored" {
					var output bytes.Buffer
					cfgPath := writeAppConfig(t, privateAppTempDir(t), family, server.URL)
					result := Run(ctx, Options{Mode: headless.ModeJSON, Prompt: literal, ConfigPath: cfgPath, ResumeThreadID: string(thread), SessionDataRoot: root, Output: &output})
					if result.ExitCode() != 0 || bytes.Contains(output.Bytes(), []byte(title)) || bytes.Contains(output.Bytes(), []byte("rename_session")) {
						t.Fatalf("headless title isolation failed: %v", result.Class)
					}
				}
			}
			mu.Lock()
			defer mu.Unlock()
			if len(requests) != 4 || !bytes.Equal(requests[0], requests[1]) || !bytes.Equal(requests[0], requests[2]) {
				t.Fatalf("complete native requests differ after rename: captured %d", len(requests))
			}
			if !bytes.Contains(requests[3], []byte(literal)) {
				t.Fatal("headless slash text did not reach the provider")
			}
			for _, body := range requests {
				if bytes.Contains(body, []byte(title)) {
					t.Fatal("title reached the provider")
				}
			}
		})
	}
}

func TestSessionTitleRestoreRejectsBeforeRepair(t *testing.T) {
	base := readTitleFixture(t, "root.jsonl")
	updates := readTitleFixture(t, "session_title_updates.jsonl")
	invalid := bytes.SplitAfter(readTitleFixture(t, "session_title_invalid.jsonl"), []byte{'\n'})
	misplaced := bytes.SplitAfter(readTitleFixture(t, "session_title_misplaced.jsonl"), []byte{'\n'})
	active := prefixLines(base, 3)
	compacting := prefixLines(readTitleFixture(t, "compaction_completed_openai.jsonl"), 9)
	for _, fixture := range []struct {
		name string
		base []byte
		tail []byte
		good bool
	}{
		{"illegal-batch", append(bytes.Clone(base), invalid[5]...), nil, false},
		{"empty-turn-id", append(bytes.Clone(base), invalid[6]...), nil, false},
		{"active-turn", append(bytes.Clone(active), misplaced[0]...), []byte(`{"seq":`), false},
		{"active-compaction", append(compacting, misplaced[1]...), []byte(`{"seq":`), false},
		{"torn-title", append(bytes.Clone(base), updates...), []byte(`{"seq":`), true},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			ctx := context.Background()
			content := append(bytes.Clone(fixture.base), fixture.tail...)
			root, path, thread := installPreviewFixture(t, content)
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				w.WriteHeader(500)
			}))
			defer server.Close()
			cfg := config.Config{Provider: config.Provider{Family: domain.ProviderOpenAI, BaseURL: server.URL, APIKey: secret.New("fixture-key"), Model: "fixture-model"}}
			preview, cleanup, err := previewSession(ctx, root, thread, defaultPreviewDependencies())
			if cleanup || (err == nil) != fixture.good || preview.Incomplete != fixture.good {
				t.Fatalf("unexpected preview status: cleanup=%t, incomplete=%t, error=%v", cleanup, preview.Incomplete, err)
			}
			rows, report, err := loadSessionList(ctx, dataPaths{sessionRoot: root, catalogPath: filepath.Join(filepath.Dir(root), "catalog.json")}, catalog.ListQuery{Scope: catalog.ListAllDirectories, Limit: 20}, defaultSessionListDependencies())
			if err != nil || (len(rows.Entries) == 1) != fixture.good || (report.Invalid == 1) == fixture.good {
				t.Fatalf("unexpected catalog result: %+v, %v", report, err)
			}
			if actual, err := os.ReadFile(path); err != nil || !bytes.Equal(actual, content) {
				t.Fatal("discovery modified journal", err)
			}
			resources, err := openChatResources(ctx, cfg, root, string(thread), privateAppTempDir(t))
			if (err == nil) != fixture.good {
				if resources != nil {
					_ = resources.close(ctx)
				}
				t.Fatalf("unexpected resume status: %v", err)
			}
			want := content
			if fixture.good {
				if resources.title == "" || resources.title != preview.Title || resources.title != rows.Entries[0].Title || !resources.repair.Repaired {
					t.Fatal("valid repair did not preserve the shared committed title")
				}
				if err := resources.close(ctx); err != nil {
					t.Fatal(err)
				}
				want = fixture.base
			}
			if actual, err := os.ReadFile(path); err != nil || !bytes.Equal(actual, want) || calls.Load() != 0 {
				t.Fatal("resume changed unexpected bytes or used the provider", err)
			}
		})
	}
}

func TestSessionTitleFollowsToolRecoveryTerminal(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	root, _, thread := installPreviewFixture(t, readTitleFixture(t, "task_openai_result_tail.jsonl"))
	cfg := config.Config{Provider: config.Provider{Family: domain.ProviderOpenAI, BaseURL: "https://example.invalid", APIKey: secret.New("fixture-key"), Model: "gpt-test"}}
	resources, err := openChatResources(ctx, cfg, root, string(thread), privateAppTempDir(t))
	if err != nil {
		t.Fatal(err)
	}
	defer resources.close(context.Background())
	if err := resources.session.StartInteractive(ctx); err != nil {
		t.Fatal(err)
	}
	confirmTitle(t, ctx, resources.session, "rename", "Recovered tool loop")
	if err := resources.close(ctx); err != nil {
		t.Fatal(err)
	}
	loaded, plan := loadToolLoopPlan(t, root, thread)
	terminal, update := loaded.Records[len(loaded.Records)-2], loaded.Records[len(loaded.Records)-1]
	if plan.Title != "Recovered tool loop" || terminal.EventKind != session.EventTurnFailed || update.EventKind != session.EventSessionTitleUpdated || update.Sequence != terminal.Sequence+1 || plan.ToolRecovery != nil {
		t.Fatal("rename bypassed durable Tool Loop recovery")
	}
}

func confirmTitle(t *testing.T, ctx context.Context, host *chatRuntime.ChatSession, id protocol.RequestID, title domain.SessionTitle) {
	t.Helper()
	result, err := host.RenameSession(ctx, id, title)
	if err != nil || result.Disposition() != protocol.CommandDispositionUpdated {
		t.Fatalf("rename was not confirmed: %v, %v", result, err)
	}
	for {
		select {
		case item, open := <-host.Outputs():
			if !open {
				t.Fatal("control output closed before title confirmation")
			}
			if receipt, ok := item.CommandResult(); ok && receipt == result {
				return
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
}

func readTitleFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("../session/testdata/current", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func prefixLines(data []byte, count int) []byte {
	return bytes.Join(bytes.SplitAfterN(data, []byte{'\n'}, count+1)[:count], nil)
}
