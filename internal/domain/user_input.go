package domain

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// MaxUserInputBytes 限制一条真实用户输入的 UTF-8 正文字节数。
const MaxUserInputBytes = 4 << 20

// MaxPendingInputs 限制一批用户输入及尚未解决的输入数量。
const MaxPendingInputs = 64

// InputID 标识一次接收的用户输入，与宿主命令和模型 item 身份分离。
type InputID string

// ParseInputID 校验并返回 canonical UUIDv7 输入标识。
func ParseInputID(value string) (InputID, error) {
	if _, err := parseUUIDv7(value); err != nil {
		return "", fmt.Errorf("invalid input ID: %w", err)
	}
	return InputID(value), nil
}

// Valid 判断输入标识是否为 canonical UUIDv7。
func (id InputID) Valid() bool {
	_, err := ParseInputID(string(id))
	return err == nil
}

// UserInput 是保留原始正文的不可变用户输入。
type UserInput struct {
	id   InputID
	text string
}

// NewUserInput 纯内存校验身份和正文，不规范化或裁剪文本。
func NewUserInput(id InputID, text string) (UserInput, error) {
	input := UserInput{id: id, text: text}
	if err := input.Validate(); err != nil {
		return UserInput{}, err
	}
	return input, nil
}

// ID 返回用户输入身份。
func (input UserInput) ID() InputID { return input.id }

// Text 返回未经显示转义的用户正文。
func (input UserInput) Text() string { return input.text }

// Validate 拒绝零值、空白和超限正文，错误不包含用户文本。
func (input UserInput) Validate() error {
	if !input.id.Valid() {
		return fmt.Errorf("input ID is invalid")
	}
	return ValidateUserText(input.text)
}

// ValidateUserText 是宿主接收与原生提交共用的文本约束。
func ValidateUserText(text string) error {
	if !utf8.ValidString(text) || strings.TrimSpace(text) == "" {
		return fmt.Errorf("input text is invalid")
	}
	if len(text) > MaxUserInputBytes {
		return fmt.Errorf("input text exceeds the byte limit")
	}
	return nil
}

// InputPlacement 区分 turn 初始输入和工具安全点追加输入。
type InputPlacement string

const (
	InputInitial  InputPlacement = "initial"
	InputSteering InputPlacement = "steering"
)

// InputAssociation 是不含正文的有序输入提交关联。
type InputAssociation struct {
	turnID      TurnID
	placement   InputPlacement
	sampleIndex uint32
	ids         []InputID
}

// NewInputAssociation 创建不与调用方共享 ID 切片的提交关联。
func NewInputAssociation(turnID TurnID, placement InputPlacement, sampleIndex uint32, ids []InputID) (InputAssociation, error) {
	association := InputAssociation{turnID: turnID, placement: placement, sampleIndex: sampleIndex, ids: append([]InputID(nil), ids...)}
	if err := association.Validate(); err != nil {
		return InputAssociation{}, err
	}
	return association, nil
}

// TurnID 返回本次输入实际归属的 turn。
func (association InputAssociation) TurnID() TurnID { return association.turnID }

// Placement 返回初始或安全点追加的位置。
func (association InputAssociation) Placement() InputPlacement { return association.placement }

// SampleIndex 返回输入提交后目标 sample 的零起始索引。
func (association InputAssociation) SampleIndex() uint32 { return association.sampleIndex }

// IDs 返回按 admission 顺序排列的独立输入身份副本。
func (association InputAssociation) IDs() []InputID {
	return append([]InputID(nil), association.ids...)
}

// Validate 校验封闭位置与非重复输入身份，不推断 journal 状态。
func (association InputAssociation) Validate() error {
	if !association.turnID.Valid() {
		return fmt.Errorf("input association turn ID is invalid")
	}
	switch association.placement {
	case InputInitial:
		if association.sampleIndex != 0 {
			return fmt.Errorf("initial input sample index is invalid")
		}
	case InputSteering:
		if association.sampleIndex == 0 {
			return fmt.Errorf("steering input sample index is invalid")
		}
	default:
		return fmt.Errorf("input placement is invalid")
	}
	if len(association.ids) == 0 || len(association.ids) > MaxPendingInputs {
		return fmt.Errorf("input association count is invalid")
	}
	seen := make(map[InputID]struct{}, len(association.ids))
	for _, id := range association.ids {
		if !id.Valid() {
			return fmt.Errorf("input association ID is invalid")
		}
		if _, exists := seen[id]; exists {
			return fmt.Errorf("input association ID is duplicated")
		}
		seen[id] = struct{}{}
	}
	return nil
}

// NativeCommitRole 表达 Session grammar 确定的原生提交职责，不包含 wire 数据。
type NativeCommitRole string

const (
	NativeCommitUserInputs  NativeCommitRole = "user_inputs"
	NativeCommitSample      NativeCommitRole = "sample"
	NativeCommitToolOutputs NativeCommitRole = "tool_outputs"
)
