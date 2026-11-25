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
	"time"

	"easycode/internal/codec"
	"easycode/internal/config"
	"easycode/internal/domain"
	"easycode/internal/protocol"
	"easycode/internal/provider"
	"easycode/internal/secret"
)

func TestFrozenFileAndTaskProgressRestoreTogetherWithCanonicalRequests(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	workspace := privateAppTempDir(t)
	path := filepath.Join(workspace, "source")
	writeAppTestFile(t, path, []byte("first\nFROZEN_TASK_FILE\nlast\n"), 0600)
	requests := make(chan []byte, 4)
	release := make(chan struct{})
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		n := calls.Add(1)
		requests <- body
		if n == 1 {
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		var tools []writeScenarioCall
		if n == 1 {
			tools = []writeScenarioCall{{"update_plan", `{"plan":[{"step":"Inspect frozen source","status":"in_progress"},{"step":"Verify result","status":"pending"}]}`}}
		}
		serveWriteScenarioSample(w, domain.ProviderOpenAI, int(n), tools)
	}))
	defer server.Close()
	cfg := cacheFixtureConfig(t, config.Config{Provider: config.Provider{Family: domain.ProviderOpenAI, BaseURL: server.URL, Model: "fixture-model", APIKey: secret.New("task-file-fixture")}}, true)
	live := mustOpenChatResources(t, ctx, cfg, filepath.Join(privateAppTempDir(t), "sessions"), "", workspace)
	defer live.close(context.Background())
	boundary := &countingFileBoundary{workspaceFileBoundary: live.workspace}
	live.fileSource.workspace = boundary
	if err := live.session.StartInteractive(ctx); err != nil {
		t.Fatal(err)
	}
	const raw = "inspect @source#L2-2"
	if _, err := appendInteractiveText(ctx, live.session, raw); err != nil {
		t.Fatal(err)
	}
	first := <-requests
	if bytes.Count(first, []byte("# Attached workspace file")) != 1 || !bytes.Contains(first, []byte("FROZEN_TASK_FILE")) {
		t.Fatal("initial request omitted the prepared attachment")
	}
	writeAppTestFile(t, path, []byte("CHANGED_AFTER_FREEZE\n"), 0600)
	close(release)
	appEffortTerminal(t, live, false)
	appEffortIdle(t, live)
	second := <-requests
	if calls.Load() != 2 || boundary.reads.Load() != 1 || !bytes.Contains(second, []byte("FROZEN_TASK_FILE")) || bytes.Contains(second, []byte("CHANGED_AFTER_FREEZE")) || !bytes.Contains(second, []byte("Inspect frozen source")) {
		t.Fatal("task execution changed frozen file input or repeated preparation")
	}
	query := func(r *chatResources) protocol.TaskProgressSnapshotPayload {
		t.Helper()
		journal, err := r.service.repository.JournalPath(r.identity.ThreadID)
		if err != nil {
			t.Fatal(err)
		}
		before := readAppTestFile(t, journal)
		network := calls.Load()
		snapshot := switchTaskSnapshot(t, ctx, r)
		after, err := os.ReadFile(journal)
		if err != nil || !bytes.Equal(before, after) || calls.Load() != network {
			t.Fatal("task query changed durable history or sampled", err)
		}
		return snapshot
	}
	saved := query(live)
	if saved.Counts.Total != 2 || saved.Counts.InProgress != 1 {
		t.Fatal("task tool did not publish the model progress")
	}
	liveHistory := live.transcriptQueries.projector.(provider.Conversation).ProjectHistory()
	journal, err := live.service.repository.JournalPath(live.identity.ThreadID)
	if err != nil {
		t.Fatal(err)
	}
	durable := readAppTestFile(t, journal)
	root, restoredJournal, thread := installPreviewFixture(t, durable)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	restored, err := openChatResources(ctx, cfg, root, string(thread), workspace)
	if err != nil {
		t.Fatal("offline restore required a source file", err)
	}
	defer restored.close(context.Background())
	if err := restored.session.StartInteractive(ctx); err != nil {
		t.Fatal(err)
	}
	if !query(restored).Equal(saved) || calls.Load() != 2 {
		t.Fatal("restored task progress differs or restore sampled")
	}
	liveBytes, err := codec.MarshalStable(liveHistory)
	if err != nil {
		t.Fatal(err)
	}
	restoredBytes, err := codec.MarshalStable(restored.history)
	if err != nil || !bytes.Equal(liveBytes, restoredBytes) || restored.history.Turns[0].Messages[0].Text != raw || restored.history.Turns[0].Messages[0].Files.Items()[0].Content() != "FROZEN_TASK_FILE\n" {
		t.Fatal("restored native history changed the frozen attachment or task ordering", err)
	}
	afterRestore, err := os.ReadFile(restoredJournal)
	if err != nil || !bytes.Equal(durable, afterRestore) {
		t.Fatal("restore rewrote the completed journal", err)
	}
	appEffortTurn(t, live, "continue", false)
	appEffortTurn(t, restored, "continue", false)
	left, right := <-requests, <-requests
	if calls.Load() != 4 || !bytes.Equal(left, right) || boundary.reads.Load() != 1 || bytes.Count(left, []byte("# Attached workspace file")) != 1 {
		t.Fatal("live and restored requests lost native evidence or canonical equality")
	}
	assertRequestFingerprintEqual(t, left, right)
	input := bytes.Index(left, []byte("FROZEN_TASK_FILE"))
	call := bytes.Index(left, []byte(`"type":"function_call"`))
	output := bytes.Index(left, []byte(`"type":"function_call_output"`))
	answer := bytes.Index(left, []byte("answer-2"))
	if input < 0 || call <= input || output <= call || answer <= output {
		t.Fatal("file, task call/output and assistant native order changed")
	}
}

func switchTaskSnapshot(t *testing.T, ctx context.Context, resources *chatResources) protocol.TaskProgressSnapshotPayload {
	t.Helper()
	requestID, err := domain.GenerateInputID()
	if err != nil {
		t.Fatal(err)
	}
	id := protocol.RequestID("switch-tasks-" + string(requestID))
	result, err := resources.session.QueryTasks(ctx, id)
	if err != nil || result.Status() != protocol.CommandStatusAccepted {
		t.Fatal("task snapshot not accepted", err)
	}
	payload, err := protocol.DecodeTaskProgressSnapshot(waitSwitchObservation(t, ctx, resources, protocol.EventTaskProgressSnapshot, id))
	if err != nil {
		t.Fatal(err)
	}
	return payload
}
