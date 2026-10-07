// Package provider 定义共享 Provider Kernel 契约，不承载具体 wire 结构。
package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"sync/atomic"

	"easycode/internal/domain"
	"easycode/internal/protocol"
)

const MaxNativeCommitBytes = 16 << 20

// Capabilities 描述服务端实际支持的能力，不能仅由厂商名称推断。
type Capabilities struct {
	Streaming          bool
	ReasoningSummary   bool
	RawReasoning       bool
	EncryptedReasoning bool
	ThinkingSignature  bool
	FunctionTools      bool
	CustomTools        bool
	ParallelToolCalls  bool
	PromptCacheControl bool
	PromptCacheKey     bool
	PreviousResponse   bool
}

// TurnInput 是共享 turn 模板交给 Provider Kernel 的最小输入。
type TurnInput struct {
	Text string

	projectInstructions    domain.ProjectInstructionsSnapshot
	hasProjectInstructions bool
}

// WithProjectInstructions 返回附着了项目指令快照的输入副本。
func (input TurnInput) WithProjectInstructions(
	snapshot domain.ProjectInstructionsSnapshot,
) (TurnInput, error) {
	clone, err := snapshot.Clone()
	if err != nil {
		return TurnInput{}, fmt.Errorf("project instructions snapshot is invalid: %w", err)
	}
	input.projectInstructions = clone
	input.hasProjectInstructions = true
	return input, nil
}

// ProjectInstructions 返回不与输入共享可变内存的项目指令快照。
func (input TurnInput) ProjectInstructions() (domain.ProjectInstructionsSnapshot, bool, error) {
	if !input.hasProjectInstructions {
		return domain.ProjectInstructionsSnapshot{}, false, nil
	}
	clone, err := input.projectInstructions.Clone()
	if err != nil {
		return domain.ProjectInstructionsSnapshot{}, false, fmt.Errorf("project instructions snapshot is invalid: %w", err)
	}
	return clone, true, nil
}

// NativeItem 是 provider 原生 item 的只读领域边界。
// 具体字段必须留在 anthropic/openai 子包的强类型结构中。
type NativeItem interface {
	ProviderFamily() domain.ProviderFamily
	ItemKind() string
}

// StreamEvent 是 Provider 流中封闭构造的只读事件。
type StreamEvent struct {
	kind     StreamEventKind
	semantic protocol.Event
	native   NativeItem
	prepared *PreparedSample
	err      error
}

// StreamEventKind 区分普通流事件与恰好一次的终态事件。
type StreamEventKind string

const (
	StreamEventSemantic  StreamEventKind = "semantic"
	StreamEventNative    StreamEventKind = "native_item"
	StreamEventCompleted StreamEventKind = "completed"
	StreamEventFailed    StreamEventKind = "failed"
	StreamEventCancelled StreamEventKind = "cancelled"
)

// Terminal 判断事件是否结束当前 provider stream。
func (kind StreamEventKind) Terminal() bool {
	return kind == StreamEventCompleted || kind == StreamEventFailed || kind == StreamEventCancelled
}

// NewSemanticStreamEvent 创建 Provider 可发布的共享语义事件。
func NewSemanticStreamEvent(event protocol.Event) (StreamEvent, error) {
	streamEvent := StreamEvent{kind: StreamEventSemantic, semantic: cloneProtocolEvent(event)}
	if err := streamEvent.Validate(); err != nil {
		return StreamEvent{}, err
	}
	return streamEvent, nil
}

// NewNativeStreamEvent 创建 Provider 原生 item 事件。
func NewNativeStreamEvent(item NativeItem) (StreamEvent, error) {
	streamEvent := StreamEvent{kind: StreamEventNative, native: item}
	if err := streamEvent.Validate(); err != nil {
		return StreamEvent{}, err
	}
	return streamEvent, nil
}

// NewCompletedStreamEvent 创建携带待 durable 提交 sample 的成功终态。
func NewCompletedStreamEvent(sample *PreparedSample) (StreamEvent, error) {
	streamEvent := StreamEvent{kind: StreamEventCompleted, prepared: sample}
	if err := streamEvent.Validate(); err != nil {
		return StreamEvent{}, err
	}
	return streamEvent, nil
}

// NewFailedStreamEvent 创建失败终态。
func NewFailedStreamEvent(err error) (StreamEvent, error) {
	streamEvent := StreamEvent{kind: StreamEventFailed, err: err}
	if validateErr := streamEvent.Validate(); validateErr != nil {
		return StreamEvent{}, validateErr
	}
	return streamEvent, nil
}

// NewCancelledStreamEvent 创建取消终态。
func NewCancelledStreamEvent(err error) (StreamEvent, error) {
	streamEvent := StreamEvent{kind: StreamEventCancelled, err: err}
	if validateErr := streamEvent.Validate(); validateErr != nil {
		return StreamEvent{}, validateErr
	}
	return streamEvent, nil
}

