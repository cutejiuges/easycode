package app

import (
	"bytes"
	"context"
	"easycode/internal/codec"
	"easycode/internal/config"
	"easycode/internal/domain"
	"easycode/internal/extension/plugin"
	"easycode/internal/fault"
	"easycode/internal/headless"
	"easycode/internal/protocol"
	"easycode/internal/secret"
	"easycode/internal/session"
	"easycode/internal/tool"
	"easycode/internal/tui"
	"errors"
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

func TestPluginSkillLifecycleBothProvidersAndHosts(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	for _, kind := range []string{"easycode", "claude", "codex", "agentplugins"} {
		for _, family := range []domain.ProviderFamily{domain.ProviderOpenAI, domain.ProviderAnthropic} {
			for _, mode := range []string{"once", "interactive", "stream"} {
				t.Run(kind+"/"+string(family)+"/"+mode, func(t *testing.T) {
					root := appPluginFixture(t, kind)
					workspace := t.TempDir()
					dataRoot := filepath.Join(t.TempDir(), "sessions")
					id := map[string]string{"easycode": "easyqa", "claude": "claudeqa", "codex": "codexqa", "agentplugins": "agentqa"}[kind]
					name := id + ":review"
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
						sample := len(requests)
						mu.Unlock()
						w.Header().Set("Content-Type", "text/event-stream")
						_, _ = io.WriteString(w, pluginToolFrames(family, sample, name))
					}))
					defer server.Close()
					cfg := config.Config{Provider: config.Provider{Family: family, Model: "fixture-model", BaseURL: server.URL, APIKey: secret.New("plugin-fixture-key")}}
					resources, err := openChatResourcesWithHooks(context.Background(), cfg, dataPaths{sessionRoot: dataRoot}, "", false, workspace, chatResourceHooks{pluginDirs: []string{root}})
					if err != nil {
						t.Fatal(err)
					}
					t.Cleanup(func() { _ = resources.close(context.Background()) })
					if len(resources.plugins) != 1 || resources.plugins[0].Available != 1 || resources.plugins[0].Status != domain.PluginAvailable {
						t.Fatal("stale plugin view", resources.plugins)
					}
					switch mode {
					case "once":
						submitAppTurn(t, resources, "$"+name+" exact args")
					case "interactive":
						if err := resources.session.StartInteractive(context.Background()); err != nil {
							t.Fatal(err)
						}
						events, err := submitInteractiveFixture(t, resources.session, context.Background(), "$"+name+" exact args")
						if err != nil {
							t.Fatal(err)
						}
						completed := false
						for event := range events {
							if event.Kind == protocol.EventTurnFailed {
								t.Fatal(string(event.Payload))
							}
							completed = completed || event.Kind == protocol.EventTurnCompleted
						}
						if !completed {
							t.Fatal("missing completion")
						}
					case "stream":
						input := `{"version":1,"type":"input.submit","request_id":"plugin","text":"$` + name + ` exact args"}` + "\n"
						output := &appWriteCloser{}
						outcome := runStreamJSON(context.Background(), Options{Input: io.NopCloser(strings.NewReader(input)), Output: output}, resources)
						if outcome.Class != ExitSuccess || strings.Contains(output.String(), "plugin.install") {
							t.Fatalf("stream: %#v %s", outcome, output.String())
						}
					}
					thread := resources.identity.ThreadID
					journal, _ := resources.service.repository.JournalPath(thread)
					if err := resources.close(context.Background()); err != nil {
						t.Fatal(err)
					}
					mu.Lock()
					captured := append([][]byte(nil), requests...)
					mu.Unlock()
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
					baseline, err := os.ReadFile(journal)
					if err != nil {
						t.Fatal(err)
					}
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
					restored, err := openChatResourcesWithHooks(context.Background(), cfg, dataPaths{sessionRoot: dataRoot}, string(thread), false, workspace, hooks)
					if err != nil {
						t.Fatal(err)
					}
					m := restored.history.Turns[0].Messages[0]
					if !m.HasSkill || m.Skill.Name != name || !strings.Contains(m.Skill.Content, "exact args") || len(restored.plugins) != 0 || reads != 0 {
						t.Fatal("recovery reread or reenabled plugin")
					}
					if err := restored.close(context.Background()); err != nil {
						t.Fatal(err)
					}
					if kind != "codex" || mode != "once" {
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
						resumed, err := openChatResourcesWithHooks(context.Background(), cfg, dataPaths{sessionRoot: dataRoot}, string(thread), false, workspace, chatResourceHooks{pluginDirs: []string{root}, pluginOptions: plugin.Options{BeforeOpen: beforeOpen}})
						if err != nil {
							t.Fatal(err)
						}
						if !inspected || len(resumed.plugins) != 1 || resumed.plugins[0].Status != domain.PluginUnavailable {
							t.Fatal("missing future source isolation")
						}
						if err := resumed.close(context.Background()); err != nil {
							t.Fatal(err)
						}
						mu.Lock()
						count := len(requests)
						mu.Unlock()
						if count != 3 {
							t.Fatal("recovery contacted provider")
						}
					}
				})
			}
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
	resources, err := openChatResourcesWithHooks(context.Background(), config.Config{Provider: config.Provider{Family: domain.ProviderOpenAI, Model: "fixture-model", BaseURL: server.URL, APIKey: secret.New("fixture")}}, dataPaths{sessionRoot: filepath.Join(t.TempDir(), "sessions")}, "", false, workspace, chatResourceHooks{pluginDirs: []string{root}, pluginOptions: plugin.Options{AfterRead: func() {
		if blocked.Load() {
			once.Do(func() { close(entered) })
			<-resume
		}
	}}})
	if err != nil {
		t.Fatal(err)
	}
	path, _ := resources.service.repository.JournalPath(resources.identity.ThreadID)
	before, _ := os.ReadFile(path)
	blocked.Store(true)
	prepared := make(chan error, 1)
	go func() {
		_, err := resources.session.Append(context.Background(), "$claudeqa:review diff")
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
	if err := resources.close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestForgedFixedPluginHistoryFailsBeforeSourcesOrRepair(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	for _, family := range []domain.ProviderFamily{domain.ProviderOpenAI, domain.ProviderAnthropic} {
		data, err := os.ReadFile("../session/testdata/current/plugin_input_" + string(family) + "_forged.jsonl")
		if err != nil {
			t.Fatal(err)
		}
		root, path, thread := installPreviewFixture(t, data)
		reads := 0
		cfg := config.Config{Provider: config.Provider{Family: family, Model: "fixture-model", BaseURL: "https://example.invalid", APIKey: secret.New("fixture")}}
		resources, err := openChatResourcesWithHooks(context.Background(), cfg, dataPaths{sessionRoot: root}, string(thread), false, t.TempDir(), chatResourceHooks{pluginDirs: []string{appPluginFixture(t, "claude")}, pluginOptions: plugin.Options{BeforeOpen: func(string) { reads++ }}})
		if resources != nil || !errors.Is(err, &fault.Error{Code: fault.CodeSessionCorruption}) || reads != 0 {
			t.Fatalf("forged source: %v reads=%d", err, reads)
		}
		after, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(after, data) {
			t.Fatal("failed restore changed fixed journal", err)
		}
	}
}

func TestPickerKeepsExplicitPluginPathsBoundToLaunchDirectory(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	data, err := os.ReadFile("../session/testdata/current/input_only_openai.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	root, _, thread := installPreviewFixture(t, data)
	launch := t.TempDir()
	pack := filepath.Join(launch, "team-package")
	if err := os.Mkdir(pack, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.CopyFS(pack, os.DirFS(filepath.Join("..", "extension", "plugin", "testdata", "claude"))); err != nil {
		t.Fatal(err)
	}
	dirs := []string{"./team-package"}
	cfg := config.Config{Provider: config.Provider{Family: domain.ProviderOpenAI, Model: "fixture-model", BaseURL: "https://example.invalid", APIKey: secret.New("fixture")}}
	execute := newPickerExecutor(cfg, dataPaths{sessionRoot: root}, launch, dirs)
	dirs[0] = "./changed-by-caller"
	preview := execute(context.Background(), tui.PickerRequest{ID: 1, Operation: tui.PickerPreview, ThreadID: thread})
	if preview.resources != nil || preview.result.Error != "" {
		t.Fatal("preview loaded a chat", preview.result.Error)
	}
	value := execute(context.Background(), tui.PickerRequest{ID: 2, Operation: tui.PickerResume, ThreadID: thread})
	if value.resources == nil {
		t.Fatal("picker failed", value.result.Error)
	}
	defer closePickerResources(value.resources)
	if len(value.resources.plugins) != 1 || value.resources.plugins[0].ID != "claudeqa" || value.resources.plugins[0].Available != 1 {
		t.Fatal("relative path reinterpreted or options aliased", value.resources.plugins)
	}
}

func TestDirectApplicationPluginArgumentsFailBeforeConfigurationOrStorage(t *testing.T) {
	for _, dirs := range [][]string{{""}, {string([]byte{0xff})}, make([]string, 33)} {
		root := filepath.Join(t.TempDir(), "must-not-create")
		options := Options{Mode: headless.ModeText, Prompt: "hello", PluginDirs: dirs, ConfigPath: filepath.Join(t.TempDir(), "must-not-read"), SessionDataRoot: root, Input: strings.NewReader(""), Output: &bytes.Buffer{}}
		versionOptions := options
		versionOptions.ShowVersion = true
		for _, outcome := range []Outcome{Run(context.Background(), options), Run(context.Background(), versionOptions), RunSessionPicker(context.Background(), options, false)} {
			if outcome.ExitCode() != 2 || !strings.HasPrefix(outcome.Failure.Message, "plugin_argument_") {
				t.Fatal("invalid app input reached configuration", outcome)
			}
		}
		if _, err := os.Stat(root); !os.IsNotExist(err) {
			t.Fatal("invalid arguments created storage")
		}
	}
}
