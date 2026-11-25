package anthropic

import (
	"testing"

	"easycode/internal/domain"
	"easycode/internal/provider"
	"easycode/internal/provider/testsupport"
)

// TestCheckpoint 仅注入家族原生摘要，调用真实生产 checkpoint 准备入口。
func (c *Conversation) TestCheckpoint(scope domain.CompactionScope, id domain.CompactionID, source provider.CompactionSource, text, opaque string, state domain.TaskProgressState) (*provider.PreparedCompaction, error) {
	return c.prepareCheckpoint(scope, id, source, compactionTestGeneration(text, opaque), syntheticRequestReceipt(domain.RequestSummary), state)
}
func (c *Conversation) TestAppendInput(t *testing.T, text string) provider.RestoreRecord {
	t.Helper()
	entry := testInputEntry(t, newUserMessage(text))
	c.history.Append(entry)
	envelope, err := encodeNativeCommit(entry)
	if err != nil {
		t.Fatal(err)
	}
	return testRestoreRecords(t, []provider.NativeCommitEnvelope{envelope})[0]
}
func (c *Conversation) TestRestore(t *testing.T, records []provider.RestoreRecord) (testsupport.CheckpointConversation, error) {
	t.Helper()
	restored, err := testRestoreAfterPreview(t, c.provider, "01900000-0000-7000-8000-000000000001", records)
	if err != nil {
		return nil, err
	}
	return restored.(*Conversation), nil
}
