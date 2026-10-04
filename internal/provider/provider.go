// Package provider 定义共享 Provider Kernel 契约，不承载具体 wire 结构。
package provider

import (
	"context"
	"encoding/json"
	"fmt"
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
}

// NativeItem 是 provider 原生 item 的只读领域边界。
// 具体字段必须留在 anthropic/openai 子包的强类型结构中。
type NativeItem interface {
	ProviderFamily() domain.ProviderFamily
	ItemKind() string
}

// StreamEvent 同时携带共享语义事件和可选原生完成项。
type StreamEvent struct {
	Kind     StreamEventKind
	Event    protocol.Event
	Native   NativeItem
	Prepared *PreparedSample
	Err      error
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
