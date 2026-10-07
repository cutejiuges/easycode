package session

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"easycode/internal/domain"
	"easycode/internal/tool"
)

// NativeCommitRecord 将通过 lifecycle 校验的 opaque commit 与顺序绑定。
type NativeCommitRecord struct {
	Sequence uint64
	TurnID   domain.TurnID
	Commit   NativeCommitPayload
}

// SampleUsageRecord 将通过 lifecycle 校验的 normalized usage 与顺序绑定。
type SampleUsageRecord struct {
	Sequence uint64
	TurnID   domain.TurnID
	Usage    domain.SampleUsage
}

// ReplayedTurnState 是语义回放后的 current text turn 终态。
type ReplayedTurnState string

const (
	ReplayedTurnCompleted ReplayedTurnState = "completed"
	ReplayedTurnFailed    ReplayedTurnState = "failed"
)

// ReplayedTurn 保存已收口 turn 的最小 lifecycle 投影。
type ReplayedTurn struct {
	TurnID domain.TurnID
	State  ReplayedTurnState
}

// InterruptedTail 表示唯一允许由应用显式补偿的 committed 活动 turn。
type InterruptedTail struct {
	TurnID   domain.TurnID
	Sequence uint64
}

// ReplayedToolCallState 描述活动turn中一次调用已经durable到达的阶段。
type ReplayedToolCallState string

const (
	ReplayedToolCallReady   ReplayedToolCallState = "ready"
	ReplayedToolCallStarted ReplayedToolCallState = "started"
	ReplayedToolCallResult  ReplayedToolCallState = "result"
)

// ReplayedToolCall 保存恢复所需的typed调用、结果和事实顺序。
type ReplayedToolCall struct {
	ReadySequence   uint64
	StartedSequence uint64
	ResultSequence  uint64
	SampleIndex     uint32
	CallIndex       uint32
	Invocation      tool.ReadInvocation
	State           ReplayedToolCallState
	Result          tool.InvocationResult
}

// ToolRecoveryPlan 描述活动Tool Loop尾部唯一允许的恢复动作输入。
type ToolRecoveryPlan struct {
	turnID               domain.TurnID
	nextSampleIndex      uint32
	calls                []ReplayedToolCall
	toolOutputsCommitted bool
}

// NewToolRecoveryPlan 创建经过完整校验的活动工具turn恢复计划。
func NewToolRecoveryPlan(
	turnID domain.TurnID,
	nextSampleIndex uint32,
	calls []ReplayedToolCall,
	toolOutputsCommitted bool,
) (ToolRecoveryPlan, error) {
	plan := ToolRecoveryPlan{
		turnID: turnID, nextSampleIndex: nextSampleIndex,
		calls: cloneReplayedToolCalls(calls), toolOutputsCommitted: toolOutputsCommitted,
	}
	if err := plan.Validate(); err != nil {
		return ToolRecoveryPlan{}, err
	}
	return plan, nil
}

// TurnID 返回待补偿活动turn的稳定身份。
func (plan ToolRecoveryPlan) TurnID() domain.TurnID { return plan.turnID }

// NextSampleIndex 返回旧turn若未中断时的下一sample序号。
func (plan ToolRecoveryPlan) NextSampleIndex() uint32 { return plan.nextSampleIndex }

// Calls 返回按模型调用顺序排列的独立副本。
func (plan ToolRecoveryPlan) Calls() []ReplayedToolCall {
	return cloneReplayedToolCalls(plan.calls)
}

// ToolOutputsCommitted 表示当前调用组的Provider原生outputs已经durable。
func (plan ToolRecoveryPlan) ToolOutputsCommitted() bool { return plan.toolOutputsCommitted }

// Clone 返回不共享可变slice的恢复计划。
func (plan ToolRecoveryPlan) Clone() ToolRecoveryPlan {
	return ToolRecoveryPlan{
		turnID: plan.turnID, nextSampleIndex: plan.nextSampleIndex,
		calls: cloneReplayedToolCalls(plan.calls), toolOutputsCommitted: plan.toolOutputsCommitted,
	}
}

