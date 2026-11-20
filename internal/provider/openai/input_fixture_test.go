package openai

import (
	"context"
	"crypto/sha256"
	"easycode/internal/codec"
	"easycode/internal/domain"
	"easycode/internal/provider"
	"easycode/internal/provider/transport"
	"easycode/internal/tool"
	"fmt"
	"testing"
)

func testInputID(text string) domain.InputID {
	sum := sha256.Sum256([]byte(text))
	return domain.InputID(fmt.Sprintf("01900000-0000-7000-8000-%x", sum[:6]))
}
func testInputEntry(t *testing.T, user NativeItem) nativeHistoryEntry {
	t.Helper()
	id := testInputID(projectItemText(user, "user", "input_text"))
	a, err := domain.NewInputAssociation(domain.TurnID(id), domain.InputInitial, 0, []domain.InputID{id})
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := provider.NewInputMetadata(a)
	if err != nil {
		t.Fatal(err)
	}
	return nativeHistoryEntry{Kind: nativeHistoryUserInputs, TurnID: a.TurnID(), InputMetadata: &metadata, UserInputs: []NativeItem{user}}
}
func testAssociatedHistory(entries []nativeHistoryEntry) []nativeHistoryEntry {
	var turnID domain.TurnID
	for i := range entries {
		if entries[i].Kind == nativeHistoryUserInputs {
			turnID = entries[i].TurnID
		}
		entries[i].TurnID = turnID
	}
	return entries
}
func testCommitHistory(t *testing.T, history *nativeHistory, entries ...nativeHistoryEntry) {
	t.Helper()
	entries = testAssociatedHistory(entries)
	for _, entry := range entries {
		history.Append(entry)
	}
}

func testCommitUserInput(t *testing.T, conversation *Conversation, text string) {
	t.Helper()
	id := testInputID(text)
	user, err := domain.NewUserInput(id, text)
	if err != nil {
		t.Fatal(err)
	}
	a, err := domain.NewInputAssociation(domain.TurnID(id), domain.InputInitial, 0, []domain.InputID{id})
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := conversation.PrepareUserInputs(a, []domain.UserInput{user})
	if err != nil {
		t.Fatal(err)
	}
	if err := prepared.Finalize(); err != nil {
		t.Fatal(err)
	}
}
func testRestoreRecords(t *testing.T, envelopes []provider.NativeCommitEnvelope, outputEvidence ...[]tool.ModelOutput) []provider.RestoreRecord {
	t.Helper()
	var turnID domain.TurnID
	outputIndex := 0
	records := make([]provider.RestoreRecord, 0, len(envelopes))
	for envelopeIndex, envelope := range envelopes {
		entry, err := decodeNativeCommit(envelope)
		if err != nil {
			t.Fatal(err)
		}
		var record provider.RestoreRecord
		switch entry.Kind {
		case nativeHistoryUserInputs:
			a, e := entry.InputMetadata.Association()
			if e != nil {
				t.Fatal(e)
			}
			turnID = a.TurnID()
			record, err = provider.NewInputRestoreRecord(a, envelope)
		case nativeHistorySample:
			record, err = provider.NewSampleRestoreRecord(turnID, envelope)
			if err == nil {
				var questions []tool.QuestionInvocation
				var tasks []tool.TaskInvocation
				var calls []tool.Invocation
				for _, item := range entry.Outputs {
					if item.Type != "function_call" && item.Type != "custom_tool_call" {
						continue
					}
					invocation := testNativeInvocation(t, item, tool.InvocationID(testInputID("question/"+item.CallID)), tool.ProviderCallID(item.CallID))
					calls = append(calls, invocation)
					if q, ok := invocation.Question(); ok {
						questions = append(questions, q)
					}
					if task, ok := invocation.Task(); ok {
						tasks = append(tasks, task)
					}
				}
				results := testQuestionRestoreResults(t, envelopes[envelopeIndex+1:], questions)
				results = append(results, testTaskRestoreResults(t, envelopes[envelopeIndex+1:], tasks)...)
				record, err = record.WithReadyFacts(calls, results)
			}
		case nativeHistoryToolOutputs:
			if outputIndex >= len(outputEvidence) {
				t.Fatal("missing explicit frozen tool output test evidence")
			}
			record, err = provider.NewToolOutputRestoreRecord(turnID, envelope, outputEvidence[outputIndex])
			outputIndex++
		}
		if err != nil {
			t.Fatal(err)
		}
		records = append(records, record)
	}
	return records
}