// Kind 返回事件类型。
func (event StreamEvent) Kind() StreamEventKind {
	return event.kind
}

// Semantic 返回语义事件的独立副本，其他 kind 返回零值。
func (event StreamEvent) Semantic() protocol.Event {
	if event.kind != StreamEventSemantic {
		return protocol.Event{}
	}
	return cloneProtocolEvent(event.semantic)
}

// NativeItem 返回原生 item，其他 kind 返回 nil。
func (event StreamEvent) NativeItem() NativeItem {
	if event.kind != StreamEventNative {
		return nil
	}
	return event.native
}

// PreparedSample 返回成功终态携带的待提交 sample，其他 kind 返回 nil。
func (event StreamEvent) PreparedSample() *PreparedSample {
	if event.kind != StreamEventCompleted {
		return nil
	}
	return event.prepared
}

// Error 返回失败或取消终态携带的错误，其他 kind 返回 nil。
func (event StreamEvent) Error() error {
	if event.kind != StreamEventFailed && event.kind != StreamEventCancelled {
		return nil
	}
	return event.err
}

// Validate 校验事件 kind 与 payload 的唯一合法组合。
func (event StreamEvent) Validate() error {
	switch event.kind {
	case StreamEventSemantic:
		if eventHasNoSemantic(event) || protocolEventEmpty(event.semantic) {
			return fmt.Errorf("semantic stream event payload is invalid")
		}
		if event.semantic.Kind != protocol.EventAssistantTextDelta {
			return fmt.Errorf("semantic stream event kind is invalid")
		}
		if err := event.semantic.Validate(); err != nil {
			return fmt.Errorf("semantic stream event is invalid: %w", err)
		}
	case StreamEventNative:
		if !protocolEventEmpty(event.semantic) || event.prepared != nil || event.err != nil || nativeItemNil(event.native) {
			return fmt.Errorf("native stream event payload is invalid")
		}
		if !event.native.ProviderFamily().Valid() || strings.TrimSpace(event.native.ItemKind()) == "" {
			return fmt.Errorf("native stream event item is invalid")
		}
	case StreamEventCompleted:
		if !protocolEventEmpty(event.semantic) || !nativeItemNil(event.native) || event.prepared == nil || event.err != nil {
			return fmt.Errorf("completed stream event payload is invalid")
		}
		if _, err := event.prepared.Envelope(); err != nil {
			return fmt.Errorf("completed stream event sample is invalid: %w", err)
		}
		if _, err := event.prepared.Usage(); err != nil {
			return fmt.Errorf("completed stream event sample is invalid: %w", err)
		}
	case StreamEventFailed, StreamEventCancelled:
		if !protocolEventEmpty(event.semantic) || !nativeItemNil(event.native) || event.prepared != nil || streamErrorNil(event.err) {
			return fmt.Errorf("terminal stream event payload is invalid")
		}
	default:
		return fmt.Errorf("stream event kind is invalid")
	}
	return nil
}

func eventHasNoSemantic(event StreamEvent) bool {
	return !nativeItemNil(event.native) || event.prepared != nil || event.err != nil
}

func protocolEventEmpty(event protocol.Event) bool {
	return event.Version == 0 && event.Kind == "" && event.Timestamp.IsZero() &&
		event.SessionID == "" && event.ThreadID == "" && event.TurnID == "" &&
		event.ItemID == "" && event.CallID == "" && len(event.Payload) == 0
}

func cloneProtocolEvent(event protocol.Event) protocol.Event {
	event.Payload = append(json.RawMessage(nil), event.Payload...)
	return event
}

