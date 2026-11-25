package app

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"easycode/internal/codec"
	"easycode/internal/config"
	contextplan "easycode/internal/context"
	"easycode/internal/domain"
	"easycode/internal/fault"
	"easycode/internal/headless"
	"easycode/internal/secret"
	"easycode/internal/session"
	"easycode/internal/tui"
	tea "github.com/charmbracelet/bubbletea"
)

func TestHeadlessProvidersPreserveResumeRequestAndOutputBoundaries(t *testing.T) {
	fixtures := []struct {
		name        string
		family      domain.ProviderFamily
		model       string
		serveTurn   func(io.Writer, int)
		assertThird func(*testing.T, []byte)
	}{
		{name: "OpenAI Responses", family: domain.ProviderOpenAI, model: "gpt-test", serveTurn: writeOpenAIAppTurn, assertThird: assertOpenAIThirdRequest},
		{name: "Anthropic Messages", family: domain.ProviderAnthropic, model: "claude-test", serveTurn: writeAnthropicAppTurn, assertThird: assertAnthropicThirdRequest},
	}
	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			startupCWD := t.TempDir()
			t.Chdir(startupCWD)
			var recorder requestRecorder
			logicalTurns := []int{1, 2, 3, 1, 2, 3}
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				requestIndex, _ := recorder.record(t, request)
				writer.Header().Set("Content-Type", "text/event-stream")
				fixture.serveTurn(writer, logicalTurns[requestIndex])
			}))
			defer server.Close()

			providerConfig := config.Config{Provider: config.Provider{
				Family: fixture.family, BaseURL: server.URL,
				APIKey: secret.New("headless-e2e-secret"), Model: fixture.model,
			}}
			live := mustOpenChatResources(t,
				context.Background(), providerConfig, filepath.Join(t.TempDir(), "live"), "", startupCWD,
			)
			submitAppTurn(t, live, "first")
			submitAppTurn(t, live, "second")
			submitAppTurn(t, live, "third")
			closeAppTestResources(t, live)

			dataHome := privateAppTempDir(t)
			dataRoot := filepath.Join(dataHome, "sessions")
			catalogPath := filepath.Join(dataHome, "catalog.json")
			configPath := writeAppConfig(t, t.TempDir(), fixture.family, server.URL)
			var firstOutput bytes.Buffer
			first := Run(context.Background(), Options{
				Mode: headless.ModeJSON, Prompt: "first", ConfigPath: configPath,
				SessionDataRoot: dataRoot, SessionCatalogPath: catalogPath, Output: &firstOutput,
			})
			if first.ExitCode() != 0 {
				t.Fatalf("first headless outcome = %#v", first)
			}
			if _, err := os.Stat(catalogPath); !os.IsNotExist(err) {
				t.Fatalf("ordinary startup created catalog: %v", err)
			}
			corruptCatalog := []byte("corrupt-catalog-fixture")
			writeAppTestFile(t, catalogPath, corruptCatalog, 0o600)
			started := decodeThreadStarted(t, firstOutput.String())
			if started.Resumed || !started.SessionID.Valid() || !started.ThreadID.Valid() {
				t.Fatalf("first thread.started = %#v", started)
			}

			var resumedOutput bytes.Buffer
			resumed := Run(context.Background(), Options{
				Mode: headless.ModeJSON, Prompt: "second", ConfigPath: configPath,
				ResumeThreadID: string(started.ThreadID), SessionDataRoot: dataRoot,
				SessionCatalogPath: catalogPath, Output: &resumedOutput,
			})
			if resumed.ExitCode() != 0 {
				t.Fatalf("resumed JSON outcome = %#v", resumed)
			}
			resumedStarted := decodeThreadStarted(t, resumedOutput.String())
			if !resumedStarted.Resumed || resumedStarted.SessionID != started.SessionID || resumedStarted.ThreadID != started.ThreadID {
				t.Fatalf("resumed thread.started = %#v, first = %#v", resumedStarted, started)
			}
			if strings.Contains(resumedOutput.String(), "answer-1") || !strings.Contains(resumedOutput.String(), "answer-2") {
				t.Fatalf("resumed JSON replayed or omitted text: %s", resumedOutput.String())
			}
			preservedCatalog, err := os.ReadFile(catalogPath)
			if err != nil || !bytes.Equal(preservedCatalog, corruptCatalog) {
				t.Fatalf("explicit resume touched catalog: %q, %v", preservedCatalog, err)
			}
			preview, cleanupFailed, err := previewSession(context.Background(), dataRoot, started.ThreadID, defaultPreviewDependencies())
			if err != nil || cleanupFailed || len(preview.Messages) == 0 {
				t.Fatalf("preview before continue failed: %v", err)
			}
			uiResources := mustOpenChatResources(t, context.Background(), providerConfig, dataRoot, string(started.ThreadID), t.TempDir())
			t.Cleanup(func() { uiResources.close(context.Background()) })
			ui := tui.NewModel(context.Background(), "test", uiResources.session).WithTranscriptQueries(started.ThreadID, uiResources.transcriptQueries)
			var restored tea.Model = ui
			initial := ui.Init()()
			if batch, ok := initial.(tea.BatchMsg); ok {
				// 首项长期等待控制流，其余初始化命令加载真实历史。
				for _, command := range batch[1:] {
					runCompletionCommand(t, &restored, command)
				}
			} else {
				restored, _ = ui.Update(initial)
			}
			for _, visible := range []string{"User: first", "● answer-1", "User: second", "● answer-2"} {
				if !strings.Contains(restored.View(), visible) {
					t.Fatalf("resumed transcript missing %q", visible)
				}
			}
			closeAppTestResources(t, uiResources)
			previewRequests := len(recorder.snapshot())
			if previewRequests != 5 {
				t.Fatal("preview or history display contacted provider")
			}

			var textOutput bytes.Buffer
			third := Run(context.Background(), Options{
				Mode: headless.ModeText, Prompt: "third", ConfigPath: configPath,
				ContinueSession: true, SessionDataRoot: dataRoot, SessionCatalogPath: catalogPath,
				Output: &textOutput,
			})
			if third.ExitCode() != 0 || textOutput.String() != "answer-3\n" {
				t.Fatalf("third outcome/output = %#v/%q", third, textOutput.String())
			}

			captured := recorder.snapshot()
			if len(captured) != 6 {
				t.Fatalf("request count = %d", len(captured))
			}
			if !bytes.Equal(captured[1], captured[4]) {
				t.Fatalf("uninterrupted and explicit-resume request bytes differ\nlive: %s\nrestored: %s", captured[1], captured[4])
			}
			if !bytes.Equal(captured[2], captured[5]) {
				t.Fatalf("uninterrupted and continue request bytes differ\nlive: %s\ncontinued: %s", captured[2], captured[5])
			}
			assertRequestFingerprintEqual(t, captured[1], captured[4])
			assertRequestFingerprintEqual(t, captured[2], captured[5])
			fixture.assertThird(t, captured[5])

			loaded := loadHeadlessJournal(t, dataRoot, started.ThreadID)
			if len(loaded.Records) != 20 || loaded.NextSequence != 21 {
				t.Fatalf("records/next = %d/%d", len(loaded.Records), loaded.NextSequence)
			}
			for index, record := range loaded.Records {
				if record.Sequence != uint64(index+1) || strings.Contains(string(record.EventKind), ".") {
					t.Fatalf("record[%d] = %#v", index, record)
				}
				if strings.Contains(string(record.Payload), "file-secret") || strings.Contains(string(record.Payload), server.URL) {
					t.Fatal("journal contains connection secret")
				}
			}
			for _, index := range []int{6, 12, 18} {
				if loaded.Records[index].EventKind != session.EventSampleUsage {
					t.Fatalf("record[%d] kind = %s", index, loaded.Records[index].EventKind)
				}
			}
			for _, forbidden := range []string{
				catalogPath, dataRoot, startupCWD, "file-secret", string(started.SessionID), string(started.ThreadID),
				loaded.Records[len(loaded.Records)-1].Timestamp.Format(time.RFC3339Nano),
			} {
				if strings.Contains(string(captured[5]), forbidden) {
					t.Fatalf("continue request contains catalog/session metadata %q", forbidden)
				}
			}
			assertCatalogFilesExclude(t, catalogPath, []string{
				"file-secret", server.URL, "first", "second", "third",
				"answer-1", "answer-2", "answer-3", "Authorization",
			})
			fresh := mustOpenChatResources(t, context.Background(), providerConfig, dataRoot, "", t.TempDir())
			t.Cleanup(func() { fresh.close(context.Background()) })
			if fresh.identity.ThreadID == started.ThreadID || len(fresh.history.Turns) != 0 {
				t.Fatal("non-resume startup reused history")
			}
			closeAppTestResources(t, fresh)
		})
	}
}

