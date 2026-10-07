package app

import (
	"bytes"
	"context"
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
	"time"

	"easycode/internal/codec"
	"easycode/internal/config"
	contextplan "easycode/internal/context"
	"easycode/internal/domain"
	"easycode/internal/fault"
	"easycode/internal/headless"
	"easycode/internal/secret"
	"easycode/internal/session"
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
			var mu sync.Mutex
			var requests [][]byte
			logicalTurns := []int{1, 2, 3, 1, 2, 3}
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				body, err := io.ReadAll(request.Body)
				if err != nil {
					t.Errorf("read request: %v", err)
					return
				}
				mu.Lock()
				requestIndex := len(requests)
				requests = append(requests, append([]byte(nil), body...))
				mu.Unlock()
				writer.Header().Set("Content-Type", "text/event-stream")
				fixture.serveTurn(writer, logicalTurns[requestIndex])
			}))
			defer server.Close()

			providerConfig := config.Config{Provider: config.Provider{
				Family: fixture.family, BaseURL: server.URL,
				APIKey: secret.New("headless-e2e-secret"), Model: fixture.model,
			}}
			live, err := openChatResources(
				context.Background(), providerConfig, filepath.Join(t.TempDir(), "live"), "", startupCWD,
			)
			if err != nil {
				t.Fatal(err)
			}
			submitAppTurn(t, live, "first")
			submitAppTurn(t, live, "second")
			submitAppTurn(t, live, "third")
			if err := live.close(context.Background()); err != nil {
				t.Fatal(err)
			}

			dataHome := privateAppTempDir(t)
			dataRoot := filepath.Join(dataHome, "sessions")
			catalogPath := filepath.Join(dataHome, "state.sqlite")
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
			if err := os.WriteFile(catalogPath, corruptCatalog, 0o600); err != nil {
				t.Fatal(err)
			}
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

			var textOutput bytes.Buffer
			third := Run(context.Background(), Options{
				Mode: headless.ModeText, Prompt: "third", ConfigPath: configPath,
				ContinueSession: true, SessionDataRoot: dataRoot, SessionCatalogPath: catalogPath,
				Output: &textOutput,
			})
			if third.ExitCode() != 0 || textOutput.String() != "answer-3\n" {
				t.Fatalf("third outcome/output = %#v/%q", third, textOutput.String())
			}

			mu.Lock()
			captured := make([][]byte, len(requests))
			for index := range requests {
				captured[index] = append([]byte(nil), requests[index]...)
			}
			mu.Unlock()
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
			if len(loaded.Records) != 14 || loaded.NextSequence != 15 {
				t.Fatalf("records/next = %d/%d", len(loaded.Records), loaded.NextSequence)
			}
			for index, record := range loaded.Records {
				if record.Sequence != uint64(index+1) || strings.Contains(string(record.EventKind), ".") {
					t.Fatalf("record[%d] = %#v", index, record)
				}
			}
			for _, index := range []int{4, 8, 12} {
				if loaded.Records[index].EventKind != session.EventSampleUsage {
					t.Fatalf("record[%d] kind = %s", index, loaded.Records[index].EventKind)
				}
			}
			for _, forbidden := range []string{
				catalogPath, dataRoot, string(started.SessionID), string(started.ThreadID),
				loaded.Records[len(loaded.Records)-1].Timestamp.Format(time.RFC3339Nano),
			} {
				if strings.Contains(string(captured[5]), forbidden) {
					t.Fatalf("continue request contains catalog/session metadata %q", forbidden)
				}
			}
			assertCatalogFilesExclude(t, dataHome, []string{
				"file-secret", server.URL, "first", "second", "third",
				"answer-1", "answer-2", "answer-3", "Authorization",
			})
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
			var mu sync.Mutex
			var requests [][]byte
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				body, err := io.ReadAll(request.Body)
				if err != nil {
					t.Errorf("read request: %v", err)
					return
				}
				mu.Lock()
				index := len(requests)
				requests = append(requests, append([]byte(nil), body...))
				mu.Unlock()
				writer.Header().Set("Content-Type", "text/event-stream")
				fixture.serveTurn(writer, index%2+1)
			}))
			defer server.Close()

			dataRoot := filepath.Join(privateAppTempDir(t), "sessions")
			configPath := writeAppConfig(t, t.TempDir(), fixture.family, server.URL)
			input := io.NopCloser(strings.NewReader(
				`{"version":1,"type":"input.submit","request_id":"stream-input-1","text":"first"}` + "\n" +
					`{"version":1,"type":"input.submit","request_id":"stream-input-2","text":"second"}` + "\n",
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
			if len(loaded.Records) != 10 || loaded.NextSequence != 11 {
				t.Fatalf("stream records/next = %d/%d", len(loaded.Records), loaded.NextSequence)
			}

			providerConfig := config.Config{Provider: config.Provider{
				Family: fixture.family, BaseURL: server.URL,
				APIKey: secret.New("stream-e2e-secret"), Model: fixture.model,
			}}
			direct, err := openChatResources(
				context.Background(), providerConfig, filepath.Join(privateAppTempDir(t), "direct"), "", startupCWD,
			)
			if err != nil {
				t.Fatal(err)
			}
			submitAppTurn(t, direct, "first")
			submitAppTurn(t, direct, "second")
			if err := direct.close(context.Background()); err != nil {
				t.Fatal(err)
			}

			mu.Lock()
			captured := append([][]byte(nil), requests...)
			mu.Unlock()
			if len(captured) != 4 {
				t.Fatalf("request count = %d", len(captured))
			}
			if !bytes.Equal(captured[0], captured[2]) || !bytes.Equal(captured[1], captured[3]) {
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

func TestStreamJSONResumeAndContinue(t *testing.T) {
	var turn atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		writeOpenAIAppTurn(writer, int(turn.Add(1)))
	}))
	defer server.Close()
	dataHome := privateAppTempDir(t)
	dataRoot := filepath.Join(dataHome, "sessions")
	catalogPath := filepath.Join(dataHome, "state.sqlite")
	configPath := writeAppConfig(t, t.TempDir(), domain.ProviderOpenAI, server.URL)

	firstOutput := runStreamAppInput(t, Options{
		Mode: headless.ModeStreamJSON, ConfigPath: configPath,
		SessionDataRoot: dataRoot, SessionCatalogPath: catalogPath,
	}, "resume-first")
	started := decodeThreadStarted(t, firstOutput)
	if started.Resumed {
		t.Fatalf("new stream started as resumed: %#v", started)
	}
	resumedOutput := runStreamAppInput(t, Options{
		Mode: headless.ModeStreamJSON, ConfigPath: configPath,
		ResumeThreadID:  string(started.ThreadID),
		SessionDataRoot: dataRoot, SessionCatalogPath: catalogPath,
	}, "resume-second")
	resumed := decodeThreadStarted(t, resumedOutput)
	if !resumed.Resumed || resumed.ThreadID != started.ThreadID {
		t.Fatalf("resumed thread.started = %#v", resumed)
	}
	continuedOutput := runStreamAppInput(t, Options{
		Mode: headless.ModeStreamJSON, ConfigPath: configPath, ContinueSession: true,
		SessionDataRoot: dataRoot, SessionCatalogPath: catalogPath,
	}, "continue-third")
	continued := decodeThreadStarted(t, continuedOutput)
	if !continued.Resumed || continued.ThreadID != started.ThreadID {
		t.Fatalf("continued thread.started = %#v", continued)
	}
	if turn.Load() != 3 {
		t.Fatalf("Provider turn count = %d", turn.Load())
	}
}

