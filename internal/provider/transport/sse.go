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
	errEventTooLarge = errors.New("SSE event exceeds maximum size")
	errIdleTimeout   = errors.New("idle timeout waiting for SSE data")
)

// IsEventTooLarge 判断错误是否表示单个 SSE event 超过配置上限。
func IsEventTooLarge(err error) bool {
	return errors.Is(err, errEventTooLarge)
}

// IsIdleTimeout 判断错误是否表示已建立的 SSE 流长时间没有任何数据或心跳。
func IsIdleTimeout(err error) bool {
	return errors.Is(err, errIdleTimeout)
}

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
	event  *SSEEvent
	err    error
	sealed bool
}

func superviseSSE(
	ctx context.Context,
	body io.ReadCloser,
	options StreamOptions,
	finished func(),
) <-chan SSEMessage {
	output := make(chan SSEMessage, 1)
	readerContext, stopReader := context.WithCancel(ctx)
	parsed := make(chan parsedSSEMessage, 1)
	activity := make(chan struct{}, 1)
	readerDone := make(chan struct{})

	go func() {
		defer close(readerDone)
		err := parseSSE(readerContext, body, options, activity, parsed)
		select {
		case parsed <- parsedSSEMessage{err: err}:
		case <-readerContext.Done():
		}
	}()

	go func() {
		defer close(output)
		defer finished()
		defer stopReader()
		defer func() { _ = body.Close() }()

		timer := time.NewTimer(options.IdleTimeout)
		defer timer.Stop()
		idle := timer.C
		resetTimer := func() {
			if idle == nil {
				return
			}
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
			case <-idle:
				finishReader()
				sendFinal(errIdleTimeout)
				return
			case <-activity:
				resetTimer()
			case message := <-parsed:
				if message.event != nil {
					if message.sealed {
						// 剩余读取已封闭，不再等待远端活动。
						timer.Stop()
						idle = nil
					} else {
						resetTimer()
					}
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
				if message.err != nil {
					sendFinal(message.err)
				}
				return
			}
		}
	}()

	return output
}

func parseSSE(
	ctx context.Context,
	reader io.Reader,
	options StreamOptions,
	activity chan<- struct{},
	output chan<- parsedSSEMessage,
) error {
	buffered := bufio.NewReaderSize(reader, 32<<10)
	var eventName string
	var eventID string
	var dataLines []string
	eventBytes := 0
	unacceptedBytes := -1

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
		sealed := unacceptedBytes < 0 && options.SealAfter != nil && options.SealAfter(*event)
		if sealed {
			// Peek 只查看此次读取已交付的字节。从此仅消费含分隔空行的完整帧。
			available, _ := buffered.Peek(buffered.Buffered())
			unacceptedBytes = len(available) - completeSSEPrefix(available)
		}
		select {
		case output <- parsedSSEMessage{event: event, sealed: sealed}:
		case <-ctx.Done():
			return ctx.Err()
		}
		eventName = ""
		eventID = ""
		dataLines = dataLines[:0]
		eventBytes = 0
		return nil
	}

	for unacceptedBytes < 0 || buffered.Buffered() > unacceptedBytes {
		line, eof, err := readSSELine(ctx, buffered, options.MaxEventBytes-eventBytes, activity)
		if err != nil {
			return err
		}
		eventBytes += len(line) + 1
		if eventBytes > options.MaxEventBytes {
			return errEventTooLarge
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
	return nil
}

// completeSSEPrefix 返回最后一个完整帧分隔空行之后的位置。
func completeSSEPrefix(data []byte) int {
	end := 0
	for start := 0; start < len(data); {
		newline := bytes.IndexByte(data[start:], '\n')
		if newline < 0 {
			break
		}
		next := start + newline + 1
		if len(trimSSELineEnding(data[start:next])) == 0 {
			end = next
		}
		start = next
	}
	return end
}

func readSSELine(
	ctx context.Context,
	reader *bufio.Reader,
	remaining int,
	activity chan<- struct{},
) ([]byte, bool, error) {
	if remaining <= 0 {
		return nil, false, errEventTooLarge
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
				return nil, false, errEventTooLarge
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
