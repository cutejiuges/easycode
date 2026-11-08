package provider

import (
	"context"
	"errors"
	"unicode/utf8"

	"easycode/internal/domain"
	"easycode/internal/protocol"
)

const MaxReasoningMetadataBytes = 1 << 20

// ReasoningBudget 只归一个 reducer 持有，在分配或追加前计算资源。
type ReasoningBudget struct {
	source   int
	nodes    int
	metadata int
	scratch  int
}

// ReserveNode 在创建块、片段或关闭身份之前预留元数据。
func (b *ReasoningBudget) ReserveNode(referenceBytes int) error {
	const nodeBytes = 256
	if referenceBytes < 0 || b.nodes >= domain.MaxReasoningNodes || referenceBytes > MaxReasoningMetadataBytes-nodeBytes-b.metadata {
		return errors.New("reasoning metadata exceeds limit")
	}
	b.nodes++
	b.metadata += nodeBytes + referenceBytes
	return nil
}

// CheckNodeCapacity 在完整快照解码前检查待创建节点，避免先物化超大数组。
func (b *ReasoningBudget) CheckNodeCapacity(additional int) error {
	if additional < 0 || additional > domain.MaxReasoningNodes-b.nodes || additional > (MaxReasoningMetadataBytes-b.metadata)/256 {
		return errors.New("reasoning metadata exceeds limit")
	}
	return nil
}

// ReplaceSource 在替换或追加来源字段前校验总量；重复快照不得再次计费。
func (b *ReasoningBudget) ReplaceSource(oldBytes, newBytes int) error {
	if oldBytes < 0 || newBytes < 0 || oldBytes > b.source || newBytes > domain.MaxReasoningSourceBytes-(b.source-oldBytes) {
		return errors.New("reasoning source exceeds limit")
	}
	b.source += newBytes - oldBytes
	return nil
}

// CheckScratch 在解析和复制完整 wire 快照前检查暂存量。
func (b *ReasoningBudget) CheckScratch(bytes int) error {
	if bytes < 0 || bytes > domain.MaxReasoningSourceBytes-b.scratch {
		return errors.New("reasoning scratch exceeds limit")
	}
	return nil
}

// ReplaceScratch 在替换保留的 wire 快照前检查累计暂存量。
func (b *ReasoningBudget) ReplaceScratch(oldBytes, newBytes int) error {
	if oldBytes < 0 || newBytes < 0 || oldBytes > b.scratch || newBytes > domain.MaxReasoningSourceBytes-(b.scratch-oldBytes) {
		return errors.New("reasoning scratch exceeds limit")
	}
	b.scratch += newBytes - oldBytes
	return nil
}

// ReasoningDraft 是 Provider 内的逻辑输出，发布时才构造成有界共享事件。
type ReasoningDraft struct {
	stage  protocol.ReasoningStage
	block  uint32
	source domain.ReasoningSource
	offset uint64
	text   string
	body   domain.ReasoningBody
}

// NewReasoningStartDraft 创建真实块的开始输出。
func NewReasoningStartDraft(block uint32, source domain.ReasoningSource) (ReasoningDraft, error) {
	if block >= domain.MaxReasoningNodes || !source.Valid() {
		return ReasoningDraft{}, errors.New("invalid reasoning start draft")
	}
	return ReasoningDraft{stage: protocol.ReasoningStarted, block: block, source: source}, nil
}

// NewReasoningTextDraft 引用已确定前缀，不将大文字预先分片为数组。
func NewReasoningTextDraft(block uint32, source domain.ReasoningSource, offset uint64, text string) (ReasoningDraft, error) {
	if block >= domain.MaxReasoningNodes || !source.Valid() || text == "" || !utf8.ValidString(text) || offset > domain.MaxReasoningSourceBytes || uint64(len(text)) > domain.MaxReasoningSourceBytes-offset {
		return ReasoningDraft{}, errors.New("invalid reasoning text draft")
	}
	return ReasoningDraft{stage: protocol.ReasoningDelta, block: block, source: source, offset: offset, text: text}, nil
}

