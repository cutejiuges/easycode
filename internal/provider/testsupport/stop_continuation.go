package testsupport

import (
	"bytes"
	"reflect"
	"testing"

	"easycode/internal/codec"
	"easycode/internal/domain"
	"easycode/internal/provider"
)

// StopConversation 只组合续写契约实际调用的接口。
type StopConversation interface {
	RequestConversation
	provider.StopContinuationPreparer
	provider.StopFeedbackEncoder
	provider.TranscriptProjector
}

// CheckStopContinuation 共用准备、单次提交、恢复和来源拒绝，两家分别检查原生反馈角色。
func CheckStopContinuation(t *testing.T, conversation StopConversation, initial domain.InputAssociation, initialEnvelope, sampleEnvelope provider.NativeCommitEnvelope, sampling provider.TurnInput, steer bool, restore func([]provider.RestoreRecord) (StopConversation, error), decodeFeedback func(provider.NativeCommitEnvelope) (domain.StopFeedback, error)) {
	t.Helper()
	feedback := StopFeedback(t, 0)
	candidate, _ := domain.NewStopFeedbackOnly(feedback)
	var association domain.InputAssociation
	if steer {
		user, _ := domain.NewUserInput("01900000-0000-7000-8000-000000000003", "  用户纠正\n")
		association, _ = domain.NewInputAssociation(StopTurn, domain.InputSteering, 1, []domain.InputID{user.ID()})
		candidate, _ = domain.NewStopWithSteering(feedback, association, []domain.UserInput{user})
	}
	prepared, err := conversation.PrepareStopContinuation(candidate)
	if err != nil {
		t.Fatal(err)
	}
	if len(conversation.ProjectHistory().Turns[0].Messages) != 2 {
		t.Fatal("staged continuation visible")
	}
	if _, err := conversation.PrepareStopContinuation(candidate); err == nil {
		t.Fatal("second pending owner accepted")
	}
	view, footprint, err := prepared.Preview()
	if err != nil {
		t.Fatal(err)
	}
	wantRevision := uint64(3)
	if steer {
		wantRevision++
	}
	if view.Revision() != wantRevision || footprint.Revision() != wantRevision {
		t.Fatal("revision not based on actual entries")
	}
	if err := prepared.CheckRequest(sampling); err != nil {
		t.Fatal("candidate request", err)
	}
	feedbackEnvelope, err := prepared.FeedbackEnvelope()
	if err != nil {
		t.Fatal(err)
	}
	frozen, err := decodeFeedback(feedbackEnvelope)
	if err != nil || frozen != feedback {
		t.Fatal("frozen bytes or native role changed", err)
	}
	r0, err := provider.NewInputRestoreRecord(initial, initialEnvelope)
	if err != nil {
		t.Fatal(err)
	}
	r1, err := provider.NewSampleRestoreRecord(StopTurn, sampleEnvelope)
	if err != nil {
		t.Fatal(err)
	}
	r2, err := provider.NewStopFeedbackRestoreRecord(feedback, feedbackEnvelope)
	if err != nil {
		t.Fatal(err)
	}
	records := []provider.RestoreRecord{r0, r1, r2}
	if envelope, ok, err := prepared.SteeringEnvelope(); err != nil {
		t.Fatal(err)
	} else if ok {
		record, err := provider.NewInputRestoreRecord(association, envelope)
		if err != nil {
			t.Fatal(err)
		}
		records = append(records, record)
	}
	if err := prepared.Finalize(); err != nil {
		t.Fatal(err)
	}
	if !prepared.Finalized() || prepared.Finalize() == nil || prepared.Discard() == nil {
		t.Fatal("double resolution accepted")
	}
	_, actualFootprint, err := conversation.ProjectContextWindow()
	if err != nil || actualFootprint != footprint {
		t.Fatal("finalized footprint differs", err)
	}
	restored, err := restore(records)
	if err != nil {
		t.Fatal("restore", err)
	}
	liveBody := RestoredRequest(t, conversation, restored, sampling)
	if !bytes.Contains(liveBody.Bytes(), []byte("PRIVATE-OPAQUE")) {
		t.Fatal("opaque history lost")
	}
	history := restored.ProjectHistory()
	encoded, err := codec.MarshalStable(history)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, []byte("PRIVATE-")) {
		t.Fatal("opaque data exposed in semantic view")
	}
	if !reflect.DeepEqual(history, conversation.ProjectHistory()) {
		t.Fatal("semantic restore differs")
	}
	messages := history.Turns[0].Messages
	if len(messages) != 3+map[bool]int{false: 0, true: 1}[steer] || messages[2].Role != domain.SemanticHook || messages[2].InputID != "" {
		t.Fatal("hook merged with real input")
	}
	if steer && (messages[3].Role != domain.SemanticUser || messages[3].Text != "  用户纠正\n") {
		t.Fatal("real correction reordered")
	}
	transcript, _, err := restored.ProjectTranscriptFrom(0)
	if err != nil {
		t.Fatal(err)
	}
	hooks := 0
	for _, item := range transcript.Items {
		if item.Kind == domain.TranscriptHook {
			hooks++
			if item.Position.Entry != 2 || item.InputID != "" || item.Text != feedback.Text {
				t.Fatal("wrong hook cursor")
			}
		}
	}
	if hooks != 1 {
		t.Fatal("hook transcript missing")
	}
	history.Turns[0].Messages[2].HookSource.Reference.ID = "changed"
	if restored.ProjectHistory().Turns[0].Messages[2].HookSource.Reference.ID == "changed" {
		t.Fatal("projection alias")
	}
	badFeedback := feedback
	badFeedback.Reference.ID = "forged"
	if badFeedback.Validate() == nil {
		t.Fatal("source changed without model bytes")
	}
	otherReason, _ := domain.NewHookFeedbackCandidate("different authorized reason")
	result := domain.HookRunResult{Identity: feedback.Identity, Status: domain.HookBlock, Code: domain.HookDenied, ExitKnown: true, CleanupConfirmed: true, Origin: "execution"}
	forged, err := domain.NewStopFeedback(result, feedback.Reference, otherReason)
	if err != nil {
		t.Fatal(err)
	}
	forgedEnvelope, err := conversation.EncodeStopFeedback(forged)
	if err != nil {
		t.Fatal(err)
	}
	badRecord, err := provider.NewStopFeedbackRestoreRecord(feedback, forgedEnvelope)
	if err != nil {
		t.Fatal(err)
	}
	badRecords := append([]provider.RestoreRecord(nil), records...)
	badRecords[2] = badRecord
	if _, err := restore(badRecords); err == nil {
		t.Fatal("forged native feedback accepted")
	}
}
