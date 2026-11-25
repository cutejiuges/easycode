//go:build darwin || linux

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
	"sync/atomic"
	"testing"
	"time"

	"easycode/internal/config"
	"easycode/internal/domain"
	"easycode/internal/headless"
	"easycode/internal/protocol"
	"easycode/internal/secret"
	"easycode/internal/session"
	"easycode/internal/tui"

	"github.com/coder/websocket"
)

func TestSessionBranchPreservesNativeContinuation(t *testing.T) {
	for _, family := range []domain.ProviderFamily{domain.ProviderOpenAI, domain.ProviderAnthropic} {
		t.Run(string(family), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			workspace := privateAppTempDir(t)
			file := filepath.Join(workspace, "source")
			if err := os.WriteFile(file, []byte("FROZEN_SWITCH_FILE\n"), 0600); err != nil {
				t.Fatal(err)
			}
			requests := make(chan []byte, 12)
			var count atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
					return
				}
				n := int(count.Add(1))
				select {
				case requests <- body:
				case <-r.Context().Done():
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				if n == 3 || n == 5 || n == 9 {
					var stream bytes.Buffer
					serveWriteScenarioSample(&stream, family, n, []writeScenarioCall{taskScenarioCall(family, n != 3)})
					_, _ = w.Write(withReasoningFixture(family, stream.Bytes()))
					return
				}
				_, _ = w.Write(activeReasoningFixture(family, n, false))
			}))
			defer server.Close()
			cfg := reasoningFixtureConfig(t, cacheFixtureConfig(t, config.Config{Provider: config.Provider{
				Family: family, Model: "fixture-model", BaseURL: server.URL, APIKey: secret.New("branch-fixture"),
			}}, true))
			dataHome := privateAppTempDir(t)
			paths := dataPaths{sessionRoot: filepath.Join(dataHome, "sessions"), catalogPath: filepath.Join(dataHome, "catalog.json")}
			owner, source, candidates := startBranchFixture(t, ctx, cfg, paths, workspace)

			appEffortTurn(t, source, "ARCHIVE_BEFORE_SWITCH", false)
			waitCompactionIdle(t, source)
			compactForSwitch(t, ctx, source)
			appEffortTurn(t, source, "inspect @source", false)
			waitCompactionIdle(t, source)
			tasks := switchTaskSnapshot(t, ctx, source)
			if tasks.Counts.Total != 2 || tasks.Counts.InProgress != 1 {
				t.Fatal("source did not commit the planned tasks")
			}
			confirmTitle(t, ctx, source.session, "branch-rename-source", "Original")
			ordinaryContext := branchContextFixture(t, ctx, source)
			sourcePath, err := source.service.repository.JournalPath(source.identity.ThreadID)
			if err != nil {
				t.Fatal(err)
			}
			retained := readSwitchJournal(t, sourcePath)
			const rewindPrompt = "excluded source alternative"
			appEffortTurn(t, source, rewindPrompt, false)
			waitCompactionIdle(t, source)
			if got := switchTaskSnapshot(t, ctx, source); tasks.Equal(got) {
				t.Fatal("source fixture did not change tasks after the cut")
			}
			confirmTitle(t, ctx, source.session, "branch-rename-later", "Later title")
			setAppWorkMode(t, ctx, source, domain.WorkModePlan, "branch-source-plan")
			appEffortSet(t, source, "branch-source-effort", appEffortChoice(t, "low"))
			durable := readSwitchJournal(t, sourcePath)
			const currentFile = "CHANGED_AFTER_FROZEN_READ\n"
			if err := os.WriteFile(file, []byte(currentFile), 0600); err != nil {
				t.Fatal(err)
			}
			for range 6 {
				<-requests
			}
			branch := activateHistoricalBranchFixture(t, ctx, owner, source, candidates, rewindPrompt)
			if branch.identity.SessionID != source.identity.SessionID || branch.identity.ThreadID == source.identity.ThreadID ||
				count.Load() != 6 || !bytes.Equal(readSwitchJournal(t, sourcePath), durable) {
				t.Fatal("branch identity, source bytes or request count differ")
			}
			if current, err := os.ReadFile(file); err != nil || string(current) != currentFile {
				t.Fatal("branch changed workspace bytes", err)
			}
			if got := switchTaskSnapshot(t, ctx, branch); !tasks.Equal(got) {
				t.Fatal("branch lost inherited task state or its durable source")
			}
			if got := appEffortSnapshot(t, branch, "branched-effort"); got.Source != "startup" || got.SelectionRevision != 0 {
				t.Fatal("branch inherited temporary effort")
			}
			path, err := branch.service.repository.JournalPath(branch.identity.ThreadID)
			if err != nil {
				t.Fatal(err)
			}
			seed := readSwitchJournal(t, path)
			last := switchJournalRecord(t, seed, false)
			if !bytes.HasPrefix(seed, retained) || last.EventKind != session.EventThreadForked ||
				last.Sequence != switchJournalRecord(t, retained, false).Sequence+1 || last.ParentThreadID != source.identity.ThreadID {
				t.Fatal("branch did not seal the exact committed source prefix")
			}
			// 使用同一分支 ID 的独立副本比对续写，副本所在仓库没有源 journal。
			copyRoot, _ := installThreadFixture(t, seed, branch.identity.ThreadID)
			preview, _, err := previewSession(ctx, copyRoot, branch.identity.ThreadID, defaultPreviewDependencies())
			if err != nil || preview.Title != "Original (Branch)" || preview.ParentThreadID != source.identity.ThreadID || len(preview.Messages) < 4 {
				t.Fatalf("independent branch preview failed: %+v %v", preview, err)
			}
			var catalogOut, catalogErr bytes.Buffer
			outcome := ListSessions(ctx, SessionListOptions{
				All: true, Limit: 20, SessionDataRoot: copyRoot, SessionCatalogPath: filepath.Join(filepath.Dir(copyRoot), "catalog.json"),
				Output: &catalogOut, ErrorOutput: &catalogErr,
			})
			if outcome.ExitCode() != 0 || catalogErr.Len() != 0 ||
				!bytes.Contains(catalogOut.Bytes(), []byte("Branch of: "+source.identity.ThreadID)) ||
				!bytes.Contains(catalogOut.Bytes(), []byte("Title: Original (Branch)")) {
				t.Fatalf("branch missing in rebuilt catalog: %s %s", catalogOut.String(), catalogErr.String())
			}
			restarted, err := openChatResources(ctx, cfg, copyRoot, string(branch.identity.ThreadID), workspace)
			if err != nil {
				t.Fatal(err)
			}
			defer closeResumeTestResources(t, restarted)
			if _, err := assembleChatModel(ctx, restarted); err != nil {
				t.Fatal(err)
			}
			branchContext := branchContextFixture(t, ctx, branch)
			if !reflect.DeepEqual(branchContext.Sources, branchContextFixture(t, ctx, restarted).Sources) ||
				branchContext.Sources[0].Revision != ordinaryContext.Sources[0].Revision ||
				branchContext.Sources[1].Revision != ordinaryContext.Sources[1].Revision {
				t.Fatal("branch inherited Plan Mode or lost the restored native footprint")
			}
			confirmTitle(t, ctx, branch.session, "branch-rename-child", "Renamed alternative")
			for _, current := range []*chatResources{branch, restarted} {
				appEffortTurn(t, current, "continue branch explicitly", false)
				waitCompactionIdle(t, current)
			}
			left, right := <-requests, <-requests
			if count.Load() != 8 || !bytes.Equal(left, right) || bytes.Contains(left, []byte(rewindPrompt)) {
				t.Fatal("live and independently resumed branch produced different canonical requests")
			}
			assertRequestFingerprintEqual(t, left, right)
			checkSwitchedNativeOrder(t, family, left)
			first, second := switchCacheSnapshot(t, ctx, branch), switchCacheSnapshot(t, ctx, restarted)
			if first.StableFingerprint == "" || first.StableFingerprint != second.StableFingerprint ||
				first.LastSample == nil || second.LastSample == nil ||
				first.LastSample.RequestFingerprint != second.LastSample.RequestFingerprint ||
				first.LastSample.NativeFingerprint != second.LastSample.NativeFingerprint {
				t.Fatal("branch changed stable content or native fingerprint")
			}
			if family == domain.ProviderOpenAI {
				key, _ := domain.ThreadCacheKey(branch.identity.ThreadID)
				if !bytes.Contains(left, []byte(key)) || bytes.Contains(left, []byte("previous_response_id")) {
					t.Fatal("OpenAI branch reused source routing or omitted its key")
				}
			} else if !bytes.Contains(left, []byte(`"ttl":"1h"`)) {
				t.Fatal("Anthropic branch lost its cache marker policy")
			}
			appEffortTurn(t, branch, "update branch tasks", false)
			waitCompactionIdle(t, branch)
			updated := switchTaskSnapshot(t, ctx, branch)
			if tasks.Equal(updated) {
				t.Fatal("the branch did not advance its task state")
			}
			compactForSwitch(t, ctx, branch)
			waitCompactionIdle(t, branch)
			owner.Stop()
			if err := owner.Wait(); err != nil {
				t.Fatal(err)
			}
			reopened, err := openChatResources(ctx, cfg, paths.sessionRoot, string(branch.identity.ThreadID), workspace)
			if err != nil {
				t.Fatal(err)
			}
			defer closeResumeTestResources(t, reopened)
			if _, err := assembleChatModel(ctx, reopened); err != nil {
				t.Fatal(err)
			}
			if got := switchTaskSnapshot(t, ctx, reopened); !updated.Equal(got) || count.Load() != 11 {
				t.Fatal("branch compaction lost task progress or ran a model during recovery")
			}
			if !bytes.Equal(readSwitchJournal(t, sourcePath), durable) || reopened.title != "Renamed alternative" {
				t.Fatal("branch continuation changed source journal or failed to persist its own title")
			}
			if family == domain.ProviderOpenAI {
				closeResumeTestResources(t, restarted)
				for range 3 {
					<-requests
				}
				checkBranchHeadlessContinue(t, ctx, workspace, copyRoot, server.URL, branch.identity)
				if !bytes.Contains(<-requests, []byte(`"/rewind"`)) {
					t.Fatal("headless continuation did not forward the literal prompt")
				}
			}
		})
	}
}

