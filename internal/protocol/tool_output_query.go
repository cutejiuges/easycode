package protocol

import (
	"errors"
	"unicode/utf8"

	"easycode/internal/domain"
)

// ToolOutputSource 标识宿主查询的命令输出流。
type ToolOutputSource string

const (
	ToolStdout       ToolOutputSource = "stdout"
	ToolStderr       ToolOutputSource = "stderr"
	ToolSearchResult ToolOutputSource = "search_result"
)

// ToolOutputSummary 分别描述保存完整性与预览省略，不暴露私有路径。
type ToolOutputSummary struct {
	Status        string `json:"status"`
	Reason        string `json:"reason"`
	SavedBytes    int64  `json:"saved_bytes"`
	ObservedBytes int64  `json:"observed_bytes"`
	Complete      bool   `json:"complete"`
	Readable      bool   `json:"readable"`
}

// ToolOutputQuery 只使用当前线程的命令身份，不接受任意路径或 artifact ID。
type ToolOutputQuery struct {
	ThreadID     domain.ThreadID  `json:"thread_id"`
	InvocationID string           `json:"invocation_id"`
	Stream       ToolOutputSource `json:"stream"`
	Backward     bool             `json:"backward"`
	Offset       uint64           `json:"offset"`
	Limit        uint64           `json:"limit"`
}

func (q ToolOutputQuery) Validate() error {
	if !q.ThreadID.Valid() || !validUUIDv7Text(q.InvocationID) || (q.Stream != ToolStdout && q.Stream != ToolStderr && q.Stream != ToolSearchResult) || q.Limit == 0 || q.Limit > 65536 {
		return errors.New("invalid command output query")
	}
	return nil
}

// ToolOutputPage 的偏移以原始 bytes 计量，Text 为已校验范围的展示副本。
type ToolOutputPage struct {
	Offset         uint64 `json:"offset"`
	NextOffset     uint64 `json:"next_offset"`
	StoredBytes    uint64 `json:"stored_bytes"`
	StoredEOF      bool   `json:"stored_eof"`
	SourceComplete bool   `json:"source_complete"`
	Text           string `json:"text"`
}

// Validate 验证展示摘要，不授权对象访问。
func (s ToolOutputSummary) Validate() error {
	if s == (ToolOutputSummary{}) {
		return nil
	}
	validReason := false
	switch s.Reason {
	case "quota_exceeded", "stream_limit", "write_failed", "sync_failed", "publish_failed", "output_incomplete", "storage_unavailable":
		validReason = true
	}
	if (s.Status == "complete" && s.Reason != "") || ((s.Status == "partial" || s.Status == "unavailable") && !validReason) {
		return errors.New("invalid saved output reason")
	}
	switch s.Status {
	case "not_needed":
		if s.Reason == "" && !s.Readable && s.SavedBytes == 0 && s.ObservedBytes >= 0 && !s.Complete {
			return nil
		}
	case "complete", "partial":
		if s.Readable && s.SavedBytes > 0 && s.SavedBytes <= 16<<20 && s.ObservedBytes >= s.SavedBytes && s.Complete == (s.Status == "complete") {
			return nil
		}
	case "unavailable":
		if !s.Readable && s.SavedBytes == 0 && s.ObservedBytes >= 0 && !s.Complete {
			return nil
		}
	}
	return errors.New("invalid tool output summary")
}

func NewToolOutputQuery(q ToolOutputQuery) (ToolOutputQuery, error) { return q, q.Validate() }
func DecodeToolOutputQuery(data []byte) (ToolOutputQuery, error) {
	return decodeTranscriptValue[ToolOutputQuery](data)
}
func (p ToolOutputPage) Validate() error {
	if uint64(len(p.Text)) < p.NextOffset-p.Offset || uint64(len(p.Text)) > 4*(p.NextOffset-p.Offset) || p.Offset > p.NextOffset || p.NextOffset > p.StoredBytes || p.NextOffset-p.Offset > 64<<10 || p.StoredEOF != (p.NextOffset == p.StoredBytes) || len(p.Text) > 256<<10 || !utf8.ValidString(p.Text) || (p.Offset == p.NextOffset && !p.StoredEOF) {
		return errors.New("invalid tool output page")
	}
	return nil
}
func NewToolOutputPage(p ToolOutputPage) (ToolOutputPage, error) { return p, p.Validate() }
func DecodeToolOutputPage(data []byte) (ToolOutputPage, error) {
	return decodeTranscriptValue[ToolOutputPage](data)
}
