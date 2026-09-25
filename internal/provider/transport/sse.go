package transport

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"time"
)

var (
	// ErrEventTooLarge 表示单个 SSE event 超过配置上限。
	ErrEventTooLarge = errors.New("SSE event exceeds maximum size")
	// ErrIdleTimeout 表示已建立的 SSE 流长时间没有任何数据或心跳。
	ErrIdleTimeout = errors.New("idle timeout waiting for SSE data")
)

// SSEEvent 是与具体 HTTP client 解耦后的原始事件。
type SSEEvent struct {
	ID   string
	Name string
	Data string
}

// SSEMessage 携带一个 frame 或流的最终读取结果。
// Event 和 Err 至多一个非零；io.EOF 表示 HTTP body 正常结束。
type SSEMessage struct {
	Event *SSEEvent
	Err   error
}

type parsedSSEMessage struct {
	event *SSEEvent
	err   error
}

func superviseSSE(
	ctx context.Context,
	body io.ReadCloser,
	options StreamOptions,
) <-chan SSEMessage {
	output := make(chan SSEMessage, 1)
	readerContext, stopReader := context.WithCancel(ctx)
	parsed := make(chan parsedSSEMessage, 1)
	activity := make(chan struct{}, 1)
	readerDone := make(chan struct{})

	go func() {
		defer close(readerDone)
		err := parseSSE(readerContext, body, options.MaxEventBytes, activity, parsed)
		select {
		case parsed <- parsedSSEMessage{err: err}:
		case <-readerContext.Done():
		}
	}()

	go func() {
		defer close(output)
		defer stopReader()
		defer func() { _ = body.Close() }()

		timer := time.NewTimer(options.IdleTimeout)
		defer timer.Stop()
		resetTimer := func() {
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(options.IdleTimeout)
		}
		finishReader := func() {
			stopReader()
			_ = body.Close()
			<-readerDone
		}
		sendFinal := func(err error) {
			select {
			case output <- SSEMessage{Err: err}:
			case <-ctx.Done():
			}
		}

		for {
			select {
			case <-ctx.Done():
				finishReader()
				sendFinal(ctx.Err())
				return
			case <-timer.C:
				finishReader()
				sendFinal(ErrIdleTimeout)
				return
			case <-activity:
				resetTimer()
			case message := <-parsed:
				if message.event != nil {
					resetTimer()
					select {
					case output <- SSEMessage{Event: message.event}:
					case <-ctx.Done():
						finishReader()
						sendFinal(ctx.Err())
						return
					}
					continue
				}
				<-readerDone
				sendFinal(message.err)
				return
			}
		}
	}()

	return output
}

func parseSSE(
	ctx context.Context,
	reader io.Reader,
	maxEventBytes int,
	activity chan<- struct{},
	output chan<- parsedSSEMessage,
) error {
	buffered := bufio.NewReaderSize(reader, 32<<10)
	var eventName string
	var eventID string
	var dataLines []string
	eventBytes := 0

	emit := func() error {
		if len(dataLines) == 0 {
			eventName = ""
			eventID = ""
			eventBytes = 0
			return nil
		}
		event := &SSEEvent{
			ID:   eventID,
			Name: eventName,
			Data: strings.Join(dataLines, "\n"),
		}
		select {
		case output <- parsedSSEMessage{event: event}:
		case <-ctx.Done():
			return ctx.Err()
		}
		eventName = ""
		eventID = ""
		dataLines = dataLines[:0]
		eventBytes = 0
		return nil
	}

	for {
		line, eof, err := readSSELine(ctx, buffered, maxEventBytes-eventBytes, activity)
		if err != nil {
			return err
		}
		eventBytes += len(line) + 1
		if eventBytes > maxEventBytes {
			return ErrEventTooLarge
		}

		if len(line) == 0 {
			if err := emit(); err != nil {
				return err
			}
		} else if line[0] != ':' {
			field, value, found := bytes.Cut(line, []byte{':'})
			if !found {
				value = nil
			} else if len(value) > 0 && value[0] == ' ' {
				value = value[1:]
			}
			switch string(field) {
			case "event":
				eventName = string(value)
			case "id":
				if !bytes.ContainsRune(value, '\x00') {
					eventID = string(value)
				}
			case "data":
				dataLines = append(dataLines, string(value))
			}
		}

		if eof {
			if err := emit(); err != nil {
				return err
			}
			return io.EOF
		}
	}
}

func readSSELine(
	ctx context.Context,
	reader *bufio.Reader,
	remaining int,
	activity chan<- struct{},
) ([]byte, bool, error) {
	if remaining <= 0 {
		return nil, false, ErrEventTooLarge
	}
	line := make([]byte, 0, min(remaining, 4096))
	for {
		fragment, err := reader.ReadSlice('\n')
		if len(fragment) > 0 {
			select {
			case activity <- struct{}{}:
			default:
			}
			if len(line)+len(fragment) > remaining {
				return nil, false, ErrEventTooLarge
			}
			line = append(line, fragment...)
		}
		switch {
		case err == nil:
			return trimSSELineEnding(line), false, nil
		case errors.Is(err, bufio.ErrBufferFull):
			select {
			case <-ctx.Done():
				return nil, false, ctx.Err()
			default:
			}
			continue
		case errors.Is(err, io.EOF):
			return trimSSELineEnding(line), true, nil
		default:
			return nil, false, err
		}
	}
}

func trimSSELineEnding(line []byte) []byte {
	line = bytes.TrimSuffix(line, []byte{'\n'})
	line = bytes.TrimSuffix(line, []byte{'\r'})
	return line
}