// NewReasoningBodyTextDraft 引用虚拟正文中尚未发布的范围，避免拼接完整前缀。
func NewReasoningBodyTextDraft(block uint32, body domain.ReasoningBody, offset uint64) (ReasoningDraft, error) {
	if block >= domain.MaxReasoningNodes || body.Validate() != nil || offset >= body.Len() {
		return ReasoningDraft{}, errors.New("invalid reasoning body draft")
	}
	return ReasoningDraft{stage: protocol.ReasoningDelta, block: block, source: body.Source(), offset: offset, body: body}, nil
}

// NewReasoningCompletionDraft 引用已完成的不可变允许正文。
func NewReasoningCompletionDraft(block uint32, body domain.ReasoningBody) (ReasoningDraft, error) {
	if block >= domain.MaxReasoningNodes || body.Validate() != nil {
		return ReasoningDraft{}, errors.New("invalid reasoning completion draft")
	}
	return ReasoningDraft{stage: protocol.ReasoningCompleted, block: block, source: body.Source(), body: body}, nil
}

// ReasoningDrafts 持有最多两个逻辑输出，零值表示没有推理输出。
type ReasoningDrafts struct {
	values [2]ReasoningDraft
	count  uint8
}

// NewReasoningDrafts 校验后复制固定大小的逻辑序列。
func NewReasoningDrafts(drafts ...ReasoningDraft) (ReasoningDrafts, error) {
	if len(drafts) > 2 {
		return ReasoningDrafts{}, errors.New("too many reasoning drafts")
	}
	var result ReasoningDrafts
	for i, draft := range drafts {
		if draft.stage == "" {
			return ReasoningDrafts{}, errors.New("invalid empty reasoning draft")
		}
		result.values[i] = draft
	}
	result.count = uint8(len(drafts))
	return result, nil
}

// PublishReasoning 惰性分片并沿原 stream owner 发送，不创建后台工作。
func PublishReasoning(ctx context.Context, output chan<- StreamEvent, drafts ReasoningDrafts) error {
	if ctx == nil || output == nil {
		return errors.New("reasoning publisher scope is invalid")
	}
	send := func(event protocol.Event, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		item, err := NewSemanticStreamEvent(event)
		if err != nil {
			return err
		}
		select {
		case output <- item:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	for i := uint8(0); i < drafts.count; i++ {
		draft := drafts.values[i]
		switch draft.stage {
		case protocol.ReasoningStarted:
			if err := send(protocol.NewReasoningStarted(draft.block, draft.source)); err != nil {
				return err
			}
		case protocol.ReasoningDelta:
			if draft.text == "" {
				for offset := draft.offset; offset < draft.body.Len(); {
					page, err := draft.body.ReadRange(offset, protocol.MaxReasoningDeltaBytes, false)
					if err != nil {
						return err
					}
					if err := send(protocol.NewReasoningDelta(draft.block, draft.source, offset, page.Text)); err != nil {
						return err
					}
					offset = page.NextOffset
				}
				continue
			}
			for offset := 0; offset < len(draft.text); {
				end := min(len(draft.text), offset+protocol.MaxReasoningDeltaBytes)
				for end < len(draft.text) && !utf8.RuneStart(draft.text[end]) {
					end--
				}
				if err := send(protocol.NewReasoningDelta(draft.block, draft.source, draft.offset+uint64(offset), draft.text[offset:end])); err != nil {
					return err
				}
				offset = end
			}
		case protocol.ReasoningCompleted:
			completion := protocol.ReasoningCompletion{Availability: draft.body.Availability(), TotalBytes: draft.body.Len()}
			if completion.Availability == domain.ReasoningReadable {
				page, err := draft.body.ReadRange(draft.body.Len(), protocol.MaxReasoningPreviewBytes, true)
				if err != nil {
					return err
				}
				completion.Offset, completion.Text = page.Offset, page.Text
			}
			if err := send(protocol.NewReasoningCompleted(draft.block, draft.source, completion)); err != nil {
				return err
			}
		default:
			return errors.New("invalid reasoning draft stage")
		}
	}
	return nil
}
