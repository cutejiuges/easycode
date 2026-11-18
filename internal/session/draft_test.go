package session

import (
	"testing"

	"easycode/internal/domain"
)

func mustDraft(t testing.TB, kind EventKind, turnID domain.TurnID, payload any) RecordDraft {
	t.Helper()
	var (
		draft RecordDraft
		err   error
	)
	switch kind {
	case EventSessionMeta:
		draft, err = NewSessionMetaDraft(payload.(SessionMetaPayload))
	case EventThreadMeta:
		draft, err = NewThreadMetaDraft(payload.(ThreadMetaPayload))
	case EventTurnStarted:
		draft, err = NewTurnStartedDraft(turnID)
	case EventProviderNativeCommit:
		draft, err = NewProviderNativeCommitDraft(turnID, payload.(NativeCommitPayload))
	case EventSampleUsage:
		draft, err = NewSampleUsageDraft(turnID, payload.(domain.SampleUsage))
	case EventTurnCompleted:
		draft, err = NewTurnCompletedDraft(turnID)
	case EventTurnFailed:
		draft, err = NewTurnFailedDraft(turnID, payload.(TurnFailedPayload))
	default:
		t.Fatalf("unsupported test draft kind %q", kind)
	}
	if err != nil {
		t.Fatalf("create %s draft: %v", kind, err)
	}
	return draft
}

func testSampleUsage(t testing.TB) domain.SampleUsage {
	t.Helper()
	usage, err := domain.NewSampleUsage(
		domain.KnownUsageMetric(0), domain.KnownUsageMetric(2),
		domain.UnknownUsageMetric(), domain.KnownUsageMetric(3),
		domain.NotApplicableUsageMetric(),
	)
	if err != nil {
		t.Fatal(err)
	}
	return usage
}
