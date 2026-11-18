package context

import "easycode/internal/domain"

// WithTaskProgress 绑定当前窗口的已验证任务资料，仅参与历史内容身份，不增加正文或估算。
func (input PlanningInput) WithTaskProgress(state domain.TaskProgressState) (PlanningInput, error) {
	if err := state.Validate(); err != nil {
		return PlanningInput{}, err
	}
	input.taskProgress = state.Clone()
	return input, input.Validate()
}
