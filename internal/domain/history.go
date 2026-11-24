package domain

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// SemanticHistoryView 是只读、允许有损的原生历史投影，不得用于构造 Provider 请求。
type SemanticHistoryView struct {
	Provider           ProviderFamily              `json:"provider"`
	Turns              []SemanticTurn              `json:"turns"`
	CheckpointContexts []SemanticCheckpointContext `json:"checkpoint_contexts,omitempty"`
}

// SemanticCheckpointContext 将补充上下文定位在真实消息之间，不创建人工回合。
type SemanticCheckpointContext struct {
	AfterMessages int                 `json:"after_messages"`
	Context       SessionStartContext `json:"context"`
}

// SemanticRole 区分用户正文和已提交的 assistant 可见正文。
type SemanticRole string

const (
	SemanticUser                SemanticRole = "user"
	SemanticAssistant           SemanticRole = "assistant"
	SemanticHook                SemanticRole = "hook"
	SemanticSessionStartContext SemanticRole = "session_start_context"
)

// SemanticMessage 保留 turn 内消息顺序；InputID 只用于用户消息关联。
type SemanticMessage struct {
	Files        FileAttachments      `json:"files,omitempty,omitzero"`
	HookSource   *HookMessageSource   `json:"hook_source,omitempty"`
	SessionStart *SessionStartContext `json:"session_start,omitempty"`
	Skill        SkillAttachment      `json:"skill,omitempty,omitzero"`
	HasSkill     bool                 `json:"has_skill,omitempty"`
	Role         SemanticRole         `json:"role"`
	InputID      InputID              `json:"input_id,omitempty"`
	Text         string               `json:"text"`
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
	if m.Files.Len() > 0 && (m.Role != SemanticUser || ValidateFileAttachments(m.Text, m.Files) != nil) {
		return fmt.Errorf("invalid semantic file attachments")
	}
	if m.Role != SemanticHook && m.HookSource != nil {
		return fmt.Errorf("unexpected semantic hook source")
	}
	if m.Role != SemanticSessionStartContext && m.SessionStart != nil {
		return fmt.Errorf("unexpected semantic session start context")
	}
	if m.HasSkill {
		if m.Role != SemanticUser || m.Skill.Validate() != nil {
			return fmt.Errorf("invalid semantic skill")
		}
	} else if m.Skill != (SkillAttachment{}) {
		return fmt.Errorf("unexpected semantic skill")
	}
	if !utf8.ValidString(m.Text) {
		return fmt.Errorf("semantic message text is invalid")
	}
	switch m.Role {
	case SemanticUser:
		if !m.InputID.Valid() {
			return fmt.Errorf("semantic user identity is invalid")
		}
	case SemanticHook:
		if m.InputID != "" || m.HookSource == nil || m.HookSource.Validate() != nil {
			return fmt.Errorf("invalid semantic hook source")
		}
	case SemanticSessionStartContext:
		if m.InputID != "" || m.SessionStart == nil || m.SessionStart.Target.Source == SessionStartCompact ||
			m.SessionStart.Validate() != nil || m.Text != sessionStartModelText(*m.SessionStart) {
			return fmt.Errorf("invalid semantic session start context")
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
	turn := SemanticTurn{TurnID: id, Messages: cloneSemanticMessages(messages)}
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
	for i, m := range turn.Messages {
		if m.SessionStart != nil && (i != 0 || m.SessionStart.Target.TurnID != turn.TurnID ||
			i+1 >= len(turn.Messages) || turn.Messages[i+1].Role != SemanticUser) {
			return fmt.Errorf("invalid semantic session start placement")
		}
		if m.HookSource != nil {
			sample, _ := m.HookSource.Identity.Target.AssistantSample()
			if sample.TurnID != turn.TurnID {
				return fmt.Errorf("semantic hook turn mismatch")
			}
		}
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
		copy.Turns[i] = SemanticTurn{TurnID: t.TurnID, Messages: cloneSemanticMessages(t.Messages)}
	}
	for _, context := range view.CheckpointContexts {
		copy.CheckpointContexts = append(copy.CheckpointContexts, SemanticCheckpointContext{
			AfterMessages: context.AfterMessages, Context: context.Context.Clone(),
		})
	}
	return copy
}

// NewSemanticSessionStartContext 保留首次真实输入前的授权来源和精确正文。
func NewSemanticSessionStartContext(value SessionStartContext) (SemanticMessage, error) {
	value = value.Clone()
	message := SemanticMessage{Role: SemanticSessionStartContext, Text: sessionStartModelText(value), SessionStart: &value}
	return message, message.Validate()
}

// ModelText 返回适配器冻结的正文块，不为显示添加字节。
func (context SemanticCheckpointContext) ModelText() string {
	return sessionStartModelText(context.Context)
}

func sessionStartModelText(value SessionStartContext) string {
	var text strings.Builder
	for _, item := range value.Items {
		text.WriteString(item.Text)
	}
	return text.String()
}

// NewSemanticHookMessage 将已授权反馈投影为独立来源，不创建真实用户身份。
func NewSemanticHookMessage(feedback StopFeedback) (SemanticMessage, error) {
	source, err := NewHookMessageSource(feedback)
	if err != nil {
		return SemanticMessage{}, err
	}
	v := SemanticMessage{Role: SemanticHook, Text: feedback.Text, HookSource: &source}
	return v, v.Validate()
}

func cloneSemanticMessages(messages []SemanticMessage) []SemanticMessage {
	out := append([]SemanticMessage{}, messages...)
	for i := range out {
		out[i].Files = out[i].Files.Clone()
		if out[i].HookSource != nil {
			source := *out[i].HookSource
			out[i].HookSource = &source
		}
		if out[i].SessionStart != nil {
			context := out[i].SessionStart.Clone()
			out[i].SessionStart = &context
		}
	}
	return out
}
