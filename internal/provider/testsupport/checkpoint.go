package testsupport

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	contextplan "easycode/internal/context"
	"easycode/internal/domain"
	"easycode/internal/provider"
)

// CheckpointConversation 的额外方法只由家族包的测试文件实现。
type CheckpointConversation interface {
	RequestConversation
	provider.TranscriptProjector
	PreflightCompaction(provider.CompactionInput) (provider.CompactionPreview, provider.ContextWindowProjection, error)
	TestCheckpoint(domain.CompactionScope, domain.CompactionID, provider.CompactionSource, string, string, domain.TaskProgressState) (*provider.PreparedCompaction, error)
	TestAppendInput(*testing.T, string) provider.RestoreRecord
	TestRestore(*testing.T, []provider.RestoreRecord) (CheckpointConversation, error)
}

// CheckCheckpointWindow 共用 checkpoint 生命周期断言，实际 native 和请求 golden 由两家提供。
func CheckCheckpointWindow(t *testing.T, conversation CheckpointConversation, scope domain.CompactionScope, input provider.TurnInput, compactInput provider.CompactionInput, records []provider.RestoreRecord, usage func(*provider.PreparedCompaction, provider.CheckpointEnvelope)) {
	t.Helper()
	full := conversation.ProjectHistory()
	fullTranscript, _, err := conversation.ProjectTranscriptFrom(0)
	if err != nil {
		t.Fatal(err)
	}
	preview, preflightContext, err := conversation.PreflightCompaction(compactInput)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := conversation.TestCheckpoint(scope, "01900000-0000-7000-8000-000000000001", preview.Source(), "goal; completed tests; next task", "summary-generation-only", domain.NewNoTaskProgress())
	if err != nil {
		t.Fatal(err)
	}
	checkpoint, err := prepared.Checkpoint()
	if err != nil {
		t.Fatal(err)
	}
	usage(prepared, checkpoint)
	_, before, _ := conversation.ProjectContextWindow()
	if before.Revision() != 2 {
		t.Fatal("staging changed committed revision")
	}
	if err := prepared.Finalize(); err != nil {
		t.Fatal(err)
	}
	captured, capturedFootprint, err := preflightContext()
	if err != nil || captured.Revision() != preview.Source().ContextRevision() ||
		capturedFootprint != before || !reflect.DeepEqual(captured.History(), full) {
		t.Fatal("preflight projection did not retain its original committed window", err)
	}
	afterTranscript, _, err := conversation.ProjectTranscriptFrom(0)
	if err != nil || !reflect.DeepEqual(fullTranscript, afterTranscript) {
		t.Fatal("checkpoint changed native transcript ordinals")
	}
	_, after, _ := conversation.ProjectContextWindow()
	if after.Revision() != 3 || !reflect.DeepEqual(full, conversation.ProjectHistory()) {
		t.Fatal("checkpoint changed full history or wrong revision")
	}
	if _, _, err := conversation.PreflightCompaction(compactInput); err == nil {
		t.Fatal("repeated compaction without increments accepted")
	}
	record, err := provider.NewCheckpointRestoreRecord(checkpoint, domain.NewNoTaskProgress())
	if err != nil {
		t.Fatal(err)
	}
	records = append(records, record)
	records = append(records, conversation.TestAppendInput(t, "continue"))
	restoredValue, err := conversation.TestRestore(t, records)
	if err != nil {
		t.Fatal(err)
	}
	restored := restoredValue
	a, err := conversation.TestRequest(input)
	if err != nil {
		t.Fatal(err)
	}
	b, err := restored.TestRequest(input)
	if err != nil {
		t.Fatal(err)
	}
	if (scope.Trigger == domain.CompactionAuto) != strings.Contains(string(a.Bytes()), "Continue the existing task directly") {
		t.Fatal("continuation scope lost")
	}
	if !bytes.Equal(a.Bytes(), b.Bytes()) {
		t.Fatal("restored canonical request differs")
	}
	if strings.Contains(string(a.Bytes()), "old-opaque") || strings.Contains(string(a.Bytes()), "summary-generation-only") || strings.Contains(string(a.Bytes()), "archived work") {
		t.Fatal("archived history or generation provenance entered request")
	}
	if !strings.Contains(string(a.Bytes()), "goal; completed tests; next task") || !strings.Contains(string(a.Bytes()), "continue") {
		t.Fatal("active summary or increment missing")
	}
	av, _ := conversation.ProjectActiveContext()
	bv, _ := restored.ProjectActiveContext()
	if !reflect.DeepEqual(av, bv) || len(av.History().Turns) != 1 {
		t.Fatal("active view restore mismatch")
	}
	_, af, _ := conversation.ProjectContextWindow()
	_, bf, _ := restored.ProjectContextWindow()
	if af != bf {
		t.Fatal("active footprint restore mismatch")
	}
	if !reflect.DeepEqual(conversation.ProjectHistory(), restored.ProjectHistory()) {
		t.Fatal("full history restore mismatch")
	}
	// 第二次 checkpoint 必须绑定前一摘要和新增输入，不能重新覆盖旧窗口来源。
	secondPreview, _, err := conversation.PreflightCompaction(compactInput)
	if err != nil {
		t.Fatal(err)
	}
	if secondPreview.Source().Previous().ID() != checkpoint.ID() {
		t.Fatal("predecessor lost")
	}
	second, err := conversation.TestCheckpoint(domain.ManualCompactionScope(), "01900000-0000-7000-8000-000000000002", secondPreview.Source(), "goal; completed tests; next task", "summary-generation-only", domain.NewNoTaskProgress())
	if err != nil {
		t.Fatal(err)
	}
	secondCheckpoint, _ := second.Checkpoint()
	if err := second.Finalize(); err != nil {
		t.Fatal(err)
	}
	secondRecord, _ := provider.NewCheckpointRestoreRecord(secondCheckpoint, domain.NewNoTaskProgress())
	records = append(records, secondRecord)
	restoredValue, err = conversation.TestRestore(t, records)
	if err != nil {
		t.Fatal(err)
	}
	_, af, _ = conversation.ProjectContextWindow()
	_, bf, _ = restoredValue.ProjectContextWindow()
	if af != bf || af.Revision() != 5 {
		t.Fatal("second checkpoint revision mismatch")
	}
	bad := append([]provider.RestoreRecord{}, records...)
	bad[len(bad)-1] = record
	if value, err := conversation.TestRestore(t, bad); err == nil || value != nil {
		t.Fatal("stale predecessor accepted")
	}
}

