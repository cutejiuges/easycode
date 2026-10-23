package builtin

import (
	"bytes"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"easycode/internal/tool"
)

type processProgress struct {
	mu     sync.Mutex
	value  tool.ExecProgress
	notify chan struct{}
}

func newProcessProgress() *processProgress { return &processProgress{notify: make(chan struct{}, 1)} }

func (progress *processProgress) snapshot() tool.ExecProgress {
	progress.mu.Lock()
	defer progress.mu.Unlock()
	return progress.value
}

func (progress *processProgress) update(data []byte, stderr bool, elapsed int64) {
	lines := int64(bytes.Count(data, []byte{'\n'}))
	recentBytes := data[max(0, len(data)-tool.MaxExecRecentBytes):]
	for len(recentBytes) > 0 && !utf8.RuneStart(recentBytes[0]) {
		recentBytes = recentBytes[1:]
	}
	recent := strings.ToValidUTF8(string(recentBytes), "?")
	progress.mu.Lock()
	previous := progress.value
	outBytes, errBytes := previous.StdoutBytes(), previous.StderrBytes()
	outLines, errLines := previous.StdoutLines(), previous.StderrLines()
	if stderr {
		errBytes += int64(len(data))
		errLines += lines
	} else {
		outBytes += int64(len(data))
		outLines += lines
	}
	recent = previous.Recent() + recent
	if len(recent) > tool.MaxExecRecentBytes {
		recent = recent[len(recent)-tool.MaxExecRecentBytes:]
		for len(recent) > 0 && !utf8.RuneStart(recent[0]) {
			recent = recent[1:]
		}
	}
	progress.value, _ = tool.NewExecProgress(max(elapsed, previous.ElapsedMillis()), outBytes, errBytes, outLines, errLines, recent, outBytes > tool.ExecCaptureBytesPerStream || errBytes > tool.ExecCaptureBytesPerStream)
	progress.mu.Unlock()
	select {
	case progress.notify <- struct{}{}:
	default:
	}
}

type processOutputWriter struct {
	buffer   tool.ExecOutputBuffer
	progress *processProgress
	stderr   bool
	clock    processClock
	started  time.Time
}

func (writer *processOutputWriter) Write(data []byte) (int, error) {
	count, err := writer.buffer.Write(data)
	writer.progress.update(data, writer.stderr, max(0, writer.clock.now().Sub(writer.started).Milliseconds()))
	return count, err
}
