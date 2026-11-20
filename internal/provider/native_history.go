package provider

import (
	"fmt"
	"math"
	"sync"

	"easycode/internal/domain"
)

type nativeClone[E any] interface {
	CloneNativeEntry() E
}

// NativeHistory 持有已提交且不可变的原生条目。适配器决定条目的具体 wire 结构。
type NativeHistory[E nativeClone[E]] struct {
	mu         sync.RWMutex
	entries    []E
	revision   uint64
	checkpoint *CheckpointEnvelope
	offset     int
	count      uint64
	summary    domain.ContextSummary
	native     E
}

// NativeWindow 是可独立使用的原生历史及 checkpoint 快照。
type NativeWindow[E nativeClone[E]] struct {
	Entries    []E
	Revision   uint64
	Checkpoint *CheckpointEnvelope
	Offset     int
	Count      uint64
	Summary    domain.ContextSummary
	native     E
}

// Entries 返回已提交历史中的独立条目。
func (h *NativeHistory[E]) Entries() []E {
	h.mu.RLock()
	entries := h.entries
	h.mu.RUnlock()
	return cloneNativeEntries(entries)
}

// EntriesFrom 返回从指定已提交 ordinal 开始的独立后缀，不复制前缀。
func (h *NativeHistory[E]) EntriesFrom(ordinal uint64) ([]E, error) {
	h.mu.RLock()
	entries := h.entries
	h.mu.RUnlock()
	if ordinal > uint64(len(entries)) {
		return nil, fmt.Errorf("native history ordinal is invalid")
	}
	return cloneNativeEntries(entries[ordinal:]), nil
}

// Snapshot 一次捕获完整窗口，再在锁外复制条目。
func (h *NativeHistory[E]) Snapshot() NativeWindow[E] {
	return h.window().clone()
}

// ActiveSnapshot 只复制 checkpoint 后的条目，并保留同次捕获的修订号与摘要。
func (h *NativeHistory[E]) ActiveSnapshot() NativeWindow[E] {
	window := h.window()
	window.Entries = window.Entries[window.Offset:]
	window.Offset = 0
	return window.clone()
}

func (w NativeWindow[E]) clone() NativeWindow[E] {
	w.Entries = cloneNativeEntries(w.Entries)
	w.native = w.native.CloneNativeEntry()
	return w
}

func (h *NativeHistory[E]) window() NativeWindow[E] {
	h.mu.RLock()
	defer h.mu.RUnlock()
	window := NativeWindow[E]{
		Entries: h.entries, Revision: h.revision, Offset: h.offset,
		Count: h.count, Summary: h.summary, native: h.native,
	}
	if h.checkpoint != nil {
		checkpoint := *h.checkpoint
		window.Checkpoint = &checkpoint
	}
	return window
}

func cloneNativeEntries[E nativeClone[E]](entries []E) []E {
	copied := make([]E, len(entries))
	for i, entry := range entries {
		copied[i] = entry.CloneNativeEntry()
	}
	return copied
}

// Append 发布单条提交，保留单条提交的饱和修订规则。
func (h *NativeHistory[E]) Append(entry E) {
	entry = entry.CloneNativeEntry()
	h.mu.Lock()
	defer h.mu.Unlock()
	h.entries = append(h.entries, entry)
	if h.revision < math.MaxUint64 {
		h.revision++
	}
}

// AppendBatch 原子发布已在准备阶段检查过修订边界的条目批次。
func (h *NativeHistory[E]) AppendBatch(entries []E) {
	copied := cloneNativeEntries(entries)
	h.mu.Lock()
	defer h.mu.Unlock()
	h.entries = append(h.entries, copied...)
	h.revision += uint64(len(copied))
}

// InstallCheckpoint 在当前位置安装已验证的摘要及窗口边界。
func (h *NativeHistory[E]) InstallCheckpoint(checkpoint CheckpointEnvelope, native E, summary domain.ContextSummary) {
	native = native.CloneNativeEntry()
	h.mu.Lock()
	defer h.mu.Unlock()
	h.checkpoint, h.native, h.summary = &checkpoint, native, summary
	h.offset = len(h.entries)
	h.count++
}

// RequestEntries 返回带原生摘要的有效请求条目。
func (w NativeWindow[E]) RequestEntries() []E {
	if w.Checkpoint == nil {
		return w.Entries
	}
	if w.Offset > len(w.Entries) {
		return nil
	}
	entries := make([]E, 0, len(w.Entries)-w.Offset+1)
	entries = append(entries, w.native)
	return append(entries, w.Entries[w.Offset:]...)
}

// ProjectNativeWindow 投影已发布窗口的正文；摘要仅作为独立的展示值。
func ProjectNativeWindow[E nativeClone[E]](w NativeWindow[E], project func([]E) domain.SemanticHistoryView) (domain.ActiveContextView, error) {
	if w.Offset > len(w.Entries) || w.Revision > math.MaxUint64-w.Count {
		return domain.ActiveContextView{}, fmt.Errorf("context window boundary is invalid")
	}
	return domain.NewActiveContextView(project(w.Entries[w.Offset:]), w.Summary, w.Revision+w.Count)
}

// ProjectContextWindow 从同一原生窗口派生语义视图、体量和修订号。
func ProjectContextWindow[E nativeClone[E]](w NativeWindow[E], project func([]E) domain.SemanticHistoryView, footprint func([]E, uint64) (domain.NativeHistoryFootprint, error)) (domain.ActiveContextView, domain.NativeHistoryFootprint, error) {
	view, err := ProjectNativeWindow(w, project)
	if err != nil {
		return domain.ActiveContextView{}, domain.NativeHistoryFootprint{}, err
	}
	size, err := footprint(w.RequestEntries(), view.Revision())
	return view, size, err
}
