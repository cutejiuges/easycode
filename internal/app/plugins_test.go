package app

import (
	"bytes"
	"context"
	"easycode/internal/codec"
	"easycode/internal/config"
	"easycode/internal/domain"
	"easycode/internal/extension/plugin"
	"easycode/internal/secret"
	"easycode/internal/session"
	"easycode/internal/tool"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func appPluginFixture(t *testing.T, kind string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS(filepath.Join("..", "extension", "plugin", "testdata", kind))); err != nil {
		t.Fatal(err)
	}
	return root
}
func pluginToolFrames(family domain.ProviderFamily, sample int, name string) string {
	var frames bytes.Buffer
	fixtureSample := sample
	if sample <= 2 {
		fixtureSample = 1
	}
	if family == domain.ProviderOpenAI {
		writeOpenAIToolLoopSample(&frames, fixtureSample)
	} else {
		writeAnthropicToolLoopSample(&frames, fixtureSample)
	}
	if sample > 2 {
		return frames.String()
	}
	args := struct {
		Skill    string `json:"skill"`
		Resource string `json:"resource,omitempty"`
	}{Skill: name}
	if sample == 2 {
		args.Resource = "references/checklist.md"
	}
	argumentBytes, _ := codec.MarshalStable(args)
	quoted, _ := codec.MarshalStable(string(argumentBytes))
	old, _ := codec.MarshalStable(`{"file_path":"README.md"}`)
	return strings.NewReplacer(`"name":"Read"`, `"name":"Skill"`, string(old), string(quoted), "resp-tool", fmt.Sprintf("resp-plugin-%d", sample), "msg-tool", fmt.Sprintf("msg-plugin-%d", sample), "call-1", fmt.Sprintf("call-plugin-%d", sample), "toolu-1", fmt.Sprintf("toolu-plugin-%d", sample), "fc-1", fmt.Sprintf("fc-plugin-%d", sample)).Replace(frames.String())
}

func TestPluginSkillLifecycleBothProviders(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	// 格式加载由 loader 与 CLI smoke 验证；这里保留自动与手工专用两种调用策略。
	for _, kind := range []string{"claude", "codex"} {
		for _, family := range []domain.ProviderFamily{domain.ProviderOpenAI, domain.ProviderAnthropic} {
			t.Run(kind+"/"+string(family), func(t *testing.T) {
				root := appPluginFixture(t, kind)
				workspace := t.TempDir()
				dataRoot := filepath.Join(t.TempDir(), "sessions")
				id := kind + "qa"
				name := id + ":review"
				var requests requestRecorder
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					index, _ := requests.record(t, r)
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = io.WriteString(w, pluginToolFrames(family, index+1, name))
				}))
				defer server.Close()
				cfg := config.Config{Provider: config.Provider{Family: family, Model: "fixture-model", BaseURL: server.URL, APIKey: secret.New("plugin-fixture-key")}}
				resources := mustOpenChatResourcesWithHooks(t, context.Background(), cfg, dataPaths{sessionRoot: dataRoot}, "", false, workspace, chatResourceHooks{pluginDirs: []string{root}})
				t.Cleanup(func() { _ = resources.close(context.Background()) })
				if len(resources.plugins) != 1 || resources.plugins[0].Available != 1 || resources.plugins[0].Status != domain.PluginAvailable {
					t.Fatal("stale plugin view", resources.plugins)
				}
				input := `{"version":1,"type":"input.submit","request_id":"plugin","text":"$` + name + ` exact args"}` + "\n"
				output := &appWriteCloser{}
				outcome := runStreamJSON(context.Background(), Options{Input: io.NopCloser(strings.NewReader(input)), Output: output}, resources)
				if outcome.Class != ExitSuccess || strings.Contains(output.String(), "plugin.install") {
					t.Fatalf("stream: %#v %s", outcome, output.String())
				}
				thread := resources.identity.ThreadID
				journal, _ := resources.service.repository.JournalPath(thread)
				closeAppTestResources(t, resources)
				captured := requests.snapshot()
				if len(captured) != 3 || !bytes.Contains(captured[0], []byte("Review exact args")) || !bytes.Contains(captured[2], []byte("Check correctness")) {
					t.Fatalf("missing actual plugin content: %d requests", len(captured))
				}
				if (kind == "codex") != bytes.Contains(captured[1], []byte("skill_model_invocation_disabled")) {
					t.Fatal("model bypassed effective sidecar policy")
				}
				for _, request := range captured {
					if bytes.Contains(request, []byte(root)) || bytes.Contains(request, []byte("plugin-fixture-key")) {
						t.Fatal("private source leaked")
					}
				}
				loaded, _ := loadToolLoopPlan(t, dataRoot, thread)
				baseline := readAppTestFile(t, journal)
				results := 0
				for _, record := range loaded.Records {
					if record.EventKind == session.EventToolCallResult {
						p, err := session.DecodeToolCallResultPayload(record)
						if err != nil {
							t.Fatal(err)
						}
						if p.Capability != tool.CapabilitySkill {
							t.Fatal("new plugin tool")
						}
						results++
					}
				}
				if results != 2 {
					t.Fatal("missing frozen tool results")
				}
				if err := os.RemoveAll(root); err != nil {
					t.Fatal(err)
				}
				reads := 0
				hooks := chatResourceHooks{pluginOptions: plugin.Options{BeforeOpen: func(string) { reads++ }}}
				restored := mustOpenChatResourcesWithHooks(t, context.Background(), cfg, dataPaths{sessionRoot: dataRoot}, string(thread), false, workspace, hooks)
				m := restored.history.Turns[0].Messages[0]
				if !m.HasSkill || m.Skill.Name != name || !strings.Contains(m.Skill.Content, "exact args") || len(restored.plugins) != 0 || reads != 0 {
					t.Fatal("recovery reread or reenabled plugin")
				}
				closeAppTestResources(t, restored)
				if kind != "codex" {
					return
				}
				for _, checkpoint := range loaded.Records {
					if checkpoint.EventKind != session.EventToolCallReady && checkpoint.EventKind != session.EventToolExecutionStarted && checkpoint.EventKind != session.EventToolCallResult && checkpoint.EventKind != session.EventProviderNativeCommit {
						continue
					}
					if checkpoint.BatchIndex+1 != checkpoint.BatchSize || checkpoint.Sequence < 6 {
						continue
					}
					writeJournalPrefix(t, journal, baseline, checkpoint.Sequence)
					var inspected bool
					beforeOpen := func(string) {
						if inspected {
							return
						}
						inspected = true
						data, err := os.ReadFile(journal)
						if err != nil {
							t.Error(err)
							return
						}
						if !bytes.Contains(data, []byte(`"event_kind":"turn_failed"`)) && !bytes.Contains(data, []byte(`"event_kind":"turn_completed"`)) {
							t.Error("source read before old turn reconciliation")
						}
					}
					resumed := mustOpenChatResourcesWithHooks(t, context.Background(), cfg, dataPaths{sessionRoot: dataRoot}, string(thread), false, workspace, chatResourceHooks{pluginDirs: []string{root}, pluginOptions: plugin.Options{BeforeOpen: beforeOpen}})
					if !inspected || len(resumed.plugins) != 1 || resumed.plugins[0].Status != domain.PluginUnavailable {
						t.Fatal("missing future source isolation")
					}
					closeAppTestResources(t, resumed)
					if len(requests.snapshot()) != 3 {
						t.Fatal("recovery contacted provider")
					}
				}
			})
		}
	}
}