func nativeItemNil(item NativeItem) bool {
	if item == nil {
		return true
	}
	value := reflect.ValueOf(item)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

func streamErrorNil(err error) bool {
	if err == nil {
		return true
	}
	value := reflect.ValueOf(err)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

// HistoryProjector 将 Provider 原生历史单向投影为只读语义快照。
type HistoryProjector interface {
	ProjectHistory() domain.SemanticHistoryView
}

// HistoryFootprinter 返回 Provider 已提交原生历史的不透明数值摘要。
type HistoryFootprinter interface {
	HistoryFootprint() (domain.NativeHistoryFootprint, error)
}

// Conversation 是 Runtime 依赖的会话级 provider 接口。
type Conversation interface {
	HistoryProjector
	HistoryFootprinter
	Family() domain.ProviderFamily
	Capabilities() Capabilities
	Stream(context.Context, TurnInput) (<-chan StreamEvent, error)
}

// NativeCommitEnvelope 是共享层可复制但不可解释的 Provider 原生历史增量。
type NativeCommitEnvelope struct {
	family         domain.ProviderFamily
	wire           string
	payloadVersion int
	payload        json.RawMessage
}

// NewNativeCommitEnvelope 校验公共字段并深拷贝 opaque payload。
func NewNativeCommitEnvelope(
	family domain.ProviderFamily,
	wire string,
	payloadVersion int,
	payload json.RawMessage,
) (NativeCommitEnvelope, error) {
	if !family.Valid() {
		return NativeCommitEnvelope{}, fmt.Errorf("native commit provider family is invalid")
	}
	if strings.TrimSpace(wire) == "" {
		return NativeCommitEnvelope{}, fmt.Errorf("native commit wire is required")
	}
	if payloadVersion <= 0 {
		return NativeCommitEnvelope{}, fmt.Errorf("native commit payload version is invalid")
	}
	if len(payload) == 0 || len(payload) > MaxNativeCommitBytes || !json.Valid(payload) {
		return NativeCommitEnvelope{}, fmt.Errorf("native commit payload is invalid")
	}
	return NativeCommitEnvelope{
		family: family, wire: wire, payloadVersion: payloadVersion,
		payload: append(json.RawMessage(nil), payload...),
	}, nil
}

// Family 返回 envelope 所属 Provider 家族。
func (envelope NativeCommitEnvelope) Family() domain.ProviderFamily {
	return envelope.family
}

// Wire 返回 envelope 所属 Provider wire。
func (envelope NativeCommitEnvelope) Wire() string {
	return envelope.wire
}

// PayloadVersion 返回 Provider-owned payload revision。
func (envelope NativeCommitEnvelope) PayloadVersion() int {
	return envelope.payloadVersion
}

// Payload 返回 opaque payload 的独立副本。
func (envelope NativeCommitEnvelope) Payload() json.RawMessage {
	return append(json.RawMessage(nil), envelope.payload...)
}

// Clone 校验当前状态并返回不共享可变 payload buffer 的 envelope。
func (envelope NativeCommitEnvelope) Clone() (NativeCommitEnvelope, error) {
	return NewNativeCommitEnvelope(
		envelope.family, envelope.wire, envelope.payloadVersion, envelope.payload,
	)
}

// PreparedSample 封装一次已校验增量及其恰好一次的纯内存 finalizer。
type PreparedSample struct {
	envelope NativeCommitEnvelope
	usage    domain.SampleUsage
	finalize func()
	state    atomic.Uint32
}

// NewPreparedSample 创建尚未进入 Conversation committed history 的 sample。
func NewPreparedSample(
	envelope NativeCommitEnvelope,
	usage domain.SampleUsage,
	finalize func(),
) (*PreparedSample, error) {
	cloned, err := envelope.Clone()
	if err != nil {
		return nil, err
	}
	if err := usage.Validate(); err != nil {
		return nil, fmt.Errorf("prepared sample usage is invalid: %w", err)
	}
	if finalize == nil {
		return nil, fmt.Errorf("prepared sample finalizer is required")
	}
	return &PreparedSample{envelope: cloned, usage: usage, finalize: finalize}, nil
}

// Envelope 返回待持久化原生增量的独立副本。
func (sample *PreparedSample) Envelope() (NativeCommitEnvelope, error) {
	if sample == nil || sample.finalize == nil {
		return NativeCommitEnvelope{}, fmt.Errorf("prepared sample is invalid")
	}
	if sample.state.Load() != 0 {
		return NativeCommitEnvelope{}, fmt.Errorf("prepared sample is already finalized")
	}
	return sample.envelope.Clone()
}

// Usage 返回待持久化 normalized usage 的值拷贝。
func (sample *PreparedSample) Usage() (domain.SampleUsage, error) {
	if sample == nil || sample.finalize == nil {
		return domain.SampleUsage{}, fmt.Errorf("prepared sample is invalid")
	}
	if sample.state.Load() != 0 {
		return domain.SampleUsage{}, fmt.Errorf("prepared sample is already finalized")
	}
	if err := sample.usage.Validate(); err != nil {
		return domain.SampleUsage{}, fmt.Errorf("prepared sample usage is invalid: %w", err)
	}
	return sample.usage, nil
}

// Finalize 在 durable success 后恰好一次提交纯内存历史。
func (sample *PreparedSample) Finalize() error {
	if sample == nil || sample.finalize == nil {
		return fmt.Errorf("prepared sample is invalid")
	}
	if !sample.state.CompareAndSwap(0, 1) {
		return fmt.Errorf("prepared sample is already finalized")
	}
	sample.finalize()
	return nil
}

// Finalized 判断 sample 是否已经提交到内存历史。
func (sample *PreparedSample) Finalized() bool {
	return sample != nil && sample.state.Load() != 0
}

// Factory 创建相互隔离的会话级 Provider Conversation。
type Factory interface {
	Family() domain.ProviderFamily
	Capabilities() Capabilities
	NewConversation() Conversation
	RestoreConversation([]NativeCommitEnvelope) (Conversation, error)
}
