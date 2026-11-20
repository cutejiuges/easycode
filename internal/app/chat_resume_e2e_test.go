//go:build darwin || linux

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

	"easycode/internal/config"
	"easycode/internal/domain"
	"easycode/internal/protocol"
	"easycode/internal/secret"
	"easycode/internal/session"
	"easycode/internal/tui"
)

func TestInChatResumePreservesCanonicalProviderContinuation(t *testing.T) {
	for _, family := range []domain.ProviderFamily{domain.ProviderOpenAI, domain.ProviderAnthropic} {
		t.Run(string(family), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			workspace := privateAppTempDir(t)
			sourceFile := filepath.Join(workspace, "source")
			if err := os.WriteFile(sourceFile, []byte("FROZEN_SWITCH_FILE\n"), 0600); err != nil {
				t.Fatal(err)
			}
			requests := make(chan []byte, 10)
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
				if n == 3 {
					var stream bytes.Buffer
					serveWriteScenarioSample(&stream, family, n, []writeScenarioCall{taskScenarioCall(family, false)})
					_, _ = w.Write(withReasoningFixture(family, stream.Bytes()))
					return
				}
				_, _ = w.Write(activeReasoningFixture(family, n, false))
			}))
			defer server.Close()
			cfg := cacheFixtureConfig(t, config.Config{Provider: config.Provider{
				Family: family, Model: "fixture-model", BaseURL: server.URL, APIKey: secret.New("chat-resume-fixture"),
			}}, true)
			cfg = reasoningFixtureConfig(t, cfg)
			dataHome := privateAppTempDir(t)
			paths := dataPaths{sessionRoot: filepath.Join(dataHome, "sessions"), catalogPath: filepath.Join(dataHome, "catalog.json")}
			target, err := openChatResources(ctx, cfg, paths.sessionRoot, "", workspace)
			if err != nil {
				t.Fatal(err)
			}
			defer closeResumeTestResources(t, target)
			if err := target.session.StartInteractive(ctx); err != nil {
				t.Fatal(err)
			}
			appEffortTurn(t, target, "ARCHIVE_BEFORE_SWITCH", false)
			waitCompactionIdle(t, target)
			compactForSwitch(t, ctx, target)
			appEffortTurn(t, target, "inspect @source", false)
			savedTasks := switchTaskSnapshot(t, ctx, target)
			if savedTasks.Counts.Total != 2 || savedTasks.Counts.InProgress != 1 {
				t.Fatal("target did not commit the planned tasks")
			}
			journal, err := target.service.repository.JournalPath(target.identity.ThreadID)
			if err != nil {
				t.Fatal(err)
			}
			closeResumeTestResources(t, target)
			durable := readSwitchJournal(t, journal)
			copyRoot, copyJournal, thread := installPreviewFixture(t, durable)
			if err := os.Remove(sourceFile); err != nil {
				t.Fatal(err)
			}
			if count.Load() != 4 {
				t.Fatalf("target creation sampled %d times", count.Load())
			}
			for range 4 {
				<-requests
			}

			environment := &chatEnvironment{}
			defer func() {
				if err := environment.close(); err != nil {
					t.Error(err)
				}
			}()
			source, err := openChatResourcesWithHooks(ctx, cfg, paths, "", false, workspace, chatResourceHooks{
				interactiveApprovals: true, environment: environment, lifetime: ctx,
			})
			if err != nil {
				t.Fatal(err)
			}
			defer closeResumeTestResources(t, source)
			if _, err := assembleChatModel(ctx, source); err != nil {
				t.Fatal(err)
			}
			appEffortSet(t, source, "source-effort", appEffortChoice(t, "low"))
			execute := newPickerExecutor(cfg, paths, workspace, nil, environment)
			candidates := make(chan *chatResources, 1)
			owner := newPickerOwner(func(operation, lifetime context.Context, request tui.PickerRequest) pickerExecution {
				execution := execute(operation, lifetime, request)
				candidates <- execution.resources
				return execution
			})
			owner.initial = source
			owner.Start(ctx)
			defer func() {
				owner.Stop()
				if err := owner.Wait(); err != nil {
					t.Error(err)
				}
			}()
			request := tui.PickerRequest{ID: 1, Operation: tui.PickerOpen, Source: source.identity.ThreadID, ThreadID: thread}
			if !owner.Submit(request) {
				t.Fatal("active picker did not admit a direct target")
			}
			ready := waitSwitchResult(t, ctx, owner)
			if ready.Request != request || ready.Stage != tui.PickerPrepared || ready.Chat == nil || !owner.Accept(request.ID) {
				t.Fatalf("candidate not ready: %s", ready.Error)
			}
			activated := waitSwitchResult(t, ctx, owner)
			switched := <-candidates
			if activated.Request != request || activated.Stage != tui.PickerActivated || activated.Chat == nil || switched.identity != target.identity {
				t.Fatalf("wrong candidate activated: %s", activated.Error)
			}
			if got := switchTaskSnapshot(t, ctx, switched); !savedTasks.Equal(got) {
				t.Fatal("switch lost the durable task state or source")
			}
			if got := appEffortSnapshot(t, switched, "switch-effort"); got.Source != "startup" || got.SelectionRevision != 0 {
				t.Fatal("candidate inherited source effort")
			}
			if count.Load() != 4 || !bytes.Equal(readSwitchJournal(t, journal), durable) {
				t.Fatal("switch sampled, reran tools or rewrote the completed journal")
			}

			restarted, err := openChatResources(ctx, cfg, copyRoot, string(thread), workspace)
			if err != nil {
				t.Fatal(err)
			}
			defer closeResumeTestResources(t, restarted)
			if _, err := assembleChatModel(ctx, restarted); err != nil {
				t.Fatal(err)
			}
			for _, current := range []*chatResources{switched, restarted} {
				appEffortTurn(t, current, "continue explicitly", false)
				waitCompactionIdle(t, current)
			}
			left, right := <-requests, <-requests
			if count.Load() != 6 || !bytes.Equal(left, right) {
				t.Fatal("switch and independent resume produced different next requests")
			}
			assertRequestFingerprintEqual(t, left, right)
			checkSwitchedNativeOrder(t, family, left)
			leftCache, rightCache := switchCacheSnapshot(t, ctx, switched), switchCacheSnapshot(t, ctx, restarted)
			if leftCache.StableFingerprint == "" || leftCache.StableFingerprint != rightCache.StableFingerprint ||
				leftCache.LastSample == nil || rightCache.LastSample == nil ||
				leftCache.LastSample.RequestFingerprint != rightCache.LastSample.RequestFingerprint ||
				leftCache.LastSample.NativeFingerprint != rightCache.LastSample.NativeFingerprint {
				t.Fatal("switch changed stable, native or request cache fingerprints")
			}
			owner.Stop()
			if err := owner.Wait(); err != nil {
				t.Fatal(err)
			}
			closeResumeTestResources(t, restarted)
			lastSeed := switchJournalRecord(t, durable, false)
			for _, path := range []string{journal, copyJournal} {
				records := readSwitchJournal(t, path)
				if !bytes.HasPrefix(records, durable) || switchJournalRecord(t, records[len(durable):], true).Sequence != lastSeed.Sequence+1 {
					t.Fatal("continuation did not append from the same next journal sequence")
				}
			}
		})
	}
}