// CompactToolHistory 验证两次摘要、失败回合输入与恢复指纹，返回后续原生接线需要的记录。
func CompactToolHistory(t *testing.T, conversation CheckpointConversation, sampling provider.TurnInput, records []provider.RestoreRecord) []provider.RestoreRecord {
	t.Helper()
	original, _, _ := conversation.ProjectTranscriptFrom(0)
	input, _ := provider.NewCompactionInput(sampling, "preserve tool observations", 4096)
	for iteration, id := range []domain.CompactionID{"01900000-0000-7000-8000-000000000080", "01900000-0000-7000-8000-000000000081"} {
		preview, _, err := conversation.PreflightCompaction(input)
		if err != nil {
			t.Fatal(err)
		}
		scope, _ := domain.NewAutoCompactionScope("01900000-0000-7000-8000-000000000099", uint32(iteration+1))
		candidate, err := conversation.TestCheckpoint(scope, id, preview.Source(), "Preserve goal and tests", "summary-opaque", domain.NewNoTaskProgress())
		if err != nil {
			t.Fatal(err)
		}
		checkpoint, _ := candidate.Checkpoint()
		if err := candidate.Finalize(); err != nil {
			t.Fatal(err)
		}
		record, _ := provider.NewCheckpointRestoreRecord(checkpoint, domain.NewNoTaskProgress())
		records = append(records, record)
		restoredValue, err := conversation.TestRestore(t, records)
		if err != nil {
			t.Fatal(err)
		}
		restored := restoredValue
		RestoredRequest(t, conversation, restored, sampling)
		liveView, _ := conversation.ProjectActiveContext()
		restoredView, _ := restored.ProjectActiveContext()
		_, liveFootprint, _ := conversation.ProjectContextWindow()
		_, restoredFootprint, _ := restored.ProjectContextWindow()
		if !reflect.DeepEqual(liveView, restoredView) || liveFootprint != restoredFootprint {
			t.Fatal("tool/steering window changed")
		}
		profile, _ := contextplan.NewProviderProfile(conversation.Family(), "fixture-model")
		catalog, _, _ := sampling.ToolCatalog()
		project, _ := domain.NewProjectInstructionsSnapshot(nil, 32<<10)
		liveInput, err := contextplan.NewPlanningInput(profile, catalog, project, liveView, liveFootprint, contextplan.NewSamplingInput(), contextplan.DisabledBudget())
		if err != nil {
			t.Fatal(err)
		}
		restoredInput, err := contextplan.NewPlanningInput(profile, catalog, project, restoredView, restoredFootprint, contextplan.NewSamplingInput(), contextplan.DisabledBudget())
		if err != nil {
			t.Fatal(err)
		}
		livePlan, err := contextplan.NewPlanner().Plan(liveInput)
		if err != nil {
			t.Fatal(err)
		}
		restoredPlan, err := contextplan.NewPlanner().Plan(restoredInput)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(livePlan.CachePlan(), restoredPlan.CachePlan()) {
			t.Fatal("restored checkpoint fingerprints changed")
		}

		transcript, _, _ := restored.ProjectTranscriptFrom(0)
		if iteration == 0 && !reflect.DeepEqual(original, transcript) {
			t.Fatal("tool cursor or frozen preview changed")
		}
		if iteration == 0 {
			// 已提交输入即使没有后续成功 sample，仍必须被下一次压缩覆盖。
			records = append(records, conversation.TestAppendInput(t, "failed turn input retained"))
		}
	}

	return records
}