// Validate 校验恢复计划只能表达ReplayPlanner已经接受的活动尾部。
func (plan ToolRecoveryPlan) Validate() error {
	if !plan.turnID.Valid() || plan.nextSampleIndex == 0 {
		return fmt.Errorf("tool recovery identity is invalid")
	}
	if plan.toolOutputsCommitted {
		if len(plan.calls) != 0 {
			return fmt.Errorf("committed tool outputs cannot retain pending calls")
		}
		return nil
	}
	if len(plan.calls) == 0 {
		return fmt.Errorf("tool recovery calls are missing")
	}
	var lastReadySequence uint64
	for index, call := range plan.calls {
		if call.ReadySequence == 0 || call.SampleIndex+1 != plan.nextSampleIndex ||
			call.CallIndex != uint32(index) || call.Invocation.Validate() != nil {
			return fmt.Errorf("tool recovery call is invalid")
		}
		if call.ReadySequence <= lastReadySequence {
			return fmt.Errorf("tool recovery ready sequence is invalid")
		}
		lastReadySequence = call.ReadySequence
	}
	lifecycleSequence := lastReadySequence
	pendingSeen := false
	for _, call := range plan.calls {
		switch call.State {
		case ReplayedToolCallReady:
			pendingSeen = true
			if call.StartedSequence != 0 || call.ResultSequence != 0 {
				return fmt.Errorf("ready tool recovery call has later facts")
			}
		case ReplayedToolCallStarted:
			if pendingSeen || call.StartedSequence <= lifecycleSequence || call.ResultSequence != 0 {
				return fmt.Errorf("started tool recovery call sequence is invalid")
			}
			pendingSeen = true
			lifecycleSequence = call.StartedSequence
		case ReplayedToolCallResult:
			if pendingSeen || call.ResultSequence <= lifecycleSequence || call.Result.Validate() != nil ||
				call.Result.InvocationID() != call.Invocation.InvocationID() ||
				call.Result.ProviderCallID() != call.Invocation.ProviderCallID() {
				return fmt.Errorf("result tool recovery call is invalid")
			}
			if call.StartedSequence == 0 {
				if call.Result.Status() != tool.ResultCancelled {
					return fmt.Errorf("tool result before execution start is not cancelled")
				}
			} else if call.StartedSequence <= lifecycleSequence || call.ResultSequence <= call.StartedSequence {
				return fmt.Errorf("result tool recovery call sequence is invalid")
			}
			lifecycleSequence = call.ResultSequence
		default:
			return fmt.Errorf("tool recovery call state is invalid")
		}
	}
	return nil
}

// ReplayPlan 是创建 Provider Conversation 前的不可变语义恢复计划。
type ReplayPlan struct {
	Identity        Identity
	SessionMetadata SessionMetaPayload
	ThreadMetadata  ThreadMetaPayload
	NativeCommits   []NativeCommitRecord
	SampleUsages    []SampleUsageRecord
	Turns           []ReplayedTurn
	OptionalRecords []Record
	InterruptedTail *InterruptedTail
	ToolRecovery    *ToolRecoveryPlan
	NextSequence    uint64
	Repair          RepairReport
}

// ReplayPlanner 在 Loader 之后校验强类型 Session lifecycle。
type ReplayPlanner struct{}

// NewReplayPlanner 创建无副作用的语义回放器。
func NewReplayPlanner() *ReplayPlanner {
	return &ReplayPlanner{}
}

