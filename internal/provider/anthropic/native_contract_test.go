package anthropic

import (
	"testing"

	"easycode/internal/codec"
	"easycode/internal/provider"
	"easycode/internal/tool"
)

// 下列入口仅供固定原生 fixture 的共享恢复断言使用，不进入正式构建。
func (c *Conversation) TestAppendFixture(t *testing.T, envelope provider.NativeCommitEnvelope) {
	t.Helper()
	entry, err := decodeNativeCommit(envelope)
	if err != nil {
		t.Fatal(err)
	}
	entry.TurnID = "01900000-0000-7000-8000-000000000010"
	c.history.Append(entry)
}
func (c *Conversation) TestNativeJSON(t *testing.T) []byte {
	t.Helper()
	data, err := codec.MarshalStable(c.history.Entries())
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func restoreToolHistory(t *testing.T, instance *Provider, history []nativeHistoryEntry, evidence []tool.ModelOutput) (*Conversation, *Conversation) {
	t.Helper()
	uninterrupted := &Conversation{provider: instance}
	var commits []provider.NativeCommitEnvelope
	for _, entry := range history {
		uninterrupted.history.Append(entry)
		commit, err := encodeNativeCommit(entry)
		if err != nil {
			t.Fatal(err)
		}
		commits = append(commits, commit)
	}
	restoredValue, err := testRestoreAfterPreview(t, instance, "01900000-0000-7000-8000-000000000001", testRestoreRecords(t, commits, evidence))
	if err != nil {
		t.Fatal(err)
	}
	restored := restoredValue.(*Conversation)
	return uninterrupted, restored
}
