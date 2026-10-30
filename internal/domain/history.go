package domain

import (
	"fmt"
	"unicode/utf8"
)

// SemanticHistoryView 是只读、允许有损的原生历史投影，不得用于构造 Provider 请求。
type SemanticHistoryView struct {
	Provider ProviderFamily `json:"provider"`
	Turns    []SemanticTurn `json:"turns"`
}

// SemanticRole 区分用户正文和已提交的 assistant 可见正文。
type SemanticRole string

const (
	SemanticUser      SemanticRole = "user"
	SemanticAssistant SemanticRole = "assistant"
)

// SemanticMessage 保留 turn 内消息顺序；InputID 只用于用户消息关联。
type SemanticMessage struct {
	Role    SemanticRole `json:"role"`
	InputID InputID      `json:"input_id,omitempty"`
	Text    string       `json:"text"`
}

// NewSemanticUserMessage 创建有身份的用户正文投影。
func NewSemanticUserMessage(id InputID, text string) (SemanticMessage, error) {
	m := SemanticMessage{Role: SemanticUser, InputID: id, Text: text}
	return m, m.Validate()
}

// NewSemanticAssistantMessage 创建不包含原生签名或私密字段的模型文本投影。
func NewSemanticAssistantMessage(text string) (SemanticMessage, error) {
	m := SemanticMessage{Role: SemanticAssistant, Text: text}
	return m, m.Validate()
}

// Validate 校验封闭角色与输入身份，允许没有可见正文的消息。
func (m SemanticMessage) Validate() error {
	if !utf8.ValidString(m.Text) {
		return fmt.Errorf("semantic message text is invalid")
	}
	switch m.Role {
	case SemanticUser:
		if !m.InputID.Valid() {
			return fmt.Errorf("semantic user identity is invalid")
		}
	case SemanticAssistant:
		if m.InputID != "" {
			return fmt.Errorf("semantic assistant has an input identity")
		}
	default:
		return fmt.Errorf("semantic message role is invalid")
	}
	return nil
}

// SemanticTurn 将有序可见消息关联到真实 turn，不按 user/assistant 对推断边界。
type SemanticTurn struct {
	TurnID   TurnID            `json:"turn_id"`
	Messages []SemanticMessage `json:"messages"`
}

// NewSemanticTurn 创建独立的有序消息快照。
func NewSemanticTurn(id TurnID, messages []SemanticMessage) (SemanticTurn, error) {
	turn := SemanticTurn{TurnID: id, Messages: append([]SemanticMessage{}, messages...)}
	if err := turn.Validate(); err != nil {
		return SemanticTurn{}, err
	}
	return turn, nil
}

// Validate 校验 turn 身份、消息角色及同 turn 内重复用户身份。
func (turn SemanticTurn) Validate() error {
	if !turn.TurnID.Valid() || turn.Messages == nil {
		return fmt.Errorf("semantic turn is invalid")
	}
	seen := make(map[InputID]struct{})
	for _, m := range turn.Messages {
		if err := m.Validate(); err != nil {
			return err
		}
		if m.Role == SemanticUser {
			if _, ok := seen[m.InputID]; ok {
				return fmt.Errorf("semantic input ID is duplicated")
			}
			seen[m.InputID] = struct{}{}
		}
	}
	return nil
}

// Clone 返回不共享消息切片的视图。
func (view SemanticHistoryView) Clone() SemanticHistoryView {
	copy := SemanticHistoryView{Provider: view.Provider, Turns: make([]SemanticTurn, len(view.Turns))}
	for i, t := range view.Turns {
		copy.Turns[i] = SemanticTurn{TurnID: t.TurnID, Messages: append([]SemanticMessage{}, t.Messages...)}
	}
	return copy
}
