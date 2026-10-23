package tool

import (
	"bytes"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestExecOutputChunkIndependentAndOwned(t *testing.T) {
	data := bytes.Repeat([]byte("开始abcdef尾巴\n"), 60000)
	var whole ExecOutputBuffer
	_, _ = whole.Write(data)
	want := whole.Snapshot()
	for _, chunk := range []int{1, 7, 4096, ExecCaptureBytesPerStream - 1, len(data)} {
		var buffer ExecOutputBuffer
		for start := 0; start < len(data); start += chunk {
			_, _ = buffer.Write(data[start:min(start+chunk, len(data))])
		}
		got := buffer.Snapshot()
		if cap(buffer.head)+cap(buffer.tail) > ExecCaptureBytesPerStream {
			t.Fatal("capture allocation exceeds hard limit")
		}
		if got != want {
			t.Fatalf("chunk %d changed snapshot", chunk)
		}
		if got.stats.observed != int64(len(data)) || got.stats.retained != ExecCaptureBytesPerStream {
			t.Fatal("wrong raw byte accounting")
		}
		_, _ = buffer.Write([]byte("changed"))
		if got != want {
			t.Fatal("snapshot mutated after write")
		}
	}
	const half = ExecCaptureBytesPerStream / 2
	if want.head != string(data[:half]) || want.tail != string(data[len(data)-half:]) {
		t.Fatal("head or tail lost")
	}
}

func TestExecOutputPreviewBudgetsAndStreams(t *testing.T) {
	for _, size := range []int{0, 1, ExecCaptureBytesPerStream - 1, ExecCaptureBytesPerStream, ExecCaptureBytesPerStream + 1, 2 * ExecCaptureBytesPerStream} {
		var out, errout ExecOutputBuffer
		_, _ = out.Write(bytes.Repeat([]byte("a"), size))
		_, _ = errout.Write(bytes.Repeat([]byte("z"), size))
		metadata := ExecResultMetadata{exitKnown: true, exitCode: 2}
		preview, truncated, err := RenderExecOutput(out.Snapshot(), errout.Snapshot(), metadata)
		if err != nil || len(preview.Text()) > ExecModelPreviewBytes || !utf8.ValidString(preview.Text()) {
			t.Fatalf("invalid preview for %d: %v", size, err)
		}
		if strings.Index(preview.Text(), "stdout:") > strings.Index(preview.Text(), "stderr:") || !strings.Contains(preview.Text(), "exit_code: 2") {
			t.Fatal("stream order or exit lost")
		}
		if size > ExecModelPreviewBytes/2 && (!truncated || strings.Count(preview.Text(), "bytes omitted") != 2) {
			t.Fatal("missing per-stream omission")
		}
		if size <= ExecCaptureBytesPerStream && out.Snapshot().Stats().OmittedBytes() != 0 {
			t.Fatal("capture prematurely truncated")
		}
	}
}

func TestExecOutputUTF8AndExactOmission(t *testing.T) {
	var buffer ExecOutputBuffer
	data := []byte(strings.Repeat("你好🙂", 100000))
	for i := range data {
		_, _ = buffer.Write(data[i : i+1])
	}
	snapshot := buffer.Snapshot()
	text, cut := snapshot.render(1000)
	if !cut || !utf8.ValidString(text) || len(text) > 1000 || strings.Contains(text, "?") {
		t.Fatal("UTF-8 boundary damaged")
	}
	var invalid ExecOutputBuffer
	_, _ = invalid.Write([]byte{'a', 255, 'z'})
	text, cut = invalid.Snapshot().render(100)
	if cut || text != "a?z" {
		t.Fatalf("invalid bytes rendered as %q", text)
	}
	var ascii ExecOutputBuffer
	_, _ = ascii.Write(bytes.Repeat([]byte("a"), 2000))
	text, cut = ascii.Snapshot().render(1000)
	if !cut || !strings.Contains(text, "1096 bytes omitted") {
		t.Fatalf("incorrect omitted count: %s", text)
	}
}
