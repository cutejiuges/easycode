package openai

import (
	"encoding/json"
	"sort"

	"easycode/internal/codec"
	"easycode/internal/domain"
	"easycode/internal/fault"
	"easycode/internal/provider"
)

// reasoningItemState 只保留当前 sample 的有界关联，不向宿主暴露 wire 身份。
type reasoningItemState struct {
	index       int
	block       uint32
	parts       map[int]*reasoningPartState
	emitted     uint64
	closed      bool
	sourceBytes int
}
type reasoningPartState struct {
	kind string
	text string
	done bool
}

func reasoningError() error {
	return fault.New(fault.CodeStreamProtocol, "Responses reasoning reference or snapshot is invalid")
}

func (r *responsesStreamReducer) startReasoning(index *int, item NativeItem) (reducerResult, error) {
	if index == nil || *index < 0 || *index >= domain.MaxReasoningNodes || item.ID == "" || r.functions[item.ID] != nil || r.customs[item.ID] != nil || r.reasoning[item.ID] != nil || r.toolIdentityConflicts("", *index) {
		return reducerResult{}, reasoningError()
	}
	for _, state := range r.reasoning {
		if state.index == *index {
			return reducerResult{}, reasoningError()
		}
	}
	if err := r.reasoningBudget.ReserveNode(len(item.ID)); err != nil {
		return reducerResult{}, err
	}
	state := &reasoningItemState{index: *index, block: r.reasoningCount, parts: make(map[int]*reasoningPartState)}
	r.reasoningCount++
	if err := r.seedReasoning(state, item.ReasoningSummary); err != nil {
		return reducerResult{}, err
	}
	r.reasoning[item.ID] = state
	r.reasoningIndexes[state.index] = state
	start, err := provider.NewReasoningStartDraft(state.block, domain.ReasoningOpenAISummary)
	if err != nil {
		return reducerResult{}, err
	}
	body, err := state.prefix()
	if err != nil {
		return reducerResult{}, err
	}
	if body.Len() == 0 {
		drafts, err := provider.NewReasoningDrafts(start)
		return reducerResult{reasoning: drafts}, err
	}
	delta, err := provider.NewReasoningBodyTextDraft(state.block, body, 0)
	if err != nil {
		return reducerResult{}, err
	}
	state.emitted = body.Len()
	drafts, err := provider.NewReasoningDrafts(start, delta)
	return reducerResult{reasoning: drafts}, err
}

func (r *responsesStreamReducer) seedReasoning(state *reasoningItemState, parts []ReasoningSummaryPart) error {
	if len(parts) >= domain.MaxReasoningNodes {
		return reasoningError()
	}
	for i, part := range parts {
		if err := r.reasoningBudget.ReserveNode(len(part.Type)); err != nil {
			return err
		}
		if err := r.reasoningBudget.ReplaceSource(0, len(part.Text)); err != nil {
			return err
		}
		state.sourceBytes += len(part.Text)
		state.parts[i] = &reasoningPartState{kind: part.Type, text: part.Text, done: part.Type != "summary_text"}
	}
	return nil
}

func (state *reasoningItemState) prefix() (domain.ReasoningBody, error) {
	parts := make([]string, 0, len(state.parts))
	for i := 0; i < domain.MaxReasoningNodes; i++ {
		part, ok := state.parts[i]
		if !ok {
			break
		}
		if part.kind == "summary_text" {
			parts = append(parts, part.text)
		}
		if !part.done {
			break
		}
	}
	return domain.NewReasoningBody(domain.ReasoningOpenAISummary, parts)
}

