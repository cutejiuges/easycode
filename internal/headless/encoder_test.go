package headless

import (
	"testing"

	"easycode/internal/domain"
)

const (
	testSessionID = domain.SessionID("00000000-0010-7000-8000-000000000010")
	testThreadID  = domain.ThreadID("00000000-0011-7000-8000-000000000011")
	testTurnID    = domain.TurnID("00000000-0012-7000-8000-000000000012")
)

func testHeadlessUsage(t *testing.T) domain.SampleUsage {
	t.Helper()
	usage, err := domain.NewSampleUsage(
		domain.KnownUsageMetric(0),
		domain.UnknownUsageMetric(),
		domain.NotApplicableUsageMetric(),
		domain.KnownUsageMetric(5),
		domain.KnownUsageMetric(2),
	)
	if err != nil {
		t.Fatal(err)
	}
	return usage
}