// Plan 校验 metadata cardinality、required placement 和当前文本 turn 状态机。
func (*ReplayPlanner) Plan(loaded LoadResult) (ReplayPlan, error) {
	if len(loaded.Records) < 2 {
		return ReplayPlan{}, replayCorrupt("initial metadata batch is missing")
	}
	batches, err := replayBatches(loaded.Records)
	if err != nil {
		return ReplayPlan{}, err
	}
	if err := validateInitialBatch(batches[0]); err != nil {
		return ReplayPlan{}, err
	}
	sessionMetadata, err := DecodeSessionMetaPayload(batches[0][0])
	if err != nil {
		return ReplayPlan{}, replayCorrupt("session metadata payload is invalid: " + err.Error())
	}
	threadMetadata, err := DecodeThreadMetaPayload(batches[0][1])
	if err != nil {
		return ReplayPlan{}, replayCorrupt("thread metadata payload is invalid")
	}
	identity := Identity{SessionID: batches[0][0].SessionID, ThreadID: batches[0][0].ThreadID}
	if err := validateMetadata(identity, sessionMetadata, threadMetadata, batches[0]); err != nil {
		return ReplayPlan{}, err
	}

	plan := ReplayPlan{
		Identity: identity, SessionMetadata: sessionMetadata, ThreadMetadata: threadMetadata,
		NativeCommits: make([]NativeCommitRecord, 0), SampleUsages: make([]SampleUsageRecord, 0),
		Turns:           make([]ReplayedTurn, 0),
		OptionalRecords: make([]Record, 0), NextSequence: loaded.NextSequence, Repair: loaded.Repair,
	}
	var active *activeReplayTurn
	seenInvocations := make(map[tool.InvocationID]struct{})
	for _, batch := range batches[1:] {
		known, optional, filterErr := filterReplayBatch(batch)
		if filterErr != nil {
			return ReplayPlan{}, filterErr
		}
		plan.OptionalRecords = append(plan.OptionalRecords, optional...)
		if len(known) == 0 {
			continue
		}
		for _, record := range known {
			if record.ParentThreadID != "" {
				return ReplayPlan{}, replayCorrupt("root thread record contains a parent thread")
			}
			if record.EventKind == EventSessionMeta || record.EventKind == EventThreadMeta {
				return ReplayPlan{}, replayCorrupt("session metadata is duplicated")
			}
		}

		switch known[0].EventKind {
		case EventTurnStarted:
			if active != nil || len(known) != 1 || known[0].TurnID == "" {
				return ReplayPlan{}, replayCorrupt("turn_started placement is invalid")
			}
			if _, decodeErr := DecodeTurnStartedPayload(known[0]); decodeErr != nil {
				return ReplayPlan{}, replayCorrupt("turn_started payload is invalid")
			}
			active = &activeReplayTurn{
				turnID: known[0].TurnID, startSequence: known[0].Sequence, lastSequence: known[0].Sequence,
				providerCallIDs: make(map[tool.ProviderCallID]struct{}),
			}
		case EventProviderNativeCommit:
			if active == nil || !batchHasTurn(known, active.turnID) {
				return ReplayPlan{}, replayCorrupt("provider native commit placement is invalid")
			}
			if len(known) == 1 {
				if err := acceptToolOutputs(&plan, active, known[0]); err != nil {
					return ReplayPlan{}, err
				}
				break
			}
			completed, err := acceptSampleBatch(&plan, active, known, seenInvocations)
			if err != nil {
				return ReplayPlan{}, err
			}
			if completed {
				plan.Turns = append(plan.Turns, ReplayedTurn{TurnID: active.turnID, State: ReplayedTurnCompleted})
				active = nil
			}
		case EventToolExecutionStarted:
			if active == nil || len(known) != 1 || known[0].TurnID != active.turnID {
				return ReplayPlan{}, replayCorrupt("tool_execution_started placement is invalid")
			}
			if err := active.acceptStarted(known[0]); err != nil {
				return ReplayPlan{}, err
			}
		case EventToolCallResult:
			if active == nil || len(known) != 1 || known[0].TurnID != active.turnID {
				return ReplayPlan{}, replayCorrupt("tool_call_result placement is invalid")
			}
			if err := active.acceptResult(known[0]); err != nil {
				return ReplayPlan{}, err
			}
		case EventTurnFailed:
			if active == nil || len(known) != 1 || known[0].TurnID != active.turnID || active.pendingCalls != nil {
				return ReplayPlan{}, replayCorrupt("turn_failed placement is invalid")
			}
			_, decodeErr := DecodeTurnFailedPayload(known[0])
			if decodeErr != nil {
				return ReplayPlan{}, replayCorrupt("turn_failed payload is invalid")
			}
			plan.Turns = append(plan.Turns, ReplayedTurn{TurnID: active.turnID, State: ReplayedTurnFailed})
			active = nil
		default:
			return ReplayPlan{}, replayCorrupt("session record placement is invalid")
		}
	}
	if active != nil {
		if loaded.Records[len(loaded.Records)-1].Sequence != active.lastSequence {
			return ReplayPlan{}, replayCorrupt("active turn is not the journal tail")
		}
		if !active.hadToolSample {
			plan.InterruptedTail = &InterruptedTail{TurnID: active.turnID, Sequence: active.startSequence}
		} else {
			recovery, recoveryErr := active.recoveryPlan()
			if recoveryErr != nil {
				return ReplayPlan{}, replayCorrupt("tool recovery plan is invalid")
			}
			plan.ToolRecovery = &recovery
		}
	}
	plan.NativeCommits = cloneNativeCommitRecords(plan.NativeCommits)
	plan.OptionalRecords = cloneRecords(plan.OptionalRecords)
	return plan, nil
}

