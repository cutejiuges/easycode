package session

import (
	"easycode/internal/domain"
	"easycode/internal/tool"
)

// ToolHistoryEntry 保存已验证工具事实的纯内存投影，不保存文件句柄或实时进度。
type ToolHistoryEntry struct {
	ResultSequence    uint64
	PreDetailSequence uint64
	Origin            tool.ToolCallOrigin
	PreHooks          *ReplayedHookGate
	Admission         tool.ExecutionAdmission
	NativeSequence    uint64
	TurnID            domain.TurnID
	SampleIndex       uint32
	CallIndex         uint32
	Sequence          uint64
	Invocation        tool.Invocation
	Result            tool.InvocationResult
	HasResult         bool
	Hooks             []HookHistoryEntry
	PostHooks         *ReplayedPostHooks
	DetailSequence    uint64
}

type replayedToolSample struct {
	nativeSequence uint64
	calls          []ReplayedToolCall
}

// projectToolHistory 只消费已经通过语法校验的调用，保持原生与详情顺序。
func projectToolHistory(samples []replayedToolSample) ([]ToolHistoryEntry, error) {
	entries := make([]ToolHistoryEntry, 0)
	for _, sample := range samples {
		for _, call := range sample.calls {
			admission, err := call.ExecutionAdmission()
			if err != nil {
				return nil, err
			}
			entry := ToolHistoryEntry{
				Origin: call.Origin, NativeSequence: sample.nativeSequence, TurnID: call.Origin.TurnID,
				SampleIndex: call.SampleIndex, CallIndex: call.CallIndex, Sequence: call.ReadySequence,
				Invocation: call.Invocation, Admission: admission, Result: call.Result,
				ResultSequence: call.ResultSequence, HasResult: call.State == ReplayedToolCallResult,
				PreHooks: call.Hooks, PostHooks: call.PostHooks, PreDetailSequence: call.preDetailSequence,
				DetailSequence: max(call.preDetailSequence, call.postDetailSequence),
			}
			if entry.PreHooks != nil {
				entry.appendHookHistory(entry.PreHooks.Finished)
			}
			if entry.PostHooks != nil {
				entry.appendHookHistory(entry.PostHooks.Finished)
			}
			entries = append(entries, entry)
		}
	}
	return entries, nil
}

func (entry *ToolHistoryEntry) appendHookHistory(finished []domain.HookFinished) {
	for _, hook := range finished {
		entry.Hooks = append(entry.Hooks, HookHistoryEntry{
			Reference: hook.Feedback.Reference, Result: hook.Result, Feedback: hook.Feedback,
		})
	}
}

// InputSelection 保留原 Invocation，不把中间 C 覆盖到原始历史。
func (entry ToolHistoryEntry) InputSelection() (tool.InputSelection, error) {
	return SelectToolInput(entry.Origin, entry.Invocation, entry.PreHooks)
}

func (entry ToolHistoryEntry) InputResultEvidence() (tool.InputResultEvidence, error) {
	selection, err := entry.InputSelection()
	if err != nil {
		return tool.InputResultEvidence{}, err
	}
	return inputResultEvidence(selection, entry.Admission, entry.Result)
}

// ToolArtifactReferences 只投影当前线程已提交的授权引用，不访问对象。
func ToolArtifactReferences(thread domain.ThreadID, entries []ToolHistoryEntry) []tool.ArtifactDescriptor {
	var refs []tool.ArtifactDescriptor
	for _, entry := range entries {
		if !entry.HasResult || entry.Origin.ThreadID != thread {
			continue
		}
		refs = append(refs, entry.Result.ArtifactDescriptors()...)
	}
	return refs
}

// HookHistoryEntry 是可重建的安全检查详情，不包含可执行命令或输出正文。
type HookHistoryEntry struct {
	Reference domain.HookReference
	Result    domain.HookRunResult
	Feedback  domain.FrozenHookFeedback
}
