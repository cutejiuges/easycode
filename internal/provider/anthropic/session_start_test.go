package anthropic

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"testing"

	"easycode/internal/codec"
	"easycode/internal/domain"
	"easycode/internal/provider"
	"easycode/internal/secret"
	"easycode/internal/tool"
	"encoding/json"
)

func TestSessionStartNativeContextRetainsToolContinuationAndCache(t *testing.T) {
	requests := make(chan []byte, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		select {
		case requests <- body:
		default:
			t.Error("unexpected duplicate request")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		writeSuccessfulAnthropicTurn(w)
	}))
	defer server.Close()
	policy, _ := domain.NewCachePolicy(domain.AnthropicCache5m, domain.OpenAICacheOmit, domain.OpenAIHTTPSSE)
	instance, err := New(Config{CachePolicy: policy, BaseURL: server.URL, APIKey: secret.New("fixture-key"), Model: "model"})
	if err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	c := testConversation(t, instance)
	input, _ := domain.NewUserInput("01900000-0000-7000-8000-000000000002", "real initial input")
	association, _ := domain.NewInputAssociation(stopTestTurn, domain.InputInitial, 0, []domain.InputID{input.ID()})
	startup := sessionStartTestContext(t, domain.SessionStartTarget{Source: domain.SessionStartStartup, TurnID: stopTestTurn}, "frozen startup context")
	start, err := c.PrepareSessionStartInputs(startup, association, []domain.UserInput{input})
	if err != nil {
		t.Fatal(err)
	}
	startEnvelope, _ := start.Envelope()
	inputEnvelope, _ := start.InputEnvelope()
	startRecord, err := provider.NewSessionStartContextRestoreRecord(startup.Target, []domain.SessionStartProof{startup.Items[0].Proof()}, startEnvelope)
	if err != nil {
		t.Fatal(err)
	}
	if err := start.Finalize(); err != nil {
		t.Fatal(err)
	}
	sampling, _ := testSamplingInput(t, instance.config.Inference).WithToolCatalog(testAnthropicToolCatalog(t))
	initialRequest, err := c.compileSamplingRequest(c.history.Entries(), sampling)
	if err != nil {
		t.Fatal(err)
	}
	requestBody := initialRequest.Bytes()
	contextIndex := bytes.Index(requestBody, []byte("frozen startup context"))
	inputIndex := bytes.Index(requestBody, []byte("real initial input"))
	if contextIndex < 0 || contextIndex >= inputIndex {
		t.Fatal("startup ordering differs")
	}

	execInput, _ := tool.NewExecInput("printf ok", "src", 1000)
	ready, _ := tool.NewExecReadyCall("call-exec", execInput)
	invocation, _ := tool.NewExecInvocation(tool.InvocationID("01900000-0000-7000-8000-000000000008"), ready)
	resultText, _ := tool.NewModelPreview("old tool result")
	stats, _ := tool.NewExecResultMetadata("src", 0, true, 0, tool.ExecStreamStats{}, tool.ExecStreamStats{}, false, false, false)
	result, err := tool.NewExecInvocationResult(invocation, tool.ResultSuccess, "ok", resultText, stats)
	if err != nil {
		t.Fatal(err)
	}
	sample := stopTestSample(stopTestTurn, "old sample")
	sample.Assistant.Content = append(sample.Assistant.Content, NativeItem{Type: blockTypeToolUse, ID: "call-exec", Name: "Bash", Input: json.RawMessage(`{"command":"printf ok","workdir":"src","timeout_ms":1000}`)})
	c.history.Append(sample)
	toolEvidence := testModelOutputs(t, c.history.Entries(), result)
	output, err := prepareToolOutputsEntry(c.history.Entries(), toolEvidence)
	if err != nil {
		t.Fatal(err)
	}
	c.history.Append(output)
	sampleEnvelope, _ := encodeNativeCommit(sample)
	outputEnvelope, _ := encodeNativeCommit(output)
	records := append([]provider.RestoreRecord{startRecord}, testRestoreRecords(t, []provider.NativeCommitEnvelope{inputEnvelope, sampleEnvelope, outputEnvelope}, toolEvidence)...)

	scope, _ := domain.NewAutoCompactionScope(stopTestTurn, 1)
	source, err := nativeCompactionSource(c.history.Snapshot(), "model")
	if err != nil {
		t.Fatal(err)
	}
	id := domain.CompactionID("01900000-0000-7000-8000-000000000003")
	generation := stopTestSample(stopTestTurn, "fresh checkpoint summary")
	generation.Metadata.StopReason = optionalString{Known: true, Value: "end_turn"}
	checkpoint, err := c.prepareCheckpoint(scope, id, source, generation, syntheticRequestReceipt(domain.RequestSummary), domain.NewNoTaskProgress())
	if err != nil {
		t.Fatal(err)
	}
	checkpointEnvelope, _ := checkpoint.Checkpoint()
	checkpointRecord, _ := provider.NewCheckpointRestoreRecord(checkpointEnvelope, domain.NewNoTaskProgress())
	if err := checkpoint.Finalize(); err != nil {
		t.Fatal(err)
	}
	value := sessionStartTestContext(t, domain.SessionStartTarget{Source: domain.SessionStartCompact, CompactionID: id, Scope: scope,
		CheckpointRevision: source.ContextRevision() + 1, CheckpointSequence: 10, CheckpointDigest: source.Digest()}, "fixed compact supplement")
	compact, err := c.PrepareCompactSessionStartContext(value, provider.SessionStartContinuation{})
	if err != nil {
		t.Fatal(err)
	}
	compactEnvelope, _ := compact.Envelope()
	compactRecord, err := provider.NewSessionStartContextRestoreRecord(value.Target, []domain.SessionStartProof{value.Items[0].Proof()}, compactEnvelope)
	if err != nil {
		t.Fatal(err)
	}
	if err := compact.Finalize(); err != nil {
		t.Fatal(err)
	}
	if _, err := nativeCompactionSource(c.history.Snapshot(), "model"); err == nil {
		t.Fatal("a supplement alone enabled another compaction")
	}
	records = append(records, checkpointRecord, compactRecord)
	recovered, _, err := instance.RestoreConversation(context.Background(), c.threadID, records)
	if err != nil {
		t.Fatal(err)
	}
	restored := recovered.(*Conversation)
	live, err := c.compileSamplingRequest(c.history.Entries(), sampling)
	if err != nil {
		t.Fatal(err)
	}
	again, err := restored.compileSamplingRequest(restored.history.Entries(), sampling)
	if err != nil || !bytes.Equal(live.Bytes(), again.Bytes()) {
		t.Fatal("restored canonical input differs", err)
	}
	plan, _ := restored.planSamplingRequest(restored.history.Snapshot(), sampling)
	evidence, _ := plan.evidence()
	livePlan, _ := c.planSamplingRequest(c.history.Snapshot(), sampling)
	liveEvidence, _ := livePlan.evidence()
	if evidence.NativeFingerprint() == "" || evidence.NativeFingerprint() != liveEvidence.NativeFingerprint() {
		t.Fatal("restored native fingerprint differs")
	}
	window, footprint, err := restored.ProjectContextWindow()
	if err != nil || footprint.Revision() != 6 || len(window.History().CheckpointContexts) != 1 || !reflect.DeepEqual(restored.ProjectHistory(), c.ProjectHistory()) {
		t.Fatal("context footprint or history projection differs", err)
	}
	item := plan.request.Messages[1]
	itemBytes, _ := codec.MarshalStable(item)
	want, err := os.ReadFile("testdata/session_start_compact_input.golden.json")
	if err != nil || !bytes.Equal(itemBytes, bytes.TrimSpace(want)) ||
		len(plan.request.Messages) != 2 || !bytes.Contains(again.Bytes(), []byte("fresh checkpoint summary")) || bytes.Contains(again.Bytes(), []byte("frozen startup context")) {
		t.Fatalf("checkpoint and compact native golden mismatch: %s (%v)", itemBytes, err)
	}
	wrong := value.Items[0].Proof()
	wrong.Digest = domain.HookDigest([]byte("wrong body"))
	forged, _ := provider.NewSessionStartContextRestoreRecord(value.Target, []domain.SessionStartProof{wrong}, compactEnvelope)
	records[len(records)-1] = forged
	if _, _, err := instance.RestoreConversation(context.Background(), c.threadID, records); err == nil {
		t.Fatal("restored a context with different disclosure evidence")
	}

	stream, err := restored.Stream(context.Background(), sampling)
	if err != nil {
		t.Fatal(err)
	}
	terminal := readAnthropicTerminal(t, restored, stream)
	if terminal.Kind() != provider.StreamEventCompleted {
		t.Fatal("closed tool group could not continue after the compact insert", terminal.Error())
	}
	receipt, err := terminal.PreparedSample().Receipt()
	if err != nil || receipt.NativeFingerprint != evidence.NativeFingerprint() || receipt.Markers == 0 {
		t.Fatal("received request fingerprint differs from the verified compact request", err)
	}
	if err := terminal.PreparedSample().Finalize(); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(<-requests, live.Bytes()) {
		t.Fatal("wire continuation differs from uninterrupted history")
	}
	actual, err := restored.ProjectAssistantSample(domain.AssistantSampleReference{TurnID: stopTestTurn, SampleIndex: 1})
	if err != nil || actual.Text() != "answer" {
		t.Fatal("continuation lost the real turn or sample index", err, actual)
	}
}

func sessionStartTestContext(t *testing.T, target domain.SessionStartTarget, body string) domain.SessionStartContext {
	t.Helper()
	scope, _ := domain.ParseSessionStartHookScope("*")
	declaration, err := domain.NewSessionStartHookDeclaration(domain.HookSourceProject, domain.HookFeedbackModel, domain.HookDigest(nil), "session-context", scope, "true", 5)
	if err != nil {
		t.Fatal(err)
	}
	text, err := domain.WrapSessionStartContext(target.Source, body)
	if err != nil {
		t.Fatal(err)
	}
	return domain.SessionStartContext{Target: target, Items: []domain.SessionStartContextItem{{Reference: declaration.Reference, Text: text}}}
}