func checkBranchHeadlessContinue(t *testing.T, ctx context.Context, workspace, root, url string, identity session.Identity) {
	t.Helper()
	t.Chdir(workspace)
	for _, key := range []string{"EASYCODE_PROVIDER", "EASYCODE_BASE_URL", "EASYCODE_API_KEY", "EASYCODE_MODEL"} {
		t.Setenv(key, "")
	}
	configPath := filepath.Join(t.TempDir(), "config.json")
	configBody := fmt.Sprintf(`{"provider":"openai","base_url":%q,"api_key":"fixture","model":"fixture-model"}`, url)
	if err := os.WriteFile(configPath, []byte(configBody), 0600); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	outcome := Run(ctx, Options{
		Mode: headless.ModeJSON, Prompt: "/rewind", ContinueSession: true,
		ConfigPath: configPath, SessionDataRoot: root, SessionCatalogPath: filepath.Join(filepath.Dir(root), "catalog.json"), Output: &output,
	})
	if outcome.ExitCode() != 0 {
		t.Fatalf("headless branch continuation failed: %+v", outcome)
	}
	started := decodeThreadStarted(t, output.String())
	if !started.Resumed || started.ThreadID != identity.ThreadID || started.SessionID != identity.SessionID ||
		bytes.Contains(output.Bytes(), []byte("parent_thread_id")) || bytes.Contains(output.Bytes(), []byte("ARCHIVE_BEFORE_SWITCH")) ||
		!bytes.Contains(output.Bytes(), []byte("answer-12")) {
		t.Fatalf("headless continuation changed the external stream or replayed history: %s", output.String())
	}
}

