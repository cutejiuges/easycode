package tool

import (
	"bytes"
	"fmt"
	"strings"
	"unicode/utf8"
)

// ExecOutputBuffer 捕获单流稳定头尾；由单一reader或外部owner串行调用。
// 零值可用，缓冲区不持有goroutine或外部资源。
type ExecOutputBuffer struct {
	head     []byte
	tail     []byte
	tailNext int
	observed int64
}

// Write 保留最多512 KiB原始字节，尾部通过环形缓冲避免反复移动。
func (buffer *ExecOutputBuffer) Write(data []byte) (int, error) {
	count := len(data)
	buffer.observed += int64(count)
	const half = ExecCaptureBytesPerStream / 2
	if len(data) > 0 && buffer.head == nil {
		buffer.head = make([]byte, 0, half)
	}
	if len(buffer.head) < half {
		n := min(half-len(buffer.head), len(data))
		buffer.head = append(buffer.head, data[:n]...)
		data = data[n:]
	}
	if len(data) >= half {
		if cap(buffer.tail) < half {
			buffer.tail = make([]byte, half)
		} else {
			buffer.tail = buffer.tail[:half]
		}
		copy(buffer.tail, data[len(data)-half:])
		buffer.tailNext = 0
		return count, nil
	}
	if len(buffer.tail) < half {
		if len(data) > 0 && buffer.tail == nil {
			buffer.tail = make([]byte, 0, half)
		}
		n := min(half-len(buffer.tail), len(data))
		buffer.tail = append(buffer.tail, data[:n]...)
		data = data[n:]
	}
	if len(data) > 0 {
		n := copy(buffer.tail[buffer.tailNext:], data)
		copy(buffer.tail, data[n:])
		buffer.tailNext = (buffer.tailNext + len(data)) % half
	}
	return count, nil
}

// ExecOutputSnapshot 是独立于捕获器生命周期的不可变原始输出快照。
type ExecOutputSnapshot struct {
	head  string
	tail  string
	stats ExecStreamStats
}

// Snapshot 深拷贝头尾并冻结统计，返回值不会被后续Write改变。
func (buffer *ExecOutputBuffer) Snapshot() ExecOutputSnapshot {
	tail := make([]byte, len(buffer.tail))
	n := copy(tail, buffer.tail[buffer.tailNext:])
	copy(tail[n:], buffer.tail[:buffer.tailNext])
	return ExecOutputSnapshot{head: string(buffer.head), tail: string(tail), stats: ExecStreamStats{observed: buffer.observed, retained: int64(len(buffer.head) + len(tail))}}
}

func (snapshot ExecOutputSnapshot) Stats() ExecStreamStats { return snapshot.stats }

// ExecOutputNeedsSaving 在发布对象前判断原始输出是否能完整内联，不生成最终模型结果。
func ExecOutputNeedsSaving(stdout, stderr ExecOutputSnapshot) bool {
	const budget = (ExecModelPreviewBytes - 2048) / 2
	for _, s := range []ExecOutputSnapshot{stdout, stderr} {
		if s.stats.OmittedBytes() > 0 || len(s.head)+len(s.tail) > budget || !utf8.ValidString(s.head+s.tail) {
			return true
		}
	}
	return false
}

// RenderExecOutput 固定按stdout后stderr生成UTF-8预览，并为每个流预留独立预算。
// 无效UTF-8以单字节问号修复，避免修复过程放大输出预算。
func RenderExecOutput(stdout, stderr ExecOutputSnapshot, metadata ExecResultMetadata) (ModelPreview, bool, error) {
	if err := metadata.Validate(); err != nil {
		return ModelPreview{}, false, err
	}
	const streamBudget = (ExecModelPreviewBytes - 2048) / 2
	out, outCut := stdout.render(streamBudget)
	errout, errCut := stderr.render(streamBudget)
	exit := "unknown"
	if metadata.exitKnown {
		exit = fmt.Sprint(metadata.exitCode)
	}
	text := fmt.Sprintf("stdout:\n%s\nstderr:\n%s\nexit_code: %s; timed_out: %t; cancelled: %t\n", out, errout, exit, metadata.timedOut, metadata.cancelled)
	text += renderArtifactSaving("stdout", metadata.StdoutSaving()) + renderArtifactSaving("stderr", metadata.StderrSaving())
	preview, err := NewModelPreview(text)
	return preview, outCut || errCut, err
}

func renderArtifactSaving(stream string, saving ArtifactSaving) string {
	if saving.Status() == ArtifactNotNeeded {
		return ""
	}
	if d, ok := saving.Descriptor(); ok {
		return fmt.Sprintf("%s saved_output: %s; artifact_id: %s; saved_bytes: %d; observed_bytes: %d; reason: %s\nUse ReadArtifact to read the saved prefix.\n", stream, saving.Status(), d.ID(), d.SavedBytes(), d.ObservedBytes(), saving.Reason())
	}
	return fmt.Sprintf("%s saved_output: unavailable; reason: %s\n", stream, saving.Reason())
}

func (snapshot ExecOutputSnapshot) render(budget int) (string, bool) {
	// 连续捕获时先合并再修复，避免恰好跨内部头尾边界的UTF-8被损坏。
	if snapshot.stats.OmittedBytes() == 0 && len(snapshot.head)+len(snapshot.tail) <= budget {
		return strings.ToValidUTF8(snapshot.head+snapshot.tail, "?"), false
	}
	const markerBudget = 96
	contentBudget := budget - markerBudget
	head := []byte(snapshot.head)
	tail := []byte(snapshot.tail)
	if snapshot.stats.OmittedBytes() == 0 {
		all := append(bytes.Clone(head), tail...)
		head, tail = all[:min(len(all), contentBudget/2)], all[max(0, len(all)-(contentBudget-contentBudget/2)):]
	} else {
		head = head[:min(len(head), contentBudget/2)]
		tail = tail[max(0, len(tail)-(contentBudget-contentBudget/2)):]
	}
	// 截断边界只保留完整码点；无效原始字节由统一修复规则处理。
	for len(head) > 0 && !utf8.Valid(head) {
		_, size := utf8.DecodeLastRune(head)
		if size != 1 || head[len(head)-1] < utf8.RuneSelf {
			break
		}
		head = head[:len(head)-1]
	}
	for len(tail) > 0 && !utf8.RuneStart(tail[0]) {
		tail = tail[1:]
	}
	omitted := snapshot.stats.observed - int64(len(head)+len(tail))
	marker := fmt.Sprintf("\n[... %d bytes omitted ...]\n", omitted)
	return strings.ToValidUTF8(string(head), "?") + marker + strings.ToValidUTF8(string(tail), "?"), true
}
