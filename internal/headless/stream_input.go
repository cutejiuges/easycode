package headless

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"unicode/utf8"

	"easycode/internal/codec"
	"easycode/internal/domain"
	"easycode/internal/protocol"
)

const (
	// MaxStreamLineBytes 限制单个 encoded NDJSON frame，覆盖 4 MiB 文本的最坏 JSON escape。
	MaxStreamLineBytes = 32 << 20
	streamReaderBuffer = 32 << 10
)

// StreamCommandReader 从任意分片的输入流逐行严格解码 v1 控制命令。
type StreamCommandReader struct {
	reader *bufio.Reader
}

// NewStreamCommandReader 创建不执行读取的增量 reader。
func NewStreamCommandReader(reader io.Reader) (*StreamCommandReader, error) {
	if reader == nil {
		return nil, fmt.Errorf("stream input is unavailable")
	}
	return &StreamCommandReader{reader: bufio.NewReaderSize(reader, streamReaderBuffer)}, nil
}

// ReadCommand 返回下一条非空命令；输入耗尽时返回 io.EOF。
func (reader *StreamCommandReader) ReadCommand() (protocol.Command, error) {
	if reader == nil || reader.reader == nil {
		return protocol.Command{}, fmt.Errorf("stream input is unavailable")
	}
	for {
		line, err := reader.readLine()
		if err != nil && len(line) == 0 {
			return protocol.Command{}, err
		}
		if len(bytes.TrimSpace(line)) == 0 {
			if err == io.EOF {
				return protocol.Command{}, io.EOF
			}
			continue
		}
		command, decodeErr := DecodeStreamCommand(line)
		if decodeErr != nil {
			return protocol.Command{}, decodeErr
		}
		return command, nil
	}
}

func (reader *StreamCommandReader) readLine() ([]byte, error) {
	line := make([]byte, 0, streamReaderBuffer)
	for {
		fragment, err := reader.reader.ReadSlice('\n')
		if len(line)+len(fragment) > MaxStreamLineBytes+1 {
			return nil, fmt.Errorf("stream input line exceeds %d bytes", MaxStreamLineBytes)
		}
		line = append(line, fragment...)
		switch err {
		case nil:
			line = line[:len(line)-1]
			if len(line) > 0 && line[len(line)-1] == '\r' {
				line = line[:len(line)-1]
			}
			if len(line) > MaxStreamLineBytes {
				return nil, fmt.Errorf("stream input line exceeds %d bytes", MaxStreamLineBytes)
			}
			return line, nil
		case bufio.ErrBufferFull:
			continue
		case io.EOF:
			if len(line) > MaxStreamLineBytes {
				return nil, fmt.Errorf("stream input line exceeds %d bytes", MaxStreamLineBytes)
			}
			return line, io.EOF
		default:
			return nil, fmt.Errorf("read stream input failed")
		}
	}
}

type streamCommandEnvelope struct {
	Version   int    `json:"version"`
	Type      string `json:"type"`
	RequestID string `json:"request_id"`
}

type streamSubmitCommand struct {
	Version   int    `json:"version"`
	Type      string `json:"type"`
	RequestID string `json:"request_id"`
	Text      string `json:"text"`
}

type streamInterruptCommand struct {
	Version        int    `json:"version"`
	Type           string `json:"type"`
	RequestID      string `json:"request_id"`
	ExpectedTurnID string `json:"expected_turn_id"`
}

type streamShutdownCommand struct {
	Version   int    `json:"version"`
	Type      string `json:"type"`
	RequestID string `json:"request_id"`
}

// DecodeStreamCommand 严格解码一个外部 stream-json v1 command frame。
func DecodeStreamCommand(line []byte) (protocol.Command, error) {
	if len(line) == 0 || len(line) > MaxStreamLineBytes || !utf8.Valid(line) {
		return protocol.Command{}, fmt.Errorf("stream command is invalid")
	}
	if _, err := scanStreamMembers(line); err != nil {
		return protocol.Command{}, fmt.Errorf("stream command is invalid")
	}
	var envelope streamCommandEnvelope
	if err := codec.Unmarshal(line, &envelope); err != nil {
		return protocol.Command{}, fmt.Errorf("stream command is invalid")
	}
	if envelope.Version != eventVersion {
		return protocol.Command{}, fmt.Errorf("stream command version is unsupported")
	}
	requestID, err := protocol.ParseRequestID(envelope.RequestID)
	if err != nil {
		return protocol.Command{}, fmt.Errorf("stream command request ID is invalid")
	}
	switch envelope.Type {
	case "input.submit":
		var wire streamSubmitCommand
		if err := codec.DecodeObject(line, &wire, "version", "type", "request_id"); err != nil {
			return protocol.Command{}, fmt.Errorf("input.submit command is invalid")
		}
		command, err := protocol.NewSubmitInputCommand(requestID, wire.Text)
		if err != nil {
			return protocol.Command{}, fmt.Errorf("input.submit command is invalid")
		}
		return command, nil
	case "turn.interrupt":
		var wire streamInterruptCommand
		if err := codec.DecodeObject(line, &wire, "version", "type", "request_id"); err != nil {
			return protocol.Command{}, fmt.Errorf("turn.interrupt command is invalid")
		}
		turnID, err := domain.ParseTurnID(wire.ExpectedTurnID)
		if err != nil {
			return protocol.Command{}, fmt.Errorf("turn.interrupt command is invalid")
		}
		return protocol.NewInterruptCommand(requestID, turnID)
	case "session.shutdown":
		var wire streamShutdownCommand
		if err := codec.DecodeObject(line, &wire, "version", "type", "request_id"); err != nil {
			return protocol.Command{}, fmt.Errorf("session.shutdown command is invalid")
		}
		return protocol.NewShutdownCommand(requestID)
	default:
		return protocol.Command{}, fmt.Errorf("stream command type is unsupported")
	}
}

func scanStreamMembers(line []byte) (map[string]json.RawMessage, error) {
	return codec.ObjectMembers(line)
}
