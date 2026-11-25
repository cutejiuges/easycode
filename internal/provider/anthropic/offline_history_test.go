package anthropic

import (
	"testing"

	"easycode/internal/domain"
	"easycode/internal/provider"
	"easycode/internal/provider/testsupport"
)

func testRestoreAfterPreview(t *testing.T, instance *Provider, thread domain.ThreadID, records []provider.RestoreRecord) (provider.Conversation, error) {
	t.Helper()
	return testsupport.RestoreAfterPreview(t, instance, NewHistoryReader(), provider.HistorySource{ThreadID: thread, Family: instance.Family(), Wire: instance.Family().Wire(), Model: instance.config.Model}, records)
}
func testOfflineUsageEvidence(t *testing.T, instance *Provider, prepared *provider.PreparedCompaction, checkpoint provider.CheckpointEnvelope) {
	t.Helper()
	testsupport.OfflineUsageEvidence(t, instance, NewHistoryReader(), provider.HistorySource{ThreadID: "01900000-0000-7000-8000-000000000001", Family: instance.Family(), Wire: instance.Family().Wire(), Model: instance.config.Model}, prepared, checkpoint)
}