func testConsumeStream(t *testing.T, conversation *Conversation, ctx context.Context, cancel context.CancelFunc, user *NativeItem, catalog tool.CatalogSnapshot, stream <-chan transport.SSEMessage) provider.StreamEvents {
	t.Helper()
	if user != nil {
		conversation.history.Append(testInputEntry(t, *user))
	}
	if conversation.provider == nil {
		settings, err := domain.DefaultInferenceSettings(domain.ProviderOpenAI)
		if err != nil {
			t.Fatal(err)
		}
		conversation.provider = &Provider{config: Config{Model: "fixture-model", CachePolicy: domain.DefaultCachePolicy(), Inference: settings}}
	}
	input, err := testSamplingInput(t, conversation.provider.config.Inference).WithToolCatalog(catalog)
	if err != nil {
		t.Fatal(err)
	}
	request, err := conversation.compileSamplingRequest(conversation.history.Entries(), input)
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := conversation.requestEvidence(input)
	if err != nil {
		t.Fatal(err)
	}
	entries := conversation.history.Snapshot().Entries
	if conversation.operations.Begin() != provider.ConversationIdle {
		t.Fatal("fixture could not acquire the conversation")
	}
	return conversation.sampleStream(entries[len(entries)-1].TurnID, catalog, stream, request, evidence, evidence.Compare(conversation.baseline), requestSelection{transport: domain.TransportHTTP, mode: domain.RequestFull, reason: domain.ResetNone, fallback: domain.FallbackNone}, func() {}).Consume(ctx, cancel, &conversation.operations, samplingErrors())
}

func testSampleRestoreSequence(t *testing.T, envelopes ...provider.NativeCommitEnvelope) []provider.RestoreRecord {
	t.Helper()
	entry := testInputEntry(t, NewUserItem("first"))
	envelope, err := encodeNativeCommit(entry)
	if err != nil {
		t.Fatal(err)
	}
	a, err := entry.InputMetadata.Association()
	if err != nil {
		t.Fatal(err)
	}
	input, err := provider.NewInputRestoreRecord(a, envelope)
	if err != nil {
		t.Fatal(err)
	}
	records := []provider.RestoreRecord{input}
	for _, envelope := range envelopes {
		record, err := provider.NewSampleRestoreRecord(a.TurnID(), envelope)
		if err != nil {
			t.Fatal(err)
		}
		records = append(records, record)
	}
	return records
}

func (conversation *Conversation) requestEvidence(input provider.TurnInput) (provider.RequestEvidence, error) {
	plan, err := conversation.planSamplingRequest(conversation.history.Snapshot(), input)
	if err != nil {
		return provider.RequestEvidence{}, err
	}
	return plan.evidence()
}

func (conversation *Conversation) historySnapshot() []nativeHistoryEntry {
	return conversation.history.Entries()
}

// compileSamplingRequest 用生产请求计划生成用于比较的字节。
func (conversation *Conversation) compileSamplingRequest(history []nativeHistoryEntry, input provider.TurnInput) (codec.CanonicalJSON, error) {
	window := conversation.history.Snapshot()
	window.Entries = history
	plan, err := conversation.planSamplingRequest(window, input)
	if err != nil {
		return codec.CanonicalJSON{}, err
	}
	return plan.body()
}

func compileResponsesRequest(
	model string,
	history []nativeHistoryEntry,
	projectInstructions *domain.ProjectInstructionsSnapshot,
	toolView tool.FacadeView,
) (codec.CanonicalJSON, error) {
	if projectInstructions != nil {
		if err := projectInstructions.Validate(); err != nil {
			return codec.CanonicalJSON{}, fmt.Errorf("project instructions snapshot is invalid: %w", err)
		}
	}
	if toolView.Family() != domain.ProviderOpenAI || len(toolView.Facades()) != 10 {
		return codec.CanonicalJSON{}, fmt.Errorf("OpenAI tool catalog view is invalid")
	}
	return codec.MarshalCanonical(
		buildResponsesRequest(model, history, projectInstructions, toolView),
		maxResponsesRequestBytes,
	)
}

// websocketBody 独立保留既有字段顺序，防止生产编码改变 WebSocket canonical 字节。
func websocketBody(request codec.CanonicalJSON, responseID string, offset int) (codec.CanonicalJSON, error) {
	var logical responsesRequest
	if err := codec.Unmarshal(request.Bytes(), &logical); err != nil {
		return codec.CanonicalJSON{}, err
	}
	if offset < 0 || offset > len(logical.Input) {
		return codec.CanonicalJSON{}, fmt.Errorf("incremental request offset is invalid")
	}
	expected := struct {
		Reasoning          *reasoningWire `json:"reasoning,omitempty"`
		Type               string         `json:"type"`
		PreviousResponseID string         `json:"previous_response_id,omitempty"`
		PromptCacheKey     string         `json:"prompt_cache_key,omitempty"`
		ToolChoice         string         `json:"tool_choice,omitempty"`
		MaxOutputTokens    uint64         `json:"max_output_tokens,omitempty"`
		Model              string         `json:"model"`
		Input              []NativeItem   `json:"input"`
		Tools              []responseTool `json:"tools"`
		Store              bool           `json:"store"`
		Include            []string       `json:"include"`
	}{
		Reasoning: logical.Reasoning, Type: "response.create", PreviousResponseID: responseID, PromptCacheKey: logical.PromptCacheKey,
		ToolChoice: logical.ToolChoice, MaxOutputTokens: logical.MaxOutputTokens, Model: logical.Model,
		Input: logical.Input[offset:], Tools: logical.Tools, Store: logical.Store, Include: logical.Include,
	}
	return codec.MarshalCanonical(expected, maxResponsesRequestBytes)
}