func TestStreamJSONProvidersPreserveSameProcessNativeHistory(t *testing.T) {
	fixtures := []struct {
		name      string
		family    domain.ProviderFamily
		model     string
		serveTurn func(io.Writer, int)
	}{
		{name: "OpenAI Responses", family: domain.ProviderOpenAI, model: "gpt-test", serveTurn: writeOpenAIAppTurn},
		{name: "Anthropic Messages", family: domain.ProviderAnthropic, model: "claude-test", serveTurn: writeAnthropicAppTurn},
	}
	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			startupCWD := t.TempDir()
			t.Chdir(startupCWD)
			var recorder requestRecorder
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				index, _ := recorder.record(t, request)
				writer.Header().Set("Content-Type", "text/event-stream")
				fixture.serveTurn(writer, index%2+1)
			}))
			defer server.Close()

			dataRoot := filepath.Join(privateAppTempDir(t), "sessions")
			configPath := writeAppConfig(t, t.TempDir(), fixture.family, server.URL)
			input := io.NopCloser(strings.NewReader(
				`{"version":1,"type":"input.submit","request_id":"stream-input-1","text":"first"}` + "\n" +
					`{"version":1,"type":"input.submit","request_id":"stream-input-2","text":"/rewind"}` + "\n",
			))
			output := &appWriteCloser{}
			outcome := Run(context.Background(), Options{
				Mode: headless.ModeStreamJSON, ConfigPath: configPath,
				SessionDataRoot: dataRoot, Input: input, Output: output,
			})
			if outcome.ExitCode() != 0 {
				t.Fatalf("stream outcome = %#v, output=%s", outcome, output.String())
			}
			if strings.Count(output.String(), `"type":"control.response"`) != 2 ||
				strings.Count(output.String(), `"type":"turn.completed"`) != 2 {
				t.Fatalf("stream output = %s", output.String())
			}
			started := decodeThreadStarted(t, output.String())
			loaded := loadHeadlessJournal(t, dataRoot, started.ThreadID)
			if len(loaded.Records) != 14 || loaded.NextSequence != 15 {
				t.Fatalf("stream records/next = %d/%d", len(loaded.Records), loaded.NextSequence)
			}

			providerConfig := config.Config{Provider: config.Provider{
				Family: fixture.family, BaseURL: server.URL,
				APIKey: secret.New("stream-e2e-secret"), Model: fixture.model,
			}}
			direct := mustOpenChatResources(t,
				context.Background(), providerConfig, filepath.Join(privateAppTempDir(t), "direct"), "", startupCWD,
			)
			submitAppTurn(t, direct, "first")
			submitAppTurn(t, direct, "/rewind")
			closeAppTestResources(t, direct)

			captured := recorder.snapshot()
			if len(captured) != 4 {
				t.Fatalf("request count = %d", len(captured))
			}
			if !bytes.Equal(captured[0], captured[2]) || !bytes.Equal(captured[1], captured[3]) ||
				!bytes.Contains(captured[1], []byte(`"/rewind"`)) {
				t.Fatalf("AgentLoop changed Provider request bytes\nstream: %s\ndirect: %s", captured[1], captured[3])
			}
			assertRequestFingerprintEqual(t, captured[1], captured[3])
			for _, metadata := range []string{"stream-input-1", "stream-input-2", "control.response", "input_id"} {
				if bytes.Contains(captured[1], []byte(metadata)) {
					t.Fatalf("Provider request contains control metadata %q: %s", metadata, captured[1])
				}
			}
		})
	}
}

