package session

import (
	"fmt"
	"time"

	"easycode/internal/domain"
	"easycode/internal/tool"
)

func validateQuestionResultPayload(p ToolCallResultPayload) error {
	if !p.InvocationID.Valid() || p.WireKind != tool.WireStructured || p.QuestionMetadata == nil || p.SkillMetadata != nil || p.ReadArtifactMetadata != nil || p.ReadMetadata != nil || p.GlobMetadata != nil || p.GrepMetadata != nil || p.PatchMetadata != nil || p.WriteMetadata != nil || p.ExecMetadata != nil {
		return fmt.Errorf("invalid question result union")
	}
	preview, err := tool.NewModelPreview(p.Preview)
	if err != nil {
		return fmt.Errorf("invalid question result preview")
	}
	return tool.ValidateQuestionResultShape(p.Status, p.Code, preview, *p.QuestionMetadata, p.Budget)
}

// PreflightQuestionResult 验证将提交的真实结果与记录编码，不分配序号或访问 writer。
func PreflightQuestionResult(identity Identity, turnID domain.TurnID, result tool.InvocationResult) error {
	if result.Capability() != tool.CapabilityQuestion {
		return fmt.Errorf("question result is required")
	}
	draft, err := NewToolCallResultDraft(turnID, result)
	if err != nil {
		return err
	}
	// 用真实身份和最大宽度的信封字段验证大小，不从 writer 预领序号。
	record, err := BuildRecord(identity, draft, ^uint64(0), time.Date(9999, 12, 31, 23, 59, 59, 999999999, time.UTC), ^uint64(0), 0, 1)
	if err != nil {
		return err
	}
	_, _, err = EncodeRecord(record)
	return err
}