type activeReplayTurn struct {
	turnID               domain.TurnID
	startSequence        uint64
	lastSequence         uint64
	nextSampleIndex      uint32
	pendingCalls         []ReplayedToolCall
	providerCallIDs      map[tool.ProviderCallID]struct{}
	hadToolSample        bool
	toolOutputsCommitted bool
}

func acceptSampleBatch(plan *ReplayPlan, active *activeReplayTurn, batch []Record, seenInvocations map[tool.InvocationID]struct{}) (bool, error) {
	if active.pendingCalls != nil || (active.hadToolSample && !active.toolOutputsCommitted) || len(batch) < 3 ||
		batch[1].EventKind != EventSampleUsage {
		return false, replayCorrupt("sample batch placement is invalid")
	}
	commit, err := DecodeNativeCommitPayload(batch[0])
	if err != nil {
		return false, replayCorrupt("provider native commit payload is invalid")
	}
	usagePayload, err := DecodeSampleUsagePayload(batch[1])
	if err != nil {
		return false, replayCorrupt("sample_usage payload is invalid")
	}
	usage, err := usagePayload.Domain()
	if err != nil {
		return false, replayCorrupt("sample_usage payload is invalid")
	}
	plan.NativeCommits = append(plan.NativeCommits, NativeCommitRecord{
		Sequence: batch[0].Sequence, TurnID: active.turnID, Commit: cloneNativeCommit(commit),
	})
	plan.SampleUsages = append(plan.SampleUsages, SampleUsageRecord{
		Sequence: batch[1].Sequence, TurnID: active.turnID, Usage: usage,
	})
	if len(batch) == 3 && batch[2].EventKind == EventTurnCompleted {
		if _, err := DecodeTurnCompletedPayload(batch[2]); err != nil {
			return false, replayCorrupt("turn_completed payload is invalid")
		}
		return true, nil
	}
	calls := make([]ReplayedToolCall, 0, len(batch)-2)
	for index, record := range batch[2:] {
		if record.EventKind != EventToolCallReady {
			return false, replayCorrupt("tool call sample batch is invalid")
		}
		payload, decodeErr := DecodeToolCallReadyPayload(record)
		if decodeErr != nil || payload.SampleIndex != active.nextSampleIndex || payload.CallIndex != uint32(index) {
			return false, replayCorrupt("tool_call_ready payload or index is invalid")
		}
		invocation, decodeErr := payload.Domain()
		if decodeErr != nil {
			return false, replayCorrupt("tool_call_ready payload is invalid")
		}
		if _, exists := seenInvocations[invocation.InvocationID()]; exists {
			return false, replayCorrupt("tool invocation ID is duplicated")
		}
		if _, exists := active.providerCallIDs[invocation.ProviderCallID()]; exists {
			return false, replayCorrupt("provider call ID is duplicated")
		}
		seenInvocations[invocation.InvocationID()] = struct{}{}
		active.providerCallIDs[invocation.ProviderCallID()] = struct{}{}
		calls = append(calls, ReplayedToolCall{
			ReadySequence: record.Sequence, SampleIndex: payload.SampleIndex, CallIndex: payload.CallIndex,
			Invocation: invocation, State: ReplayedToolCallReady,
		})
	}
	active.pendingCalls = calls
	active.hadToolSample = true
	active.toolOutputsCommitted = false
	active.nextSampleIndex++
	active.lastSequence = batch[len(batch)-1].Sequence
	return false, nil
}