func (r *responsesStreamReducer) reduceReasoningPart(e responsesEventEnvelope) (reducerResult, error) {
	state := r.reasoning[e.ItemID]
	if state == nil || e.OutputIndex == nil || state.index != *e.OutputIndex || e.SummaryIndex == nil || *e.SummaryIndex < 0 || *e.SummaryIndex >= domain.MaxReasoningNodes {
		return reducerResult{}, reasoningError()
	}
	done := e.Type == "response.reasoning_summary_text.done" || e.Type == "response.reasoning_summary_part.done"
	if state.closed {
		if done {
			return reducerResult{}, nil
		}
		return reducerResult{}, reasoningError()
	}
	part := state.parts[*e.SummaryIndex]
	if part != nil && part.kind != "summary_text" {
		return reducerResult{}, reasoningError()
	}
	var text string
	switch e.Type {
	case "response.reasoning_summary_part.added", "response.reasoning_summary_part.done":
		if len(e.Part) == 0 || string(e.Part) == "null" {
			return reducerResult{}, reasoningError()
		}
		if err := r.reasoningBudget.CheckScratch(len(e.Part)); err != nil {
			return reducerResult{}, err
		}
		var wire ReasoningSummaryPart
		if err := codec.Unmarshal(e.Part, &wire); err != nil || wire.Type != "summary_text" {
			return reducerResult{}, reasoningError()
		}
		text = wire.Text
	case "response.reasoning_summary_text.delta":
		if part == nil || part.done || e.Delta == nil {
			return reducerResult{}, reasoningError()
		}
		if err := r.reasoningBudget.ReplaceSource(len(part.text), len(part.text)+len(*e.Delta)); err != nil {
			return reducerResult{}, err
		}
		state.sourceBytes += len(*e.Delta)
		part.text += *e.Delta
	case "response.reasoning_summary_text.done":
		if e.Text == nil {
			return reducerResult{}, reasoningError()
		}
		text = *e.Text
	default:
		return reducerResult{}, reasoningError()
	}
	if e.Type != "response.reasoning_summary_text.delta" {
		if part == nil {
			if err := r.reasoningBudget.ReserveNode(len("summary_text")); err != nil {
				return reducerResult{}, err
			}
			if err := r.reasoningBudget.ReplaceSource(0, len(text)); err != nil {
				return reducerResult{}, err
			}
			state.sourceBytes += len(text)
			part = &reasoningPartState{kind: "summary_text", text: text}
			state.parts[*e.SummaryIndex] = part
		} else {
			if !done || part.text != "" && part.text != text || part.done && part.text != text {
				return reducerResult{}, reasoningError()
			}
			if part.text == "" {
				if err := r.reasoningBudget.ReplaceSource(0, len(text)); err != nil {
					return reducerResult{}, err
				}
				state.sourceBytes += len(text)
				part.text = text
			}
		}
		part.done = done
	}
	body, err := state.prefix()
	if err != nil {
		return reducerResult{}, err
	}
	if body.Len() <= state.emitted {
		return reducerResult{}, nil
	}
	draft, err := provider.NewReasoningBodyTextDraft(state.block, body, state.emitted)
	if err != nil {
		return reducerResult{}, err
	}
	state.emitted = body.Len()
	drafts, err := provider.NewReasoningDrafts(draft)
	return reducerResult{reasoning: drafts}, err
}