func TestStreamJSONQueueFullAndShutdownAreReported(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		if flusher, ok := writer.(http.Flusher); ok {
			flusher.Flush()
		}
		<-request.Context().Done()
	}))
	defer server.Close()
	var commands strings.Builder
	for index := 0; index < 66; index++ {
		_, _ = fmt.Fprintf(
			&commands,
			"{\"version\":1,\"type\":\"input.submit\",\"request_id\":\"queue-%02d\",\"text\":\"input-%02d\"}\n",
			index, index,
		)
	}
	commands.WriteString(`{"version":1,"type":"session.shutdown","request_id":"queue-shutdown"}` + "\n")
	output := &appWriteCloser{}
	outcome := Run(context.Background(), Options{
		Mode:            headless.ModeStreamJSON,
		ConfigPath:      writeAppConfig(t, t.TempDir(), domain.ProviderOpenAI, server.URL),
		SessionDataRoot: filepath.Join(privateAppTempDir(t), "sessions"),
		Input:           io.NopCloser(strings.NewReader(commands.String())), Output: output,
	})
	if outcome.ExitCode() != 0 {
		t.Fatalf("queue stream outcome = %#v output=%s", outcome, output.String())
	}
	if !strings.Contains(output.String(), `"code":"input_queue_full"`) ||
		!strings.Contains(output.String(), `"request_id":"queue-shutdown"`) ||
		strings.Count(output.String(), `"type":"input.discarded"`) != 64 {
		t.Fatalf("queue full/shutdown output = %s", output.String())
	}
}

