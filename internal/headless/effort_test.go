package headless

import (
	"io"
	"strings"
	"testing"
	"testing/iotest"

	"easycode/internal/protocol"
)

func TestEffortCommandsStrictBranchesAndBoundedLine(t *testing.T) {
	for _, line := range []string{
		`{"version":1,"type":"effort.query","request_id":"查询🙂"}`,
		`{"version":1,"type":"effort.set","request_id":"a","mode":"auto"}`,
		`{"version":1,"type":"effort.set","request_id":"r","mode":"reset"}`,
		`{"version":1,"type":"effort.set","request_id":"v","mode":"value","level":"none"}`,
	} {
		reader, err := NewStreamCommandReader(iotest.OneByteReader(strings.NewReader(line)))
		if err != nil {
			t.Fatal(err)
		}
		command, err := reader.ReadCommand()
		if err != nil || command.Validate() != nil {
			t.Fatal("valid effort frame rejected", err)
		}
		if _, err := reader.ReadCommand(); err != io.EOF {
			t.Fatal("EOF not drained")
		}
	}
	prefix := `{"version":1,"type":"effort.set","request_id":"x",`
	for _, tail := range []string{
		`"mode":"AUTO"}`, `"mode":"unset"}`, `"mode":"value","level":"HIGH"}`, `"mode":"value"}`, `"mode":"value","level":null}`, `"mode":"value","level":"future"}`,
		`"mode":"auto","level":""}`, `"mode":"reset","level":"high"}`, `"mode":null}`, `"mode":"auto","extra":"secret"}`, `"mode":"auto","mode":"reset"}`,
	} {
		_, err := DecodeStreamCommand([]byte(prefix + tail))
		if err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatal("invalid effort frame or unsafe error", tail, err)
		}
	}
	query := `{"version":1,"type":"effort.query","request_id":"q"}`
	if _, err := DecodeStreamCommand([]byte(query + strings.Repeat(" ", MaxEffortCommandBytes-len(query)))); err != nil {
		t.Fatal("limit rejected", err)
	}
	if _, err := DecodeStreamCommand([]byte(query + strings.Repeat(" ", MaxEffortCommandBytes-len(query)+1))); err == nil {
		t.Fatal("effort limit ignored")
	}
	for _, line := range []string{strings.Replace(query, `"q"`, `null`, 1), strings.Replace(query, `"q"`, `"q","mode":"auto"`, 1), strings.Replace(query, `"version":1,`, "", 1)} {
		if _, err := DecodeStreamCommand([]byte(line)); err == nil {
			t.Fatal("query shape accepted", line)
		}
	}
	literal, err := DecodeStreamCommand([]byte(`{"version":1,"type":"input.submit","request_id":"literal","text":"/effort high"}`))
	if err != nil || literal.Kind() != protocol.CommandSubmitInput || literal.Text() != "/effort high" {
		t.Fatal("slash input interpreted")
	}
}
