package tool

import (
	"errors"

	"easycode/internal/codec"
	"easycode/internal/domain"
)

// TaskInputText 从冻结输入恢复真实 facade 的只读详情，不披露内部 profile。
func TaskInputText(input TaskInput) (string, error) {
	if input.Validate() != nil {
		return "", errors.New("invalid task input")
	}
	switch input.Profile() {
	case TaskProfileTodoWrite:
		type todo struct {
			Content    string            `json:"content"`
			ActiveForm string            `json:"activeForm"`
			Status     domain.TaskStatus `json:"status"`
		}
		items := make([]todo, 0, len(input.update.Items()))
		for _, step := range input.update.Items() {
			items = append(items, todo{step.Content(), step.ActiveForm(), step.Status()})
		}
		encoded, err := codec.MarshalStable(struct {
			Todos []todo `json:"todos"`
		}{items})
		return string(encoded), err
	case TaskProfileUpdatePlan:
		type item struct {
			Step   string            `json:"step"`
			Status domain.TaskStatus `json:"status"`
		}
		items := make([]item, 0, len(input.update.Items()))
		for _, step := range input.update.Items() {
			items = append(items, item{step.Content(), step.Status()})
		}
		encoded, err := codec.MarshalStable(struct {
			Explanation string `json:"explanation,omitempty"`
			Plan        []item `json:"plan"`
		}{input.update.Explanation(), items})
		return string(encoded), err
	default:
		return "", errors.New("invalid task input profile")
	}
}

// TaskResultText 只从真实冻结结果生成详情；失败不展示候选为已应用状态。
func TaskResultText(result InvocationResult) (string, error) {
	if result.Capability() != CapabilityTaskProgress || ValidateFrozenResult(result) != nil {
		return "", errors.New("invalid task result")
	}
	wire := struct {
		Status  ResultStatus       `json:"status"`
		Code    string             `json:"code"`
		Preview string             `json:"preview"`
		Update  *domain.TaskUpdate `json:"update,omitempty"`
	}{Status: result.Status(), Code: result.Code(), Preview: result.Preview().Text()}
	if update, applied := result.TaskUpdate(); applied {
		wire.Update = &update
	}
	encoded, err := codec.MarshalStable(wire)
	return string(encoded), err
}
