package provider

import (
	"easycode/internal/domain"
	"fmt"
)

// RestoreRecord 将 opaque 原生增量绑定到已校验的 turn 和提交角色。
type RestoreRecord struct {
	turnID      domain.TurnID
	role        domain.NativeCommitRole
	association domain.InputAssociation
	envelope    NativeCommitEnvelope
}

// NewInputRestoreRecord 创建必须具有完整输入关联的恢复记录。
func NewInputRestoreRecord(association domain.InputAssociation, envelope NativeCommitEnvelope) (RestoreRecord, error) {
	if err := association.Validate(); err != nil {
		return RestoreRecord{}, err
	}
	cloned, err := envelope.Clone()
	if err != nil {
		return RestoreRecord{}, err
	}
	a, err := domain.NewInputAssociation(association.TurnID(), association.Placement(), association.SampleIndex(), association.IDs())
	if err != nil {
		return RestoreRecord{}, err
	}
	return RestoreRecord{turnID: a.TurnID(), role: domain.NativeCommitUserInputs, association: a, envelope: cloned}, nil
}

// NewSampleRestoreRecord 创建模型 sample 的恢复记录。
func NewSampleRestoreRecord(turnID domain.TurnID, envelope NativeCommitEnvelope) (RestoreRecord, error) {
	return newRestoreRecord(turnID, domain.NativeCommitSample, envelope)
}

// NewToolOutputRestoreRecord 创建工具结果的恢复记录。
func NewToolOutputRestoreRecord(turnID domain.TurnID, envelope NativeCommitEnvelope) (RestoreRecord, error) {
	return newRestoreRecord(turnID, domain.NativeCommitToolOutputs, envelope)
}

func newRestoreRecord(turnID domain.TurnID, role domain.NativeCommitRole, envelope NativeCommitEnvelope) (RestoreRecord, error) {
	if !turnID.Valid() {
		return RestoreRecord{}, fmt.Errorf("restore turn ID is invalid")
	}
	cloned, err := envelope.Clone()
	if err != nil {
		return RestoreRecord{}, err
	}
	return RestoreRecord{turnID: turnID, role: role, envelope: cloned}, nil
}

// TurnID 返回记录所属的真实 turn。
func (record RestoreRecord) TurnID() domain.TurnID { return record.turnID }

// Role 返回回放 grammar 确定的提交角色。
func (record RestoreRecord) Role() domain.NativeCommitRole { return record.role }

// Envelope 返回经校验的独立原生增量。
func (record RestoreRecord) Envelope() (NativeCommitEnvelope, error) {
	if err := record.Validate(); err != nil {
		return NativeCommitEnvelope{}, err
	}
	return record.envelope.Clone()
}

// Association 返回用户输入的独立关联，其他角色返回不存在。
func (record RestoreRecord) Association() (domain.InputAssociation, bool) {
	if record.role != domain.NativeCommitUserInputs {
		return domain.InputAssociation{}, false
	}
	a := record.association
	copy, err := domain.NewInputAssociation(a.TurnID(), a.Placement(), a.SampleIndex(), a.IDs())
	return copy, err == nil
}

// Validate 拒绝零值或不匹配的恢复角色。
func (record RestoreRecord) Validate() error {
	if !record.turnID.Valid() {
		return fmt.Errorf("restore turn ID is invalid")
	}
	if _, err := record.envelope.Clone(); err != nil {
		return err
	}
	switch record.role {
	case domain.NativeCommitUserInputs:
		if record.association.Validate() != nil || record.association.TurnID() != record.turnID {
			return fmt.Errorf("restore input association is invalid")
		}
	case domain.NativeCommitSample, domain.NativeCommitToolOutputs:
		if record.association.TurnID() != "" {
			return fmt.Errorf("restore record has unrelated input association")
		}
	default:
		return fmt.Errorf("restore commit role is invalid")
	}
	return nil
}