func (r *responsesStreamReducer) completeReasoning(index *int, item NativeItem) (reducerResult, error) {
	state := r.reasoning[item.ID]
	itemOnly := state == nil
	if itemOnly {
		if r.functions[item.ID] != nil || r.customs[item.ID] != nil {
			return reducerResult{}, reasoningError()
		}
		if index != nil && (*index < 0 || *index >= domain.MaxReasoningNodes || r.toolIdentityConflicts("", *index)) {
			return reducerResult{}, reasoningError()
		}
		if err := r.reasoningBudget.ReserveNode(len(item.ID)); err != nil {
			return reducerResult{}, err
		}
		resolvedIndex := -1
		if index != nil {
			resolvedIndex = *index
		}
		state = &reasoningItemState{index: resolvedIndex, block: r.reasoningCount, parts: make(map[int]*reasoningPartState)}
		r.reasoningCount++
	} else if state.closed || index == nil || state.index != *index {
		return reducerResult{}, reasoningError()
	}
	if len(item.ReasoningSummary) >= domain.MaxReasoningNodes {
		return reducerResult{}, reasoningError()
	}
	for i, part := range state.parts {
		if i >= len(item.ReasoningSummary) || item.ReasoningSummary[i].Type != part.kind || part.text != "" && part.text != item.ReasoningSummary[i].Text || part.done && part.text != item.ReasoningSummary[i].Text {
			return reducerResult{}, reasoningError()
		}
	}
	source := len(item.EncryptedContent)
	for _, part := range item.ReasoningSummary {
		source += len(part.Text)
	}
	for _, part := range item.Content {
		source += len(part.Text)
	}
	if err := r.reasoningBudget.ReplaceSource(state.sourceBytes, source); err != nil {
		return reducerResult{}, err
	}
	for i := range item.ReasoningSummary {
		if state.parts[i] == nil {
			if err := r.reasoningBudget.ReserveNode(len(item.ReasoningSummary[i].Type)); err != nil {
				return reducerResult{}, err
			}
		}
	}
	if err := r.reasoningBudget.ReplaceScratch(0, len(item.Raw)); err != nil {
		return reducerResult{}, err
	}
	body, err := reasoningBody(item)
	if err != nil {
		return reducerResult{}, err
	}
	complete, err := provider.NewReasoningCompletionDraft(state.block, body)
	if err != nil {
		return reducerResult{}, err
	}
	var drafts provider.ReasoningDrafts
	if itemOnly {
		start, buildErr := provider.NewReasoningStartDraft(state.block, domain.ReasoningOpenAISummary)
		if buildErr != nil {
			return reducerResult{}, buildErr
		}
		drafts, err = provider.NewReasoningDrafts(start, complete)
	} else if body.Len() > state.emitted {
		delta, buildErr := provider.NewReasoningBodyTextDraft(state.block, body, state.emitted)
		if buildErr != nil {
			return reducerResult{}, buildErr
		}
		drafts, err = provider.NewReasoningDrafts(delta, complete)
	} else {
		drafts, err = provider.NewReasoningDrafts(complete)
	}
	if err != nil {
		return reducerResult{}, err
	}
	binding, err := domain.NewReasoningBinding(state.block, uint32(len(r.items)), body.Source(), body.Availability())
	if err != nil {
		return reducerResult{}, err
	}
	r.bindings = append(r.bindings, binding)
	state.closed = true
	state.parts = nil
	if item.ID != "" {
		r.reasoning[item.ID] = state
	}
	if state.index >= 0 {
		r.reasoningIndexes[state.index] = state
	}
	r.items = append(r.items, item)
	return reducerResult{native: &item, reasoning: drafts}, nil
}

// presentation 将块序号与原生完成顺序关联，绝不重排 native items。
func (r *responsesStreamReducer) presentation() (domain.SamplePresentation, error) {
	bindings := append([]domain.ReasoningBinding{}, r.bindings...)
	sort.Slice(bindings, func(i, j int) bool { return bindings[i].Block < bindings[j].Block })
	return domain.NewSamplePresentation(uint32(len(r.items)), bindings)
}

// preflightReasoningItem 在完整 typed item 和 parts 数组分配前扫描关联与节点上限。
func (r *responsesStreamReducer) preflightReasoningItem(raw json.RawMessage) error {
	var header struct {
		Type    string          `json:"type"`
		ID      string          `json:"id"`
		Summary json.RawMessage `json:"summary"`
	}
	if err := r.reasoningBudget.CheckScratch(len(raw)); err != nil {
		return err
	}
	if err := codec.Unmarshal(raw, &header); err != nil {
		return err
	}
	if header.Type != "reasoning" {
		return nil
	}
	state := r.reasoning[header.ID]
	additional := 0
	if state == nil {
		additional++
	}
	if len(header.Summary) == 0 || string(header.Summary) == "null" {
		return r.reasoningBudget.CheckNodeCapacity(additional)
	}
	count, err := codec.CountArrayElements(header.Summary, domain.MaxReasoningNodes)
	if err != nil {
		return err
	}
	for index := 0; index < count; index++ {
		if state == nil || state.parts[index] == nil {
			additional++
		}
	}
	return r.reasoningBudget.CheckNodeCapacity(additional)
}