func waitSwitchResult(t *testing.T, ctx context.Context, owner *pickerOwner) tui.PickerResult {
	t.Helper()
	select {
	case result, open := <-owner.Results():
		if !open {
			t.Fatal("picker closed before publishing the handoff result")
		}
		return result
	case <-ctx.Done():
		t.Fatal(ctx.Err())
		return tui.PickerResult{}
	}
}

func compactForSwitch(t *testing.T, ctx context.Context, resources *chatResources) {
	t.Helper()
	result, err := resources.session.CompactContext(ctx, "preserve test facts")
	if err != nil || result.Status() != protocol.CommandStatusAccepted {
		t.Fatal("target could not start compaction", err)
	}
	waitSwitchObservation(t, ctx, resources, protocol.EventCompactionCompleted, result.RequestID())
	waitCompactionIdle(t, resources)
}

func switchTaskSnapshot(t *testing.T, ctx context.Context, resources *chatResources) protocol.TaskProgressSnapshotPayload {
	t.Helper()
	const id protocol.RequestID = "switch-tasks"
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

func switchCacheSnapshot(t *testing.T, ctx context.Context, resources *chatResources) protocol.CacheSnapshotPayload {
	t.Helper()
	const id protocol.RequestID = "switch-cache"
	result, err := resources.session.QueryCache(ctx, id)
	if err != nil || result.Status() != protocol.CommandStatusAccepted {
		t.Fatal("cache snapshot not accepted", err)
	}
	payload, err := protocol.DecodeCacheSnapshot(waitSwitchObservation(t, ctx, resources, protocol.EventCacheSnapshot, id))
	if err != nil {
		t.Fatal(err)
	}
	return payload
}

func waitSwitchObservation(t *testing.T, ctx context.Context, resources *chatResources, kind protocol.EventKind, id protocol.RequestID) protocol.Event {
	t.Helper()
	for {
		select {
		case item, open := <-resources.session.Outputs():
			if !open {
				t.Fatal("control session closed before observation")
			}
			event, ok := item.ContextEvent()
			if ok && event.Kind == protocol.EventCompactionFailed {
				t.Fatalf("compaction failed: %s", event.Payload)
			}
			if ok && event.Kind == kind && item.RequestID() == id {
				return event
			}
		case <-ctx.Done():
			t.Fatalf("observation %s: %v", kind, ctx.Err())
		}
	}
}

func checkSwitchedNativeOrder(t *testing.T, family domain.ProviderFamily, request []byte) {
	t.Helper()
	call, output := []byte(`"type":"function_call"`), []byte(`"type":"function_call_output"`)
	if family == domain.ProviderAnthropic {
		call, output = []byte(`"type":"tool_use"`), []byte(`"type":"tool_result"`)
	}
	fileIndex, callIndex, outputIndex, answerIndex := bytes.Index(request, []byte("FROZEN_SWITCH_FILE")), bytes.Index(request, call), bytes.Index(request, output), bytes.Index(request, []byte("answer-4"))
	if fileIndex < 0 || callIndex <= fileIndex || outputIndex <= callIndex || answerIndex <= outputIndex ||
		!bytes.Contains(request, []byte("opaque-reasoning-sentinel")) || bytes.Contains(request, []byte("ARCHIVE_BEFORE_SWITCH")) ||
		!bytes.Contains(request, []byte("answer-2")) {
		t.Fatal("continuation lost checkpoint, opaque reasoning, frozen file or native tool pairing")
	}
}

func closeResumeTestResources(t *testing.T, resources *chatResources) {
	t.Helper()
	if err := closePickerResources(resources); err != nil {
		t.Error(err)
	}
}

func readSwitchJournal(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func switchJournalRecord(t *testing.T, data []byte, first bool) session.Record {
	t.Helper()
	data = bytes.TrimSuffix(data, []byte("\n"))
	if first {
		data, _, _ = bytes.Cut(data, []byte("\n"))
	} else if i := bytes.LastIndexByte(data, '\n'); i >= 0 {
		data = data[i+1:]
	}
	record, err := session.DecodeRecord(data)
	if err != nil {
		t.Fatal(err)
	}
	return record
}
