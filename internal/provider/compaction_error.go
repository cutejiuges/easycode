package provider

// CompactionErrorCode 是摘要生成边界的封闭错误分类。
type CompactionErrorCode string

const (
	CompactionBusy            CompactionErrorCode = "session_busy"
	CompactionNothing         CompactionErrorCode = "nothing_to_compact"
	CompactionSourceChanged   CompactionErrorCode = "compaction_source_changed"
	CompactionInvalidResponse CompactionErrorCode = "compaction_invalid_response"
	CompactionRequestFailed   CompactionErrorCode = "compaction_request_failed"
	CompactionCancelled       CompactionErrorCode = "compaction_cancelled"
	CompactionTimedOut        CompactionErrorCode = "compaction_timeout"
)

// CompactionError 只暴露固定代码，禁止携带响应正文或连接凭据。
type CompactionError struct{ code CompactionErrorCode }

// NewCompactionError 创建固定分类的压缩失败。
func NewCompactionError(code CompactionErrorCode) *CompactionError {
	switch code {
	case CompactionBusy, CompactionNothing, CompactionSourceChanged, CompactionInvalidResponse, CompactionRequestFailed, CompactionCancelled, CompactionTimedOut:
		return &CompactionError{code: code}
	default:
		return &CompactionError{code: CompactionInvalidResponse}
	}
}

// Code 返回强类型失败原因。
func (err *CompactionError) Code() CompactionErrorCode { return err.code }

// Error 返回固定英文代码。
func (err *CompactionError) Error() string { return string(err.code) }
