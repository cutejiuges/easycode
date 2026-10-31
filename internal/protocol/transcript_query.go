package protocol

import (
	"context"
	"errors"
	"unicode/utf8"

	"easycode/internal/codec"
	"easycode/internal/domain"
)

// TranscriptCursor 使用原生提交 seq 与提交内位置，状态更新不改变游标。
type TranscriptCursor struct {
	Sequence uint64 `json:"sequence"`
	Item     uint64 `json:"item"`
}

func (c TranscriptCursor) Valid() bool { return c.Sequence > 0 }
func (c TranscriptCursor) Before(other TranscriptCursor) bool {
	return c.Sequence < other.Sequence || (c.Sequence == other.Sequence && c.Item < other.Item)
}

// TranscriptListQuery 的零 Before 指向最新逻辑页。
type TranscriptListQuery struct {
	ThreadID domain.ThreadID  `json:"thread_id"`
	Before   TranscriptCursor `json:"before"`
	Newer    bool             `json:"newer"`
	Through  uint64           `json:"through"`
	Limit    uint32           `json:"limit"`
}

func (q TranscriptListQuery) Validate() error {
	if !q.ThreadID.Valid() || q.Limit == 0 || q.Limit > 50 || (q.Before.Sequence == 0 && q.Before.Item != 0) {
		return errors.New("invalid transcript list query")
	}
	return nil
}
func NewTranscriptListQuery(q TranscriptListQuery) (TranscriptListQuery, error) {
	return q, q.Validate()
}
func DecodeTranscriptListQuery(data []byte) (TranscriptListQuery, error) {
	return decodeTranscriptValue[TranscriptListQuery](data)
}

// TranscriptListItem 只携带短摘要，完整正文必须通过范围查询获取。
type TranscriptListItem struct {
	CompactionID       domain.CompactionID   `json:"compaction_id,omitempty"`
	Preview            string                `json:"preview"`
	Stdout             ToolOutputSummary     `json:"stdout"`
	Stderr             ToolOutputSummary     `json:"stderr"`
	Search             ToolOutputSummary     `json:"search"`
	ModelShortened     bool                  `json:"model_shortened"`
	CoverageIncomplete bool                  `json:"coverage_incomplete"`
	SummaryOnly        bool                  `json:"summary_only"`
	Cursor             TranscriptCursor      `json:"cursor"`
	Kind               domain.TranscriptKind `json:"kind"`
	TurnID             domain.TurnID         `json:"turn_id"`
	InputID            domain.InputID        `json:"input_id"`
	Text               string                `json:"text"`
	More               bool                  `json:"more"`
	Activity           ToolActivityPayload   `json:"activity"`
}

func (v TranscriptListItem) Validate() error {
	if len(v.Preview) > 4096 || !utf8.ValidString(v.Preview) {
		return errors.New("invalid transcript preview")
	}
	if v.Kind != domain.TranscriptCall && (v.Preview != "" || v.Stdout != (ToolOutputSummary{}) || v.Stderr != (ToolOutputSummary{}) || v.Search != (ToolOutputSummary{}) || v.ModelShortened || v.CoverageIncomplete || v.SummaryOnly) {
		return errors.New("invalid text transcript metadata")
	}
	if v.Stdout.Validate() != nil || v.Stderr.Validate() != nil || v.Search.Validate() != nil || !v.Cursor.Valid() || len(v.Text) > 512 || !utf8.ValidString(v.Text) {
		return errors.New("invalid transcript list item")
	}
	if v.Kind == domain.TranscriptCompaction {
		if !v.CompactionID.Valid() || v.TurnID != "" || v.InputID != "" || v.Activity != (ToolActivityPayload{}) {
			return errors.New("invalid compaction transcript item")
		}
		return nil
	}
	if !v.TurnID.Valid() || v.CompactionID != "" {
		return errors.New("invalid transcript identity")
	}
	switch v.Kind {
	case domain.TranscriptUser:
		if !v.InputID.Valid() || v.Activity != (ToolActivityPayload{}) {
			return errors.New("invalid user transcript item")
		}
	case domain.TranscriptAssistant:
		if v.InputID != "" || v.Activity != (ToolActivityPayload{}) {
			return errors.New("invalid assistant transcript item")
		}
	case domain.TranscriptCall:
		if v.InputID != "" || v.Text != "" || v.More {
			return errors.New("invalid call transcript item")
		}
		if v.Activity.Tool != ToolShell && (v.Stdout != (ToolOutputSummary{}) || v.Stderr != (ToolOutputSummary{})) {
			return errors.New("invalid command output metadata")
		}
		if v.Activity.Tool != ToolGlob && v.Activity.Tool != ToolGrep && (v.Search != (ToolOutputSummary{}) || v.CoverageIncomplete) {
			return errors.New("invalid search output metadata")
		}
		if v.SummaryOnly && v.Activity.Tool != ToolPatch && v.Activity.Tool != ToolWrite {
			return errors.New("invalid change summary metadata")
		}
		kind := EventToolTerminal
		if v.Activity.Status == "waiting" {
			kind = EventToolReady
		} else if v.Activity.Status == "running" {
			kind = EventToolStarted
		}
		return validateToolActivity(v.Activity, kind)
	default:
		return errors.New("invalid transcript item kind")
	}
	return nil
}
func NewTranscriptListItem(v TranscriptListItem) (TranscriptListItem, error) { return v, v.Validate() }
func DecodeTranscriptListItem(data []byte) (TranscriptListItem, error) {
	return decodeTranscriptValue[TranscriptListItem](data)
}

