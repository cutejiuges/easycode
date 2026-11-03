package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// RequestPurpose 区分普通模型采样和独立摘要。
type RequestPurpose string

const (
	RequestSample  RequestPurpose = "sample"
	RequestSummary RequestPurpose = "summary"
)

// NativeRelation 表示本次有效原生上下文与已提交基线的关系。
type NativeRelation string

const (
	NativeFirst    NativeRelation = "first"
	NativeAppend   NativeRelation = "append"
	NativeReplaced NativeRelation = "replaced"
	NativeBoundary NativeRelation = "boundary"
)

// RequestTransport 表示实际确认采用的传输。
type RequestTransport string

const (
	TransportHTTP      RequestTransport = "http_sse"
	TransportWebSocket RequestTransport = "websocket"
)

// RequestMode 表示完整或连接内增量发送。
type RequestMode string

const (
	RequestFull        RequestMode = "full"
	RequestIncremental RequestMode = "incremental"
)

// RequestResetReason 是续写选择的封闭原因。
type RequestResetReason string

const (
	ResetNone       RequestResetReason = "none"
	ResetFirst      RequestResetReason = "first_request"
	ResetRestored   RequestResetReason = "restored_history"
	ResetPrefix     RequestResetReason = "prefix_changed"
	ResetProperties RequestResetReason = "request_properties_changed"
	ResetConnection RequestResetReason = "connection_reset"
	ResetCompacted  RequestResetReason = "context_compacted"
	ResetSummary    RequestResetReason = "summary"
)

// CacheFallbackReason 表示生成请求发送前的安全回退。
type CacheFallbackReason string

const (
	FallbackNone      CacheFallbackReason = "none"
	FallbackHandshake CacheFallbackReason = "handshake_unsupported"
	FallbackConnect   CacheFallbackReason = "connect_unavailable"
)

// RequestReceipt 是不含正文、凭据或真实缓存身份的不可变收据值。
type RequestReceipt struct {
	Family             ProviderFamily
	Purpose            RequestPurpose
	Policy             CachePolicy
	RequestFingerprint string
	NativeFingerprint  string
	Relation           NativeRelation
	Transport          RequestTransport
	Mode               RequestMode
	Reason             RequestResetReason
	Fallback           CacheFallbackReason
	Markers            uint8
}

// NewRequestReceipt 校验并复制单次实际请求的证据。
func NewRequestReceipt(draft RequestReceipt) (RequestReceipt, error) {
	if err := draft.Validate(); err != nil {
		return RequestReceipt{}, err
	}
	return draft, nil
}

// Validate 校验收据内部组合及固定摘要大小。
func (r RequestReceipt) Validate() error {
	if err := r.Policy.ValidateFamily(r.Family); err != nil {
		return err
	}
	if r.Purpose != RequestSample && r.Purpose != RequestSummary {
		return fmt.Errorf("invalid request purpose")
	}
	if !ValidFingerprint(r.RequestFingerprint) || !ValidFingerprint(r.NativeFingerprint) {
		return fmt.Errorf("invalid request fingerprint")
	}
	switch r.Relation {
	case NativeFirst, NativeAppend, NativeReplaced, NativeBoundary:
	default:
		return fmt.Errorf("invalid native relation")
	}
	switch r.Reason {
	case ResetNone, ResetFirst, ResetRestored, ResetPrefix, ResetProperties, ResetConnection, ResetCompacted, ResetSummary:
	default:
		return fmt.Errorf("invalid request reset reason")
	}
	switch r.Fallback {
	case FallbackNone, FallbackHandshake, FallbackConnect:
	default:
		return fmt.Errorf("invalid fallback reason")
	}
	if r.Transport != TransportHTTP && r.Transport != TransportWebSocket {
		return fmt.Errorf("invalid request transport")
	}
	if r.Transport == TransportWebSocket && (r.Family != ProviderOpenAI || r.Policy.Transport() != OpenAIWebSocketAuto) {
		return fmt.Errorf("websocket receipt requires enabled OpenAI transport")
	}
	if r.Mode != RequestFull && r.Mode != RequestIncremental {
		return fmt.Errorf("invalid request mode")
	}
	if r.Mode == RequestIncremental && (r.Family != ProviderOpenAI || r.Transport != TransportWebSocket || r.Purpose != RequestSample || r.Reason != ResetNone) {
		return fmt.Errorf("invalid incremental request receipt")
	}
	if r.Family == ProviderAnthropic && (r.Transport != TransportHTTP || r.Markers > 4) {
		return fmt.Errorf("invalid Anthropic request receipt")
	}
	if r.Family == ProviderOpenAI && r.Markers != 0 {
		return fmt.Errorf("OpenAI receipt contains Anthropic markers")
	}
	if r.Family == ProviderAnthropic && r.Policy.Anthropic() == AnthropicCacheDisabled && r.Markers != 0 {
		return fmt.Errorf("disabled cache contains markers")
	}
	if r.Fallback != FallbackNone && (r.Transport != TransportHTTP || r.Policy.Transport() != OpenAIWebSocketAuto || r.Family != ProviderOpenAI) {
		return fmt.Errorf("invalid fallback receipt")
	}
	return nil
}

// ContentFingerprint 只计算给定确定字节的内容摘要，不接触外部状态。
func ContentFingerprint(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

// ValidFingerprint 检查固定长度的小写 SHA-256 摘要。
func ValidFingerprint(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, c := range value {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