type appWriteCloser struct{ bytes.Buffer }

func (*appWriteCloser) Close() error { return nil }

func assertRequestFingerprintEqual(t *testing.T, left []byte, right []byte) {
	t.Helper()
	leftJSON := canonicalizeCapturedRequest(t, left)
	rightJSON := canonicalizeCapturedRequest(t, right)
	leftSegment, err := contextplan.NewSegment("provider-request", contextplan.StabilityTurnStable, "v1", leftJSON)
	if err != nil {
		t.Fatal(err)
	}
	rightSegment, err := contextplan.NewSegment("provider-request", contextplan.StabilityTurnStable, "v1", rightJSON)
	if err != nil {
		t.Fatal(err)
	}
	if leftSegment.Fingerprint() != rightSegment.Fingerprint() {
		t.Fatalf("request fingerprints differ: %s != %s", leftSegment.Fingerprint(), rightSegment.Fingerprint())
	}
}

func canonicalizeCapturedRequest(t *testing.T, data []byte) codec.CanonicalJSON {
	t.Helper()
	var value any
	if err := codec.Unmarshal(data, &value); err != nil {
		t.Fatalf("decode captured request: %v", err)
	}
	canonical, err := codec.MarshalCanonical(value, contextplan.MaxSegmentBytes)
	if err != nil {
		t.Fatalf("canonicalize captured request: %v", err)
	}
	return canonical
}