func runStreamAppInput(t *testing.T, options Options, text string) string {
	t.Helper()
	options.Input = io.NopCloser(strings.NewReader(
		`{"version":1,"type":"input.submit","request_id":"` + text + `","text":"` + text + `"}` + "\n",
	))
	output := &appWriteCloser{}
	options.Output = output
	outcome := Run(context.Background(), options)
	if outcome.ExitCode() != 0 {
		t.Fatalf("stream Run(%s) outcome = %#v output=%s", text, outcome, output.String())
	}
	return output.String()
}

func TestStreamJSONFailedInputDoesNotEnterNextProviderHistory(t *testing.T) {
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
			var mu sync.Mutex
			var requests [][]byte
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				body, _ := io.ReadAll(request.Body)
				mu.Lock()
				index := len(requests)
				requests = append(requests, append([]byte(nil), body...))
				mu.Unlock()
				if index == 0 {
					http.Error(writer, "provider-private-body", http.StatusBadGateway)
					return
				}
				writer.Header().Set("Content-Type", "text/event-stream")
				fixture.serveTurn(writer, 1)
			}))
			defer server.Close()

			input := io.NopCloser(strings.NewReader(
				`{"version":1,"type":"input.submit","request_id":"failed-input","text":"first-must-not-commit"}` + "\n" +
					`{"version":1,"type":"input.submit","request_id":"success-input","text":"second"}` + "\n",
			))
			output := &appWriteCloser{}
			outcome := Run(context.Background(), Options{
				Mode:            headless.ModeStreamJSON,
				ConfigPath:      writeAppConfig(t, t.TempDir(), fixture.family, server.URL),
				SessionDataRoot: filepath.Join(privateAppTempDir(t), "sessions"),
				Input:           input, Output: output,
			})
			if outcome.ExitCode() != 0 {
				t.Fatalf("stream outcome = %#v, output=%s", outcome, output.String())
			}
			if strings.Count(output.String(), `"type":"turn.failed"`) != 1 ||
				strings.Count(output.String(), `"type":"turn.completed"`) != 1 ||
				strings.Contains(output.String(), "provider-private-body") {
				t.Fatalf("stream failure output = %s", output.String())
			}
			mu.Lock()
			captured := append([][]byte(nil), requests...)
			mu.Unlock()
			if len(captured) != 2 || bytes.Contains(captured[1], []byte("first-must-not-commit")) ||
				!bytes.Contains(captured[1], []byte("second")) {
				t.Fatalf("failed input entered next history: %#v", captured)
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

func assertCatalogFilesExclude(t *testing.T, dataHome string, forbidden []string) {
	t.Helper()
	entries, err := os.ReadDir(dataHome)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), "state.sqlite") {
			continue
		}
		content, err := os.ReadFile(filepath.Join(dataHome, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		for _, value := range forbidden {
			if value != "" && bytes.Contains(content, []byte(value)) {
				t.Fatalf("catalog file %s contains forbidden value %q", entry.Name(), value)
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
	if len(loaded.Records) != 4 || loaded.Records[len(loaded.Records)-1].EventKind != session.EventTurnFailed {
		t.Fatalf("broken pipe records = %#v", loaded.Records)
	}
	for _, record := range loaded.Records {
		if record.EventKind == session.EventProviderNativeCommit || record.EventKind == session.EventTurnCompleted {
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
		if record.EventKind == session.EventProviderNativeCommit || record.EventKind == session.EventTurnCompleted {
			t.Fatalf("broken stream output committed success: %#v", record)
		}
	}
}

func TestHeadlessProviderFailureDoesNotPublishPartialText(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		http.Error(writer, "provider-private-body", http.StatusBadGateway)
	}))
	defer server.Close()
	var output bytes.Buffer
	outcome := Run(context.Background(), Options{
		Mode: headless.ModeText, Prompt: "hello",
		ConfigPath:      writeAppConfig(t, t.TempDir(), domain.ProviderOpenAI, server.URL),
		SessionDataRoot: filepath.Join(t.TempDir(), "sessions"), Output: &output,
	})
	if outcome.ExitCode() != 1 || output.Len() != 0 || outcome.Failure.Code != fault.CodeProviderRequest {
		t.Fatalf("provider failure outcome/output = %#v/%q", outcome, output.String())
	}
	if strings.Contains(outcome.Failure.Message, "provider-private-body") {
		t.Fatalf("provider failure leaked body: %#v", outcome)
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
