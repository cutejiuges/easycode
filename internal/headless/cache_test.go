package headless

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
	"testing/iotest"

	"easycode/internal/codec"
	"easycode/internal/domain"
	"easycode/internal/protocol"
)

func TestCacheQueryStrictInputAndLiteralSlash(t *testing.T) {
	valid := `{"version":1,"type":"cache.query","request_id":"缓存-query"}`
	reader, err := NewStreamCommandReader(iotest.OneByteReader(strings.NewReader(valid)))
	if err != nil {
		t.Fatal(err)
	}
	command, err := reader.ReadCommand()
	if err != nil || command.Kind() != protocol.CommandQueryCache {
		t.Fatal(command, err)
	}
	if _, err := reader.ReadCommand(); err != io.EOF {
		t.Fatal(err)
	}
	for _, invalid := range []string{`{"version":1,"type":"cache.query","request_id":null}`, `{"version":1,"type":"cache.query","request_id":""}`, `{"version":1,"type":"cache.query","request_id":"q","text":"private"}`, `{"version":1,"type":"cache.query","request_id":"q","request_id":"r"}`, `{"version":1,"type":"cache.query","Request_id":"q"}`, `{"version":1,"type":"cache.query","request_id":"q","key":"private"}`, `{"version":1,"type":"cache.query","request_id":"` + strings.Repeat("a", 129) + `"}`} {
		if _, err := DecodeStreamCommand([]byte(invalid)); err == nil {
			t.Fatal("invalid query accepted", invalid)
		}
	}
	slash, err := DecodeStreamCommand([]byte(`{"version":1,"type":"input.submit","request_id":"body","text":"/cache"}`))
	if err != nil || slash.Kind() != protocol.CommandSubmitInput || slash.Text() != "/cache" {
		t.Fatal("slash interpreted as command")
	}
}
func TestCacheSnapshotExternalGoldenAndSensitiveFields(t *testing.T) {
	data, err := os.ReadFile("../protocol/testdata/cache_snapshot.json")
	if err != nil {
		t.Fatal(err)
	}
	var payload protocol.CacheSnapshotPayload
	if err = codec.UnmarshalRecord(data, &payload); err != nil {
		t.Fatal(err)
	}
	sid := domain.SessionID("00000000-0010-7000-8000-000000000010")
	tid := domain.ThreadID("00000000-0011-7000-8000-000000000011")
	projector, err := NewStreamProjector(sid, tid)
	if err != nil {
		t.Fatal(err)
	}
	command, _ := protocol.NewQueryCacheCommand("cache")
	accepted, _ := protocol.NewAcceptedCommandResult(command, protocol.CommandDispositionQueried)
	response, _ := protocol.NewCommandResultItem(accepted)
	event, err := protocol.NewCacheSnapshot(sid, tid, payload)
	if err != nil {
		t.Fatal(err)
	}
	item, err := protocol.NewCorrelatedContextItem("cache", event)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	for _, control := range []protocol.ControlItem{response, item} {
		wire, err := projector.Project(control)
		if err != nil {
			t.Fatal(err)
		}
		if err := NewStreamEncoder(&output).Write(wire); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile("testdata/cache.golden.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(want, output.Bytes()) {
		t.Fatalf("cache golden differs:\n%s", output.String())
	}
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != 2 || !strings.Contains(lines[0], `"type":"control.response"`) || !strings.Contains(lines[1], `"type":"cache.snapshot"`) {
		t.Fatal("response/snapshot order changed")
	}
	for _, forbidden := range []string{`"prompt_cache_key":`, `"authorization":`, `"input":`, `"previous_response_id":`, `"payload":`, `"api_key":`} {
		if strings.Contains(output.String(), forbidden) {
			t.Fatal("sensitive field leaked", forbidden)
		}
	}
	if output.Len() > 64<<10 {
		t.Fatal("snapshot is unbounded")
	}
	malicious := event
	malicious.Payload = bytes.Replace(event.Payload, []byte(`"scope":"process"`), []byte(`"scope":"process","authorization":"private"`), 1)
	if _, err := protocol.NewCorrelatedContextItem("cache", malicious); err == nil {
		t.Fatal("sensitive payload accepted")
	}
}