func acceptToolOutputs(plan *ReplayPlan, active *activeReplayTurn, record Record) error {
	if active.pendingCalls == nil || active.toolOutputsCommitted {
		return replayCorrupt("tool output commit placement is invalid")
	}
	for _, call := range active.pendingCalls {
		if call.State != ReplayedToolCallResult {
			return replayCorrupt("tool output commit appears before all results")
		}
	}
	commit, err := DecodeNativeCommitPayload(record)
	if err != nil {
		return replayCorrupt("provider native commit payload is invalid")
	}
	plan.NativeCommits = append(plan.NativeCommits, NativeCommitRecord{
		Sequence: record.Sequence, TurnID: active.turnID, Commit: cloneNativeCommit(commit),
	})
	active.pendingCalls = nil
	active.toolOutputsCommitted = true
	active.lastSequence = record.Sequence
	return nil
}

func (active *activeReplayTurn) acceptStarted(record Record) error {
	if active.pendingCalls == nil || active.toolOutputsCommitted {
		return replayCorrupt("tool_execution_started has no pending call")
	}
	payload, err := DecodeToolExecutionStartedPayload(record)
	if err != nil {
		return replayCorrupt("tool_execution_started payload is invalid")
	}
	for index := range active.pendingCalls {
		call := &active.pendingCalls[index]
		if call.State == ReplayedToolCallResult {
			continue
		}
		if call.State != ReplayedToolCallReady || call.Invocation.InvocationID() != payload.InvocationID {
			return replayCorrupt("tool execution order is invalid")
		}
		call.State = ReplayedToolCallStarted
		call.StartedSequence = record.Sequence
		active.lastSequence = record.Sequence
		return nil
	}
	return replayCorrupt("tool_execution_started is duplicated")
}

func (active *activeReplayTurn) acceptResult(record Record) error {
	if active.pendingCalls == nil || active.toolOutputsCommitted {
		return replayCorrupt("tool_call_result has no pending call")
	}
	payload, err := DecodeToolCallResultPayload(record)
	if err != nil {
		return replayCorrupt("tool_call_result payload is invalid")
	}
	for index := range active.pendingCalls {
		call := &active.pendingCalls[index]
		if call.State == ReplayedToolCallResult {
			continue
		}
		if call.Invocation.InvocationID() != payload.InvocationID {
			return replayCorrupt("tool result order is invalid")
		}
		result, decodeErr := payload.Domain(call.Invocation)
		if decodeErr != nil {
			return replayCorrupt("tool_call_result does not match ready call")
		}
		if call.State == ReplayedToolCallReady && result.Status() != tool.ResultCancelled {
			return replayCorrupt("tool result before execution start is not cancelled")
		}
		if call.State != ReplayedToolCallReady && call.State != ReplayedToolCallStarted {
			return replayCorrupt("tool result order is invalid")
		}
		call.State = ReplayedToolCallResult
		call.ResultSequence = record.Sequence
		call.Result = result
		active.lastSequence = record.Sequence
		return nil
	}
	return replayCorrupt("tool_call_result is duplicated")
}

func (active *activeReplayTurn) recoveryPlan() (ToolRecoveryPlan, error) {
	return NewToolRecoveryPlan(
		active.turnID, active.nextSampleIndex, active.pendingCalls, active.toolOutputsCommitted,
	)
}

func cloneReplayedToolCalls(calls []ReplayedToolCall) []ReplayedToolCall {
	if calls == nil {
		return nil
	}
	cloned := make([]ReplayedToolCall, len(calls))
	copy(cloned, calls)
	return cloned
}

