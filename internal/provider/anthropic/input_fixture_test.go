package anthropic

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
func testInputEntry(t *testing.T, user nativeMessage) nativeHistoryEntry {
	t.Helper()
	id := testInputID(projectMessageText(user, roleUser))
	a, err := domain.NewInputAssociation(domain.TurnID(id), domain.InputInitial, 0, []domain.InputID{id})
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := provider.NewInputMetadata(a)
	if err != nil {
		t.Fatal(err)
	}
	return nativeHistoryEntry{Kind: nativeHistoryUserInputs, TurnID: a.TurnID(), InputMetadata: &metadata, UserInputs: []nativeMessage{user}}
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
		history.commit(entry)
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
				for _, item := range entry.Assistant.Content {
					if item.Type != blockTypeToolUse {
						continue
					}
					invocation := testNativeInvocation(t, item, tool.InvocationID(testInputID("question/"+item.ID)), tool.ProviderCallID(item.ID))
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
		case nativeHistoryHookFeedback:
			feedback, feedbackErr := entryStopFeedback(entry)
			if feedbackErr != nil {
				t.Fatal(feedbackErr)
			}
			record, err = provider.NewStopFeedbackRestoreRecord(feedback, envelope)
		default:
			t.Fatalf("unsupported restore fixture kind: %s", entry.Kind)
		}
		if err != nil {
			t.Fatal(err)
		}
		records = append(records, record)
	}
	return records
}

func testConsumeStream(t *testing.T, conversation *Conversation, ctx context.Context, cancel context.CancelFunc, user *nativeMessage, catalog tool.CatalogSnapshot, stream <-chan transport.SSEMessage, output chan<- provider.StreamEvent) {
	t.Helper()
	if user != nil {
		conversation.history.commit(testInputEntry(t, *user))
	}
	if conversation.provider == nil {
		conversation.provider = &Provider{config: Config{Model: "fixture-model", CachePolicy: domain.DefaultCachePolicy(), Inference: testInference(t, 4096)}}
	}
	input, err := testSamplingInput(t, conversation.provider.config.Inference).WithToolCatalog(catalog)
	if err != nil {
		t.Fatal(err)
	}
	request, err := conversation.compileSamplingRequest(conversation.history.snapshot(), input)
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := conversation.requestEvidence(input)
	if err != nil {
		t.Fatal(err)
	}
	conversation.sampleStream(catalog, stream, request, evidence).Consume(ctx, cancel, &conversation.active, output, samplingErrors())
}

func testSampleRestoreSequence(t *testing.T, envelopes ...provider.NativeCommitEnvelope) []provider.RestoreRecord {
	t.Helper()
	entry := testInputEntry(t, newUserMessage("first"))
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
	plan, err := conversation.planSamplingRequest(conversation.history.snapshot(), input)
	if err != nil {
		return provider.RequestEvidence{}, err
	}
	return plan.evidence()
}

func (conversation *Conversation) historySnapshot() []nativeHistoryEntry {
	return conversation.history.snapshot()
}

// compileSamplingRequest 用生产请求计划生成用于比较的字节。
func (conversation *Conversation) compileSamplingRequest(history []nativeHistoryEntry, input provider.TurnInput) (codec.CanonicalJSON, error) {
	plan, err := conversation.planSamplingRequest(history, input)
	if err != nil {
		return codec.CanonicalJSON{}, err
	}
	return plan.body(conversation.provider.config.CachePolicy.Anthropic())
}

func compileMessagesRequest(
	model string,
	maxTokens int,
	history []nativeHistoryEntry,
	projectInstructions *domain.ProjectInstructionsSnapshot,
	toolView tool.FacadeView,
) (codec.CanonicalJSON, error) {
	if projectInstructions != nil {
		if err := projectInstructions.Validate(); err != nil {
			return codec.CanonicalJSON{}, fmt.Errorf("project instructions snapshot is invalid: %w", err)
		}
	}
	if toolView.Family() != domain.ProviderAnthropic || len(toolView.Facades()) != 10 {
		return codec.CanonicalJSON{}, fmt.Errorf("anthropic tool catalog view is invalid")
	}
	return codec.MarshalCanonical(
		buildMessagesRequest(model, maxTokens, history, projectInstructions, toolView),
		maxMessagesRequestBytes,
	)
}
