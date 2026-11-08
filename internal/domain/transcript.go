package domain

import (
	"errors"
	"unicode/utf8"

	"easycode/internal/codec"
)

// TranscriptKind 是展示投影的封闭种类，不承载原生 wire 或执行结果。
type TranscriptKind string

const (
	TranscriptUser       TranscriptKind = "user"
	TranscriptCompaction TranscriptKind = "compaction"
	TranscriptAssistant  TranscriptKind = "assistant"
	TranscriptCall       TranscriptKind = "call"
	TranscriptReasoning  TranscriptKind = "reasoning"
)

// TranscriptPosition 用原生提交序号及提交内位置定位内容，均从零开始。
// Entry 包括没有可见内容的原生提交；宿主将其对应到 journal seq。
type TranscriptPosition struct {
	Entry uint64 `json:"entry"`
	Item  uint64 `json:"item"`
}

// TranscriptItem 只包含允许展示的内容与调用关联；调用名称仅用于核验身份。
type TranscriptItem struct {
	Reasoning *ReasoningBody     `json:"reasoning,omitempty"`
	Skill     SkillAttachment    `json:"skill,omitempty,omitzero"`
	HasSkill  bool               `json:"has_skill,omitempty"`
	Kind      TranscriptKind     `json:"kind"`
	Position  TranscriptPosition `json:"position"`
	TurnID    TurnID             `json:"turn_id"`
	InputID   InputID            `json:"input_id"`
	Text      string             `json:"text"`
	CallID    string             `json:"call_id"`
	CallName  string             `json:"call_name"`
}

// NewTranscriptUser 构造保留原始空白的用户正文投影。
func NewTranscriptUser(position TranscriptPosition, turn TurnID, input InputID, text string) (TranscriptItem, error) {
	v := TranscriptItem{Kind: TranscriptUser, Position: position, TurnID: turn, InputID: input, Text: text}
	return v, v.Validate()
}

// NewTranscriptAssistant 构造不含私有推理的可见文本段。
func NewTranscriptAssistant(position TranscriptPosition, turn TurnID, text string) (TranscriptItem, error) {
	v := TranscriptItem{Kind: TranscriptAssistant, Position: position, TurnID: turn, Text: text}
	return v, v.Validate()
}

// NewTranscriptCall 构造完整调用的锚点，不接收参数或原始 JSON。
func NewTranscriptCall(position TranscriptPosition, turn TurnID, callID, name string) (TranscriptItem, error) {
	v := TranscriptItem{Kind: TranscriptCall, Position: position, TurnID: turn, CallID: callID, CallName: name}
	return v, v.Validate()
}

// NewTranscriptReasoning 构造仅含允许正文或不可用状态的推理项。
func NewTranscriptReasoning(position TranscriptPosition, turn TurnID, body ReasoningBody) (TranscriptItem, error) {
	v := TranscriptItem{Kind: TranscriptReasoning, Position: position, TurnID: turn, Reasoning: &body}
	return v, v.Validate()
}

// Validate 拒绝未知种类、无效身份和跨种类字段，不改变正文。
func (v TranscriptItem) Validate() error {
	if v.HasSkill {
		if v.Kind != TranscriptUser || v.Skill.Validate() != nil {
			return errors.New("invalid transcript skill")
		}
	} else if v.Skill != (SkillAttachment{}) {
		return errors.New("unexpected transcript skill")
	}
	if !v.TurnID.Valid() || !utf8.ValidString(v.Text) {
		return errors.New("invalid transcript item")
	}
	if v.Kind != TranscriptReasoning && v.Reasoning != nil {
		return errors.New("unexpected transcript reasoning")
	}
	switch v.Kind {
	case TranscriptReasoning:
		if v.Reasoning == nil || v.Reasoning.Validate() != nil || v.Text != "" || v.InputID != "" || v.CallID != "" || v.CallName != "" {
			return errors.New("invalid transcript reasoning")
		}
	case TranscriptUser:
		if !v.InputID.Valid() || v.CallID != "" || v.CallName != "" {
			return errors.New("invalid transcript user")
		}
	case TranscriptAssistant:
		if v.InputID != "" || v.CallID != "" || v.CallName != "" {
			return errors.New("invalid transcript assistant")
		}
	case TranscriptCall:
		if v.InputID != "" || v.Text != "" || !validTranscriptIdentity(v.CallID) || !validTranscriptIdentity(v.CallName) {
			return errors.New("invalid transcript call")
		}
	default:
		return errors.New("invalid transcript kind")
	}
	return nil
}

func validTranscriptIdentity(value string) bool {
	return value != "" && len(value) <= 512 && utf8.ValidString(value)
}

// DecodeTranscriptItem 严格解码唯一当前展示值，拒绝私有字段与不完整结构。
func DecodeTranscriptItem(data []byte) (TranscriptItem, error) {
	var v TranscriptItem
	fields, err := codec.ObjectMembers(data)
	if err != nil {
		return TranscriptItem{}, errors.New("invalid transcript encoding")
	}
	var position TranscriptPosition
	if err := codec.DecodeObject(fields["position"], &position, "entry", "item"); err != nil {
		return TranscriptItem{}, errors.New("invalid transcript position")
	}
	if err := codec.DecodeObject(data, &v, "kind", "position", "turn_id", "input_id", "text", "call_id", "call_name"); err != nil {
		return TranscriptItem{}, errors.New("invalid transcript encoding")
	}
	if v.Kind == TranscriptReasoning {
		raw, exists := fields["reasoning"]
		if !exists {
			return TranscriptItem{}, errors.New("transcript reasoning is required")
		}
		body, err := DecodeReasoningBody(raw)
		if err != nil {
			return TranscriptItem{}, err
		}
		v.Reasoning = &body
	} else if _, exists := fields["reasoning"]; exists {
		return TranscriptItem{}, errors.New("unexpected transcript reasoning")
	}
	if err := v.Validate(); err != nil {
		return TranscriptItem{}, err
	}
	return v, nil
}

// TranscriptView 是纯内存独立展示快照，不能用于构造 Provider 请求。
type TranscriptView struct {
	Provider ProviderFamily   `json:"provider"`
	Items    []TranscriptItem `json:"items"`
}

// NewTranscriptView 校验顺序并复制条目集合，字符串无需重复复制。
func NewTranscriptView(family ProviderFamily, items []TranscriptItem) (TranscriptView, error) {
	v := TranscriptView{Provider: family, Items: append([]TranscriptItem{}, items...)}
	for i := range v.Items {
		if v.Items[i].Reasoning != nil {
			body := *v.Items[i].Reasoning
			v.Items[i].Reasoning = &body
		}
	}
	if err := v.Validate(); err != nil {
		return TranscriptView{}, err
	}
	return v, nil
}

// Validate 校验稳定顺序以及输入身份唯一性；调用身份由宿主按提交关联。
func (v TranscriptView) Validate() error {
	if !v.Provider.Valid() || v.Items == nil {
		return errors.New("invalid transcript view")
	}
	inputs := make(map[InputID]struct{})
	for i, item := range v.Items {
		if err := item.Validate(); err != nil {
			return err
		}
		if i > 0 {
			previous := v.Items[i-1].Position
			if item.Position.Entry < previous.Entry || (item.Position.Entry == previous.Entry && item.Position.Item <= previous.Item) {
				return errors.New("invalid transcript order")
			}
		}
		if item.Kind == TranscriptUser {
			if _, exists := inputs[item.InputID]; exists {
				return errors.New("duplicate transcript input")
			}
			inputs[item.InputID] = struct{}{}
		}
	}
	return nil
}