func assertCatalogFilesExclude(t *testing.T, snapshotPath string, forbidden []string) {
	t.Helper()
	for _, path := range []string{snapshotPath, snapshotPath + ".lock"} {
		content := readAppTestFile(t, path)
		for _, value := range forbidden {
			if value != "" && bytes.Contains(content, []byte(value)) {
				t.Fatalf("catalog file %s contains forbidden value %q", filepath.Base(path), value)
			}
		}
	}
}

func privateAppTempDir(t *testing.T) string {
	t.Helper()
	directory := t.TempDir()
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(directory)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

func TestHeadlessJSONBrokenPipeCancelsAndReleasesLease(t *testing.T) {
	requestStarted := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(writer, "data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp-partial\"}}\n\n")
		_, _ = io.WriteString(writer, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"partial\"}\n\n")
		if flusher, ok := writer.(http.Flusher); ok {
			flusher.Flush()
		}
		close(requestStarted)
		<-request.Context().Done()
	}))
	defer server.Close()

	dataRoot := filepath.Join(t.TempDir(), "sessions")
	configPath := writeAppConfig(t, t.TempDir(), domain.ProviderOpenAI, server.URL)
	writer := &failOnHeadlessWrite{failCall: 3}
	outcome := Run(context.Background(), Options{
		Mode: headless.ModeJSON, Prompt: "hello", ConfigPath: configPath,
		SessionDataRoot: dataRoot, Output: writer,
	})
	<-requestStarted
	if outcome.ExitCode() != 1 || outcome.Report != ReportOutputUnavailable || outcome.Failure.Code != fault.CodeOutput {
		t.Fatalf("broken pipe outcome = %#v", outcome)
	}
	started := decodeThreadStarted(t, writer.buffer.String())
	loaded := loadHeadlessJournal(t, dataRoot, started.ThreadID)
	if len(loaded.Records) != 6 || loaded.Records[len(loaded.Records)-1].EventKind != session.EventTurnFailed {
		t.Fatalf("broken pipe records = %#v", loaded.Records)
	}
	for _, record := range loaded.Records {
		if record.EventKind == session.EventSampleUsage || record.EventKind == session.EventTurnCompleted {
			t.Fatalf("broken pipe committed success: %#v", record)
		}
	}
}