// TranscriptListPage 按稳定位置倒序返回最多五十项，供主线或详细记录复用。
type TranscriptListPage struct {
	Items      []TranscriptListItem `json:"items"`
	NextBefore TranscriptCursor     `json:"next_before"`
}

func (p TranscriptListPage) Validate() error {
	if len(p.Items) > 50 || (p.NextBefore.Sequence == 0 && p.NextBefore.Item != 0) {
		return errors.New("invalid transcript list page")
	}
	for i, item := range p.Items {
		if item.Validate() != nil || (i > 0 && !item.Cursor.Before(p.Items[i-1].Cursor)) {
			return errors.New("invalid transcript page order")
		}
	}
	if p.NextBefore.Valid() && (len(p.Items) == 0 || p.NextBefore != p.Items[len(p.Items)-1].Cursor) {
		return errors.New("invalid transcript next cursor")
	}
	return nil
}
func NewTranscriptListPage(p TranscriptListPage) (TranscriptListPage, error) {
	p.Items = append([]TranscriptListItem(nil), p.Items...)
	return p, p.Validate()
}
func DecodeTranscriptListPage(data []byte) (TranscriptListPage, error) {
	return decodeTranscriptValue[TranscriptListPage](data)
}

// TranscriptBodySource 区分用户/assistant 正文、工具输入摘要和冻结结果。
type TranscriptBodySource string

const (
	TranscriptText   TranscriptBodySource = "text"
	TranscriptInput  TranscriptBodySource = "input"
	TranscriptResult TranscriptBodySource = "result"
)

func (s TranscriptBodySource) Valid() bool {
	return s == TranscriptText || s == TranscriptInput || s == TranscriptResult
}

type TranscriptBodyQuery struct {
	ThreadID domain.ThreadID      `json:"thread_id"`
	Cursor   TranscriptCursor     `json:"cursor"`
	Source   TranscriptBodySource `json:"source"`
	Offset   uint64               `json:"offset"`
	Backward bool                 `json:"backward"`
	Limit    uint32               `json:"limit"`
}

func (q TranscriptBodyQuery) Validate() error {
	if !q.ThreadID.Valid() || !q.Cursor.Valid() || !q.Source.Valid() || q.Limit == 0 || q.Limit > 64<<10 {
		return errors.New("invalid transcript body query")
	}
	return nil
}
func NewTranscriptBodyQuery(q TranscriptBodyQuery) (TranscriptBodyQuery, error) {
	return q, q.Validate()
}
func DecodeTranscriptBodyQuery(data []byte) (TranscriptBodyQuery, error) {
	return decodeTranscriptValue[TranscriptBodyQuery](data)
}

// TranscriptBodyPage 保持 UTF-8 边界与实际正文偏移，不以屏幕行号分页。
type TranscriptBodyPage struct {
	Offset     uint64 `json:"offset"`
	NextOffset uint64 `json:"next_offset"`
	TotalBytes uint64 `json:"total_bytes"`
	Text       string `json:"text"`
}

func (p TranscriptBodyPage) Validate() error {
	if p.Offset > p.NextOffset || p.NextOffset > p.TotalBytes || p.NextOffset-p.Offset != uint64(len(p.Text)) || len(p.Text) > 64<<10 || !utf8.ValidString(p.Text) || (p.Offset == p.NextOffset && p.NextOffset != p.TotalBytes) {
		return errors.New("invalid transcript body page")
	}
	return nil
}
func NewTranscriptBodyPage(p TranscriptBodyPage) (TranscriptBodyPage, error) { return p, p.Validate() }
func DecodeTranscriptBodyPage(data []byte) (TranscriptBodyPage, error) {
	return decodeTranscriptValue[TranscriptBodyPage](data)
}

// TranscriptQueries 是唯一浏览端口；不会产生 Provider 请求或执行工具。
type TranscriptQueries interface {
	ListTranscript(context.Context, TranscriptListQuery) (TranscriptListPage, error)
	ReadTranscriptBody(context.Context, TranscriptBodyQuery) (TranscriptBodyPage, error)
	ReadToolOutput(context.Context, ToolOutputQuery) (ToolOutputPage, error)
}

func decodeTranscriptValue[T interface{ Validate() error }](data []byte) (T, error) {
	var value T
	if err := codec.UnmarshalRecord(data, &value); err != nil {
		return value, err
	}
	return value, value.Validate()
}
