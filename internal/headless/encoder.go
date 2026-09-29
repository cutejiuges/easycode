package headless

import (
	"bytes"
	"fmt"
	"io"
	"unicode/utf8"

	"easycode/internal/codec"
	"easycode/internal/fault"
)

// Encoder 是 JSONL stdout 的唯一顺序 writer。
type Encoder struct {
	writer io.Writer
	failed bool
}

// NewEncoder 创建不执行 I/O 的 JSONL encoder。
func NewEncoder(writer io.Writer) *Encoder {
	return &Encoder{writer: writer}
}

// Write 写入一个完整 JSON object 和尾部换行。
func (encoder *Encoder) Write(event Event) error {
	if encoder == nil || encoder.writer == nil {
		return fmt.Errorf("headless output is unavailable")
	}
	if encoder.failed {
		return fmt.Errorf("headless output is unavailable")
	}
	if err := validateEvent(event); err != nil {
		return err
	}
	encoded, err := codec.MarshalStable(event)
	if err != nil || !utf8.Valid(encoded) || bytes.ContainsAny(encoded, "\r\n") {
		encoder.failed = true
		return fmt.Errorf("encode headless event failed")
	}
	frame := make([]byte, 0, len(encoded)+1)
	frame = append(frame, encoded...)
	frame = append(frame, '\n')
	if err := writeFull(encoder.writer, frame); err != nil {
		encoder.failed = true
		return fmt.Errorf("write headless event failed")
	}
	return nil
}

// WriteError 写入一个独立的 stream-level error 事件。
func WriteError(writer io.Writer, failure fault.Summary) error {
	event, err := NewErrorEvent(failure)
	if err != nil {
		return err
	}
	return NewEncoder(writer).Write(event)
}

func writeFull(writer io.Writer, content []byte) error {
	for len(content) > 0 {
		written, err := writer.Write(content)
		if written < 0 || written > len(content) {
			return io.ErrShortWrite
		}
		content = content[written:]
		if err != nil {
			return err
		}
		if written == 0 {
			return io.ErrShortWrite
		}
	}
	return nil
}
