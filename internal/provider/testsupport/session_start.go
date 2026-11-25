package testsupport

import (
	"bytes"
	"reflect"
	"testing"

	"easycode/internal/codec"
	"easycode/internal/domain"
	"easycode/internal/provider"
)

func SessionStartContext(t *testing.T, target domain.SessionStartTarget, body string) domain.SessionStartContext {
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

type SessionStartConversation interface {
	RequestConversation
	provider.SessionStartPreparer
}

// SessionStartInputs 验证启动上下文在真实输入之前提交。
func SessionStartInputs(t *testing.T, c SessionStartConversation, sampling provider.TurnInput) (provider.RestoreRecord, provider.NativeCommitEnvelope) {
	t.Helper()
	input, _ := domain.NewUserInput("01900000-0000-7000-8000-000000000002", "real initial input")
	association, _ := domain.NewInputAssociation(StopTurn, domain.InputInitial, 0, []domain.InputID{input.ID()})
	startup := SessionStartContext(t, domain.SessionStartTarget{Source: domain.SessionStartStartup, TurnID: StopTurn}, "frozen startup context")
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
	initialRequest, err := c.TestRequest(sampling)
	if err != nil {
		t.Fatal(err)
	}
	requestBody := initialRequest.Bytes()
	contextIndex := bytes.Index(requestBody, []byte("frozen startup context"))
	inputIndex := bytes.Index(requestBody, []byte("real initial input"))
	if contextIndex < 0 || contextIndex >= inputIndex {
		t.Fatal("startup ordering differs")
	}

	return startRecord, inputEnvelope
}

func SessionStartSupplement(t *testing.T, c provider.SessionStartPreparer, scope domain.CompactionScope, id domain.CompactionID, source provider.CompactionSource) (domain.SessionStartContext, provider.NativeCommitEnvelope, provider.RestoreRecord) {
	t.Helper()
	value := SessionStartContext(t, domain.SessionStartTarget{Source: domain.SessionStartCompact, CompactionID: id, Scope: scope,
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
	return value, compactEnvelope, compactRecord
}

func CheckSessionStartRestore(t *testing.T, c, restored NativeConversation, sampling provider.TurnInput) (codec.CanonicalJSON, provider.RequestEvidence) {
	t.Helper()
	live, err := c.TestRequest(sampling)
	if err != nil {
		t.Fatal(err)
	}
	again, err := restored.TestRequest(sampling)
	if err != nil || !bytes.Equal(live.Bytes(), again.Bytes()) {
		t.Fatal("restored canonical input differs", err)
	}
	evidence, _ := restored.TestEvidence(sampling)
	liveEvidence, _ := c.TestEvidence(sampling)
	if evidence.NativeFingerprint() == "" || evidence.NativeFingerprint() != liveEvidence.NativeFingerprint() {
		t.Fatal("restored native fingerprint differs")
	}
	window, footprint, err := restored.ProjectContextWindow()
	if err != nil || footprint.Revision() != 6 || len(window.History().CheckpointContexts) != 1 || !reflect.DeepEqual(restored.ProjectHistory(), c.ProjectHistory()) {
		t.Fatal("context footprint or history projection differs", err)
	}
	return live, evidence
}