func TestSourceCloseWaitsForPreadmissionPluginPreparation(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := appPluginFixture(t, "claude")
	workspace := t.TempDir()
	var blocked atomic.Bool
	entered, resume := make(chan struct{}), make(chan struct{})
	var once sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("cancelled input contacted provider") }))
	defer server.Close()
	resources := mustOpenChatResourcesWithHooks(t, context.Background(), config.Config{Provider: config.Provider{Family: domain.ProviderOpenAI, Model: "fixture-model", BaseURL: server.URL, APIKey: secret.New("fixture")}}, dataPaths{sessionRoot: filepath.Join(t.TempDir(), "sessions")}, "", false, workspace, chatResourceHooks{pluginDirs: []string{root}, pluginOptions: plugin.Options{AfterRead: func() {
		if blocked.Load() {
			once.Do(func() { close(entered) })
			<-resume
		}
	}}})
	path, _ := resources.service.repository.JournalPath(resources.identity.ThreadID)
	before, _ := os.ReadFile(path)
	blocked.Store(true)
	prepared := make(chan error, 1)
	go func() {
		_, err := appendInteractiveText(context.Background(), resources.session, "$claudeqa:review diff")
		prepared <- err
	}()
	<-entered
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := resources.pluginSources.Close(ctx); err != context.Canceled {
		t.Fatal(err)
	}
	select {
	case <-resources.pluginSources.Done():
		t.Fatal("preparation ownership lost")
	default:
	}
	if err := resources.artifactLayout.CheckSource(context.Background(), nil); err == nil {
		t.Fatal("invalid source accepted")
	}
	close(resume)
	if err := <-prepared; err == nil {
		t.Fatal("cancelled preparation admitted")
	}
	if err := resources.pluginSources.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := resources.artifactLayout.Check(context.Background()); err != nil {
		t.Fatal("storage boundary released before sources", err)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("preadmission cancellation wrote input")
	}
	closeAppTestResources(t, resources)
}