func TestStreamJSONBrokenOutputCancelsAgentLoopAndReleasesLease(t *testing.T) {
	requestStarted := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(writer, "data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp-partial\"}}\n\n")
		_, _ = io.WriteString(writer, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"partial\"}\n\n")
		if flusher, ok := writer.(http.Flusher); ok {
			flusher.Flush()
		}
		close(requestStarted)
		<-request.Context().Done()
	}))
	defer server.Close()

	dataRoot := filepath.Join(privateAppTempDir(t), "sessions")
	output := &failOnHeadlessWrite{failCall: 4}
	input := io.NopCloser(strings.NewReader(
		`{"version":1,"type":"input.submit","request_id":"broken-output-input","text":"hello"}` + "\n",
	))
	outcome := Run(context.Background(), Options{
		Mode:            headless.ModeStreamJSON,
		ConfigPath:      writeAppConfig(t, t.TempDir(), domain.ProviderOpenAI, server.URL),
		SessionDataRoot: dataRoot, Input: input, Output: output,
	})
	<-requestStarted
	if outcome.ExitCode() != 1 || outcome.Report != ReportOutputUnavailable || outcome.Failure.Code != fault.CodeOutput {
		t.Fatalf("broken stream output outcome = %#v", outcome)
	}
	started := decodeThreadStarted(t, output.buffer.String())
	loaded := loadHeadlessJournal(t, dataRoot, started.ThreadID)
	if loaded.Records[len(loaded.Records)-1].EventKind != session.EventTurnFailed {
		t.Fatalf("broken stream output records = %#v", loaded.Records)
	}
	for _, record := range loaded.Records {
		if record.EventKind == session.EventSampleUsage || record.EventKind == session.EventTurnCompleted {
			t.Fatalf("broken stream output committed success: %#v", record)
		}
	}
}

func TestHeadlessCancellationWaitsForDurableFailureAndReleasesLease(t *testing.T) {
	requestStarted := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		if flusher, ok := writer.(http.Flusher); ok {
			flusher.Flush()
		}
		close(requestStarted)
		<-request.Context().Done()
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	dataRoot := filepath.Join(t.TempDir(), "sessions")
	configPath := writeAppConfig(t, t.TempDir(), domain.ProviderOpenAI, server.URL)
	var output bytes.Buffer
	result := make(chan Outcome, 1)
	go func() {
		result <- Run(ctx, Options{
			Mode: headless.ModeJSON, Prompt: "cancel me",
			ConfigPath:      configPath,
			SessionDataRoot: dataRoot, Output: &output,
		})
	}()
	<-requestStarted
	cancel()
	outcome := <-result
	if outcome.ExitCode() != 1 || outcome.Report != ReportComplete ||
		outcome.Failure.Code != fault.CodeUserCancelled || !outcome.Failure.Cancelled {
		t.Fatalf("cancel outcome = %#v", outcome)
	}
	if strings.Count(output.String(), `"type":"turn.failed"`) != 1 ||
		!strings.Contains(output.String(), `"code":"user_cancelled"`) ||
		!strings.Contains(output.String(), `"cancelled":true`) {
		t.Fatalf("cancel JSONL = %s", output.String())
	}
	started := decodeThreadStarted(t, output.String())
	loaded := loadHeadlessJournal(t, dataRoot, started.ThreadID)
	if loaded.Records[len(loaded.Records)-1].EventKind != session.EventTurnFailed {
		t.Fatalf("cancel records = %#v", loaded.Records)
	}
}

func decodeThreadStarted(t *testing.T, output string) headless.ThreadStartedEvent {
	t.Helper()
	line, _, _ := strings.Cut(output, "\n")
	var event headless.ThreadStartedEvent
	if err := codec.UnmarshalStrict([]byte(line), &event); err != nil {
		t.Fatalf("decode thread.started from %q: %v", output, err)
	}
	if event.Version != 1 || event.Type != "thread.started" {
		t.Fatalf("first event = %#v", event)
	}
	return event
}

func loadHeadlessJournal(t *testing.T, dataRoot string, threadID domain.ThreadID) session.LoadResult {
	t.Helper()
	repository, err := session.OpenOrCreateRepository(dataRoot)
	if err != nil {
		t.Fatal(err)
	}
	lease, err := repository.Open(context.Background(), threadID)
	if err != nil {
		t.Fatalf("open released headless lease: %v", err)
	}
	loaded, err := session.NewLoader().Load(context.Background(), lease)
	if err != nil {
		t.Fatal(err)
	}
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}
	if err := repository.Close(); err != nil {
		t.Fatal(err)
	}
	return loaded
}

type failOnHeadlessWrite struct {
	buffer   bytes.Buffer
	calls    int
	failCall int
}

func (writer *failOnHeadlessWrite) Write(content []byte) (int, error) {
	writer.calls++
	if writer.calls == writer.failCall {
		return 0, errors.New("fixture broken pipe secret")
	}
	return writer.buffer.Write(content)
}

func (*failOnHeadlessWrite) Close() error { return nil }
