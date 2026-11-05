package tool

import (
	"fmt"
	"strings"
)

// QuestionResultSummary 生成主线摘要，不把完整问卷复制到活动事件。
func QuestionResultSummary(result InvocationResult) string {
	if result.Status() != ResultSuccess {
		return ActivityText(result.Preview().Text(), 160)
	}
	m := result.QuestionMetadata()
	return fmt.Sprintf("Answered %d · Skipped %d", m.AnsweredCount, m.SkippedCount)
}

// QuestionInputText 从已保存题目生成完整只读详情，不访问当前环境。
func QuestionInputText(input QuestionInput) string {
	var b strings.Builder
	for i, q := range input.Questions() {
		fmt.Fprintf(&b, "%d. %s [%s]\n%s\n", i+1, q.Header, q.ID, q.Question)
		for index, option := range q.Options {
			fmt.Fprintf(&b, "  %d. %s\n     %s\n", index+1, option.Label, option.Description)
		}
		if q.MultiSelect {
			b.WriteString("Multiple choices allowed\n")
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// QuestionResultText 从冻结答案生成可分页的安全展示源，保留原始说明。
func QuestionResultText(input QuestionInput, result InvocationResult) string {
	if result.Status() != ResultSuccess {
		return result.Preview().Text()
	}
	a, err := result.QuestionAnswers()
	if err != nil || a.ValidateAgainst(input) != nil {
		return "Question result is unavailable"
	}
	var b strings.Builder
	questions := input.Questions()
	for i, answer := range a.Answers() {
		fmt.Fprintf(&b, "%s\n", questions[i].Question)
		if answer.Skipped {
			b.WriteString("  Skipped\n")
		} else {
			for _, index := range answer.SelectedOptions {
				fmt.Fprintf(&b, "  %s\n", questions[i].Options[index].Label)
			}
			if answer.Text != "" {
				b.WriteString(answer.Text)
				b.WriteByte('\n')
			}
		}
		b.WriteByte('\n')
	}
	return b.String()
}