func batchHasTurn(batch []Record, turnID domain.TurnID) bool {
	for _, record := range batch {
		if record.TurnID != turnID {
			return false
		}
	}
	return true
}

func validateInitialBatch(batch []Record) error {
	if len(batch) != 2 || batch[0].EventKind != EventSessionMeta || batch[1].EventKind != EventThreadMeta ||
		batch[0].BatchID != batch[1].BatchID || batch[0].Sequence != 1 ||
		batch[0].TurnID != "" || batch[1].TurnID != "" ||
		batch[0].ParentThreadID != "" || batch[1].ParentThreadID != "" {
		return replayCorrupt("initial metadata batch is invalid")
	}
	for _, record := range batch {
		if _, exact := LookupDescriptor(record.EventKind, record.PayloadVersion); !exact ||
			record.ReplayRequirement != ReplayRequired {
			return replayCorrupt("initial metadata revision is unsupported")
		}
	}
	return nil
}

func validateMetadata(
	identity Identity,
	sessionMetadata SessionMetaPayload,
	threadMetadata ThreadMetaPayload,
	batch []Record,
) error {
	if identity.SessionID == "" || identity.ThreadID == "" ||
		batch[1].SessionID != identity.SessionID || batch[1].ThreadID != identity.ThreadID ||
		sessionMetadata.RootThreadID != identity.ThreadID || !sessionMetadata.Provider.Valid() ||
		strings.TrimSpace(sessionMetadata.ProviderWire) == "" || strings.TrimSpace(sessionMetadata.Model) == "" ||
		sessionMetadata.SchemaRevision <= 0 || sessionMetadata.CreatedAt.IsZero() ||
		sessionMetadata.CreatedAt.Location() != time.UTC {
		return replayCorrupt("session metadata is invalid")
	}
	if !filepath.IsAbs(sessionMetadata.CreationCWD) || filepath.Clean(sessionMetadata.CreationCWD) != sessionMetadata.CreationCWD {
		return replayCorrupt("session creation cwd is invalid")
	}
	if !threadMetadata.Root || threadMetadata.ParentThreadID != "" {
		return replayCorrupt("root thread metadata is invalid")
	}
	return nil
}

func filterReplayBatch(batch []Record) ([]Record, []Record, error) {
	known := make([]Record, 0, len(batch))
	optional := make([]Record, 0)
	for _, record := range batch {
		if _, exact := LookupDescriptor(record.EventKind, record.PayloadVersion); exact {
			known = append(known, record)
			continue
		}
		if record.ReplayRequirement == ReplayRequired {
			return nil, nil, replayCorrupt("required session payload revision is unsupported")
		}
		optional = append(optional, cloneRecord(record))
	}
	return known, optional, nil
}

func replayBatches(records []Record) ([][]Record, error) {
	batches := make([][]Record, 0)
	for index := 0; index < len(records); {
		size := int(records[index].BatchSize)
		if size <= 0 || index+size > len(records) {
			return nil, replayCorrupt("session batch is incomplete")
		}
		batch := records[index : index+size]
		for offset, record := range batch {
			if record.BatchID != batch[0].BatchID || record.BatchSize != batch[0].BatchSize ||
				record.BatchIndex != uint32(offset) {
				return nil, replayCorrupt("session batch boundary is invalid")
			}
		}
		batches = append(batches, batch)
		index += size
	}
	return batches, nil
}

func cloneNativeCommit(commit NativeCommitPayload) NativeCommitPayload {
	commit.Payload = append([]byte(nil), commit.Payload...)
	return commit
}

func cloneNativeCommitRecords(records []NativeCommitRecord) []NativeCommitRecord {
	cloned := make([]NativeCommitRecord, len(records))
	for index, record := range records {
		cloned[index] = record
		cloned[index].Commit = cloneNativeCommit(record.Commit)
	}
	return cloned
}

func replayCorrupt(message string) error {
	return fmt.Errorf("session replay is corrupted: %s", message)
}
