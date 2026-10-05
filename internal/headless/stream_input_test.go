package headless

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"easycode/internal/protocol"
)

type fragmentedReader struct {
	data  []byte
	sizes []int
	index int
}

func (reader *fragmentedReader) Read(target []byte) (int, error) {
	if len(reader.data) == 0 {
		return 0, io.EOF
	}
	size := 1
	if len(reader.sizes) > 0 {
		size = reader.sizes[reader.index%len(reader.sizes)]
		reader.index++
	}
	if size > len(reader.data) {
		size = len(reader.data)
	}
	if size > len(target) {
		size = len(target)
	}
	copy(target, reader.data[:size])
	reader.data = reader.data[size:]
	return size, nil
}

func TestStreamCommandReaderDecodesFragmentedUTF8AndEOFTail(t *testing.T) {
	t.Parallel()
	wire := []byte("\n{\"version\":1,\"type\":\"input.submit\",\"request_id\":\"input-1\",\"text\":\"你🙂好\"}\n" +
		"{\"version\":1,\"type\":\"session.shutdown\",\"request_id\":\"shutdown-1\"}")
	reader, err := NewStreamCommandReader(&fragmentedReader{data: wire, sizes: []int{1, 2, 3, 1, 5}})
	if err != nil {
		t.Fatal(err)
	}
	first, err := reader.ReadCommand()
	if err != nil || first.Kind() != protocol.CommandSubmitInput || first.Text() != "你🙂好" {
		t.Fatalf("first command = %#v, %v", first, err)
	}
	second, err := reader.ReadCommand()
	if err != nil || second.Kind() != protocol.CommandShutdown {
		t.Fatalf("second command = %#v, %v", second, err)
	}
	if _, err := reader.ReadCommand(); err != io.EOF {
		t.Fatalf("tail ReadCommand() error = %v", err)
	}
}

func TestDecodeStreamCommandStrictFailures(t *testing.T) {
	t.Parallel()
	invalidUTF8 := append([]byte(`{"version":1,"type":"input.submit","request_id":"input-1","text":"`), 0xff)
	invalidUTF8 = append(invalidUTF8, []byte(`"}`)...)
	tests := map[string][]byte{
		"empty":              {},
		"non object":         []byte(`[]`),
		"missing field":      []byte(`{"version":1,"type":"input.submit","request_id":"input-1"}`),
		"duplicate":          []byte(`{"version":1,"type":"session.shutdown","request_id":"one","request_id":"two"}`),
		"unknown field":      []byte(`{"version":1,"type":"session.shutdown","request_id":"one","api_key":"do-not-echo"}`),
		"multiple values":    []byte(`{"version":1,"type":"session.shutdown","request_id":"one"} {}`),
		"unknown type":       []byte(`{"version":1,"type":"future","request_id":"one"}`),
		"unknown version":    []byte(`{"version":2,"type":"session.shutdown","request_id":"one"}`),
		"invalid request ID": []byte(`{"version":1,"type":"session.shutdown","request_id":" bad "}`),
		"invalid turn ID":    []byte(`{"version":1,"type":"turn.interrupt","request_id":"one","expected_turn_id":"bad"}`),
		"extra payload":      []byte(`{"version":1,"type":"session.shutdown","request_id":"one","text":"private"}`),
		"invalid UTF-8":      invalidUTF8,
	}
	for name, wire := range tests {
		wire := wire
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := DecodeStreamCommand(wire); err == nil {
				t.Fatal("DecodeStreamCommand() unexpectedly succeeded")
			} else if strings.Contains(err.Error(), "do-not-echo") || strings.Contains(err.Error(), "private") {
				t.Fatalf("error leaked input: %v", err)
			}
		})
	}
}

func TestStreamInputSizeBoundaries(t *testing.T) {
	t.Parallel()
	boundary := strings.Repeat("x", protocol.MaxInputBytes)
	wire, err := json.Marshal(streamSubmitCommand{
		Version: 1, Type: "input.submit", RequestID: "boundary", Text: boundary,
	})
	if err != nil {
		t.Fatal(err)
	}
	command, err := DecodeStreamCommand(wire)
	if err != nil || len(command.Text()) != protocol.MaxInputBytes {
		t.Fatalf("boundary command length=%d error=%v", len(command.Text()), err)
	}
	oversized, err := json.Marshal(streamSubmitCommand{
		Version: 1, Type: "input.submit", RequestID: "oversized", Text: boundary + "x",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeStreamCommand(oversized); err == nil {
		t.Fatal("oversized decoded input was accepted")
	}

	line := bytes.Repeat([]byte{'x'}, MaxStreamLineBytes+1)
	reader, err := NewStreamCommandReader(bytes.NewReader(line))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reader.ReadCommand(); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversized encoded line error = %v", err)
	}
}