func startBranchFixture(t *testing.T, ctx context.Context, cfg config.Config, paths dataPaths, workspace string) (*pickerOwner, *chatResources, <-chan *chatResources) {
	t.Helper()
	environment := &chatEnvironment{}
	t.Cleanup(func() {
		if err := environment.close(); err != nil {
			t.Error(err)
		}
	})
	source, err := openChatResourcesWithHooks(ctx, cfg, paths, "", false, workspace, chatResourceHooks{
		interactiveApprovals: true, environment: environment, lifetime: ctx,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := assembleChatModel(ctx, source); err != nil {
		closeResumeTestResources(t, source)
		t.Fatal(err)
	}
	owner := newPickerOwner(newPickerExecutor(cfg, paths, workspace, nil, environment))
	binding, err := bindProvider(cfg.Provider.Family)
	if err != nil {
		t.Fatal(err)
	}
	owner.rewind = newRewindPickerExecutor(binding, cfg.Provider.Model, workspace)
	executor := newBranchPickerExecutor(cfg, paths, workspace, nil, environment)
	candidates := make(chan *chatResources, 1)
	owner.branch = func(operation, lifetime context.Context, request tui.PickerRequest, source *chatResources, selection *rewindCandidate, admit func(domain.ThreadID) bool) pickerExecution {
		result := executor(operation, lifetime, request, source, selection, admit)
		candidates <- result.resources
		return result
	}
	owner.initial = source
	owner.Start(ctx)
	t.Cleanup(func() {
		owner.Stop()
		if err := owner.Wait(); err != nil {
			t.Error(err)
		}
	})
	return owner, source, candidates
}

func activateHistoricalBranchFixture(t *testing.T, ctx context.Context, owner *pickerOwner, source *chatResources, candidates <-chan *chatResources, prompt string) *chatResources {
	t.Helper()
	browse := tui.PickerRequest{ID: 1, Operation: tui.PickerRewind, Source: source.identity.ThreadID}
	if !owner.Submit(browse) {
		t.Fatal("rewind browse admission failed")
	}
	list := waitSwitchResult(t, ctx, owner)
	if list.Request != browse || list.Stage != tui.PickerCompleted || list.Error != "" || len(list.RewindEntries) == 0 {
		t.Fatalf("rewind list failed: %+v", list)
	}
	entry := list.RewindEntries[len(list.RewindEntries)-1]
	request := tui.PickerRequest{ID: 2, Operation: tui.PickerBranch, Source: browse.Source, BrowseID: browse.ID, RewindSelection: entry.RewindSelection}
	branch, activated := finishBranchFixture(t, ctx, owner, request, candidates)
	if activated.RewindDraft.Text != prompt {
		t.Fatal("activation did not return the verified prompt")
	}
	return branch
}

func finishBranchFixture(t *testing.T, ctx context.Context, owner *pickerOwner, request tui.PickerRequest, candidates <-chan *chatResources) (*chatResources, tui.PickerResult) {
	t.Helper()
	if !owner.Submit(request) {
		t.Fatal("branch admission failed")
	}
	ready := waitSwitchResult(t, ctx, owner)
	if ready.Request != request || ready.Stage != tui.PickerPrepared || ready.Chat == nil || ready.RewindDraft.Text != "" || !owner.Accept(request.ID) {
		t.Fatalf("branch not ready: %+v", ready)
	}
	activated := waitSwitchResult(t, ctx, owner)
	if activated.Request != request || activated.Stage != tui.PickerActivated || activated.Chat == nil ||
		activated.BranchThreadID != ready.BranchThreadID || !activated.BranchThreadID.Valid() {
		t.Fatalf("branch not activated: %+v", activated)
	}
	// 只获取与该尝试绑定的 fixture 资源，生产入口始终仅暴露 typed TUI 结果。
	branch := <-candidates
	if branch == nil || branch.identity.ThreadID != activated.BranchThreadID {
		t.Fatal("activated branch did not retain the bound resources")
	}
	return branch, activated
}

func branchContextFixture(t *testing.T, ctx context.Context, resources *chatResources) protocol.ContextSnapshotPayload {
	t.Helper()
	const id protocol.RequestID = "branch-context"
	if result, err := resources.session.QueryContext(ctx, id); err != nil || result.Status() != protocol.CommandStatusAccepted {
		t.Fatal("context snapshot not accepted", err)
	}
	result, err := protocol.DecodeContextSnapshot(waitSwitchObservation(t, ctx, resources, protocol.EventContextSnapshot, id))
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestSessionBranchStartsOpenAIWebSocketWithoutSourceCursor(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	requests := make(chan []byte, 4)
	var connections, attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		socket, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer socket.CloseNow()
		connections.Add(1)
		for {
			_, payload, err := socket.Read(ctx)
			if err != nil {
				return
			}
			n := int(attempts.Add(1))
			select {
			case requests <- payload:
			case <-ctx.Done():
				return
			}
			var events bytes.Buffer
			writeOpenAIAppTurn(&events, n)
			for _, line := range bytes.Split(events.Bytes(), []byte("\n")) {
				if event, ok := bytes.CutPrefix(line, []byte("data: ")); ok {
					if err := socket.Write(ctx, websocket.MessageText, event); err != nil {
						return
					}
				}
			}
		}
	}))
	defer server.Close()
	cfg := cacheFixtureConfigMode(t, config.Config{Provider: config.Provider{
		Family: domain.ProviderOpenAI, Model: "fixture-model", BaseURL: server.URL, APIKey: secret.New("branch-websocket"),
	}}, true, "websocket_auto")
	workspace, home := privateAppTempDir(t), privateAppTempDir(t)
	paths := dataPaths{sessionRoot: filepath.Join(home, "sessions"), catalogPath: filepath.Join(home, "catalog.json")}
	owner, source, candidates := startBranchFixture(t, ctx, cfg, paths, workspace)
	for _, prompt := range []string{"original question", "original follow-up"} {
		appEffortTurn(t, source, prompt, false)
		waitCompactionIdle(t, source)
	}
	<-requests
	if previous := <-requests; !bytes.Contains(previous, []byte(`"previous_response_id":"resp-1"`)) {
		t.Fatal("source failed to establish an incremental cursor")
	}
	branch := activateHistoricalBranchFixture(t, ctx, owner, source, candidates, "original follow-up")
	appEffortTurn(t, branch, "branch question", false)
	body := <-requests
	key, _ := domain.ThreadCacheKey(branch.identity.ThreadID)
	old, _ := domain.ThreadCacheKey(source.identity.ThreadID)
	if connections.Load() != 2 || attempts.Load() != 3 || !bytes.Contains(body, []byte(key)) ||
		bytes.Contains(body, []byte(old)) || bytes.Contains(body, []byte("previous_response_id")) ||
		!bytes.Contains(body, []byte("original question")) || !bytes.Contains(body, []byte("answer-1")) ||
		bytes.Contains(body, []byte("original follow-up")) || bytes.Contains(body, []byte("answer-2")) {
		t.Fatal("branch did not send a full request over its own connection and cache key")
	}
}
