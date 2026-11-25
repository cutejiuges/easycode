package session

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"easycode/internal/codec"
	"easycode/internal/domain"
	"easycode/internal/tool"
)

func sessionTaskInvocation(t *testing.T, index int, profile tool.TaskProfile, content string, status domain.TaskStatus) tool.Invocation {
	t.Helper()
	items := make([]domain.TaskStep, 0, 1)
	if content != "" {
		active := ""
		if profile == tool.TaskProfileTodoWrite {
			active = "正在" + content
		}
		step, err := domain.NewTaskStep(content, active, status)
		requireNoError(t, err)
		items = append(items, step)
	}
	explanation := ""
	if profile == tool.TaskProfileUpdatePlan && content != "" {
		explanation = "保留完整说明\n\t逐项验证"
	}
	update, err := domain.NewTaskUpdate(items, explanation)
	requireNoError(t, err)
	input, err := tool.NewTaskInput(profile, update)
	requireNoError(t, err)
	ready, err := tool.NewTaskReadyCall(tool.ProviderCallID(fmt.Sprintf("task-call-%d", index)), input)
	requireNoError(t, err)
	id, err := tool.ParseInvocationID(fmt.Sprintf("01890f3e-7bcd-7abc-8abc-%012d", index))
	requireNoError(t, err)
	inv, err := tool.NewInvocation(id, ready)
	requireNoError(t, err)
	return inv
}

func sessionTaskResult(t *testing.T, inv tool.Invocation) tool.InvocationResult {
	t.Helper()
	v, _ := inv.Task()
	r, err := tool.NewTaskInvocationResult(v)
	requireNoError(t, err)
	return r
}

func taskResultDraft(t *testing.T, inv tool.Invocation) RecordDraft {
	t.Helper()
	draft, err := NewToolCallResultDraft(testTurnID, sessionTaskResult(t, inv))
	requireNoError(t, err)
	return draft
}

func TestTaskPayloadsRejectUnknownMissingNullAndMixedUnions(t *testing.T) {
	inv := sessionTaskInvocation(t, 2, tool.TaskProfileUpdatePlan, "检查实现", domain.TaskInProgress)
	ready, _ := NewToolCallReadyDraft(testTurnID, inv, 0, 0)
	result := taskResultDraft(t, inv)
	for _, test := range []struct {
		name, old, replacement string
		kind                   EventKind
	}{
		{"ready unknown", `"task_input":{`, `"task_input":{"future":"task-secret",`, EventToolCallReady},
		{"ready missing", `"task_input":`, `"missing_input":`, EventToolCallReady},
		{"ready null", `"task_input":{`, `"task_input":null,"old_input":{`, EventToolCallReady},
		{"ready duplicate", `"profile":"update_plan"`, `"profile":"update_plan","profile":"update_plan"`, EventToolCallReady},
		{"ready profile null", `"profile":"update_plan"`, `"profile":null`, EventToolCallReady},
		{"ready wrong profile", `"profile":"update_plan"`, `"profile":"todo_write"`, EventToolCallReady},
		{"ready nested required", `"explanation":`, `"unknown_explanation":`, EventToolCallReady},
		{"ready nested unknown", `"active_form":""`, `"active_form":"","future":"task-secret"`, EventToolCallReady},
		{"ready wrong capability", `"capability":"task.progress"`, `"capability":"fs.read"`, EventToolCallReady},
		{"ready custom", `"wire_kind":"structured"`, `"wire_kind":"custom_freeform"`, EventToolCallReady},
		{"ready other input", `"task_input":`, `"read_input":{"file_path":"README.md","offset":1,"limit":1},"task_input":`, EventToolCallReady},
		{"result unknown", `"task_metadata":{`, `"task_metadata":{"future":"task-secret",`, EventToolCallResult},
		{"result missing", `"task_metadata":`, `"missing_metadata":`, EventToolCallResult},
		{"result missing applied", `},"update":{"items":`, `},"missing_update":{"items":`, EventToolCallResult},
		{"result candidate mismatch", `"content":"检查实现"`, `"content":"伪造候选"`, EventToolCallResult},
		{"result failed applied", `"status":"success"`, `"status":"error"`, EventToolCallResult},
		{"result preview", `"preview":"Task progress updated: 0/1 completed"`, `"preview":"task-secret"`, EventToolCallResult},
		{"result allowance", `"allowance":1024`, `"allowance":1025`, EventToolCallResult},
		{"result actual", `"actual_bytes":36`, `"actual_bytes":0`, EventToolCallResult},
		{"result duplicate", `"code":"ok"`, `"code":"ok","code":"ok"`, EventToolCallResult},
		{"result input null", `"input":{`, `"input":null,"old_input":{`, EventToolCallResult},
		{"result other metadata", `"task_metadata":`, `"read_metadata":{"relative_path":"README.md","requested_offset":1,"requested_limit":1,"start_line":1,"end_line":1,"reached_eof":true,"long_line_truncated":false,"output_truncated":false},"task_metadata":`, EventToolCallResult},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := ready.PayloadBytes()
			if test.kind == EventToolCallResult {
				source = result.PayloadBytes()
			}
			mutated := bytes.Replace(source, []byte(test.old), []byte(test.replacement), 1)
			if bytes.Equal(mutated, source) {
				t.Fatal("test did not modify task payload")
			}
			record := Record{EventKind: test.kind, Payload: mutated}
			var err error
			if test.kind == EventToolCallReady {
				_, err = DecodeToolCallReadyPayload(record)
			} else {
				_, err = DecodeToolCallResultPayload(record)
			}
			if err == nil || strings.Contains(err.Error(), "task-secret") {
				t.Fatal("invalid task payload accepted or leaked candidate", err)
			}
		})
	}
	for _, draft := range []RecordDraft{ready, result} {
		record := Record{EventKind: draft.EventKind(), Payload: append(draft.PayloadBytes(), []byte(`{}`)...)}
		var err error
		if record.EventKind == EventToolCallReady {
			_, err = DecodeToolCallReadyPayload(record)
		} else {
			_, err = DecodeToolCallResultPayload(record)
		}
		if err == nil {
			t.Fatal("task payload accepted trailing JSON")
		}
	}
	payload, err := DecodeToolCallResultPayload(Record{EventKind: EventToolCallResult, Payload: result.PayloadBytes()})
	requireNoError(t, err)
	payload.TaskMetadata.Update = nil
	withoutApplied, err := codec.MarshalStable(payload)
	requireNoError(t, err)
	if _, err := DecodeToolCallResultPayload(Record{EventKind: EventToolCallResult, Payload: withoutApplied}); err == nil {
		t.Fatal("successful task result without an applied snapshot accepted")
	}
}

func TestTaskResultMetadataRemainsCompleteBeyondConfirmationCap(t *testing.T) {
	items := make([]domain.TaskStep, 0, domain.MaxTaskSteps)
	for i := 0; i < domain.MaxTaskSteps; i++ {
		item, err := domain.NewTaskStep(fmt.Sprintf("%02d", i)+strings.Repeat("界", 170), "", domain.TaskPending)
		requireNoError(t, err)
		items = append(items, item)
	}
	update, _ := domain.NewTaskUpdate(items, strings.Repeat("说", 682))
	input, err := tool.NewTaskInput(tool.TaskProfileUpdatePlan, update)
	requireNoError(t, err)
	ready, _ := tool.NewTaskReadyCall("task-large", input)
	id, _ := tool.ParseInvocationID("01890f3e-7bcd-7abc-8abc-0123456789af")
	inv, _ := tool.NewInvocation(id, ready)
	result := sessionTaskResult(t, inv)
	draft, err := NewToolCallResultDraft(testTurnID, result)
	if err != nil || len(draft.PayloadBytes()) <= tool.MaxTaskPreviewBytes {
		t.Fatal("large complete metadata did not survive", err)
	}
	payload, err := DecodeToolCallResultPayload(Record{EventKind: EventToolCallResult, Payload: draft.PayloadBytes()})
	requireNoError(t, err)
	got, err := payload.Domain(inv)
	actual, applied := got.TaskUpdate()
	if err != nil || !applied || !actual.Equal(update) || actual.Counts().Total != domain.MaxTaskSteps || got.Preview().Text() != "Task progress updated: 0/20 completed" || strings.Contains(got.Preview().Text(), "界") {
		t.Fatal("task metadata was truncated into preview or lost", err)
	}
	if err := PreflightTaskResult(Identity{SessionID: testSessionID, ThreadID: testThreadID}, testTurnID, got); err != nil {
		t.Fatal(err)
	}
}

func TestTaskReplayEnforcesExclusiveNoApprovalAndNoHookGrammar(t *testing.T) {
	for _, phase := range []string{"ready", "started", "result", "failure_before_start", "cancel_before_start", "uncertain_started", "success_before_start", "uncertain_before_start", "duplicate_start", "duplicate_result", "forged_input", "approval", "pre_hook", "post_hook", "later_started", "earlier_unfinished", "call_index", "non_atomic_sample", "terminal_before_result"} {
		t.Run(phase, func(t *testing.T) {
			f := newReplayFixture(t)
			f.appendMetadata()
			f.appendBatch(mustDraft(t, EventTurnStarted, testTurnID, TurnStartedPayload{}))
			f.appendInitialInputs(testTurnID)
			inv := sessionTaskInvocation(t, 3, tool.TaskProfileUpdatePlan, "检查实现", domain.TaskInProgress)
			read := testReplayInvocation(t, "01890f3e-7bcd-7abc-8abc-0123456789ae", "other-call")
			index := uint32(0)
			if phase == "call_index" {
				index = 1
			}
			batch := []RecordDraft{mustDraft(t, EventProviderNativeCommit, testTurnID, validNativeCommit()), mustDraft(t, EventSampleUsage, testTurnID, testSampleUsage(t))}
			if phase == "earlier_unfinished" {
				batch = append(batch, mustReadyDraftForReplay(t, read, 0, 0))
				index = 1
			}
			batch = append(batch, mustReadyDraftForReplay(t, inv, 0, index))
			if phase == "later_started" {
				batch = append(batch, mustReadyDraftForReplay(t, read, 0, 1))
			}
			if phase == "non_atomic_sample" {
				f.appendBatch(batch[0])
				f.appendBatch(batch[1:]...)
			} else {
				f.appendBatch(batch...)
			}
			switch phase {
			case "started", "result", "uncertain_started", "duplicate_start", "duplicate_result", "forged_input", "terminal_before_result", "earlier_unfinished":
				f.appendBatch(mustStartedDraftForReplay(t, inv))
			}
			switch phase {
			case "result", "success_before_start", "duplicate_result":
				f.appendBatch(taskResultDraft(t, inv))
				if phase == "duplicate_result" {
					f.appendBatch(taskResultDraft(t, inv))
				}
			case "failure_before_start", "cancel_before_start", "uncertain_started", "uncertain_before_start":
				status, code := tool.ResultError, "result_budget_exceeded"
				if phase == "cancel_before_start" {
					status, code = tool.ResultCancelled, "session_interrupted_before_execution"
				} else if phase != "failure_before_start" {
					status, code = tool.ResultOutcomeUncertain, "outcome_uncertain"
				}
				result := tool.NewInvocationErrorResult(inv, status, code, "Task not updated")
				draft, _ := NewToolCallResultDraft(testTurnID, result)
				f.appendBatch(draft)
			case "duplicate_start":
				f.appendBatch(mustStartedDraftForReplay(t, inv))
			case "forged_input":
				foreign := sessionTaskInvocation(t, 3, tool.TaskProfileUpdatePlan, "伪造候选", domain.TaskInProgress)
				f.appendBatch(taskResultDraft(t, foreign))
			case "later_started":
				f.appendBatch(mustStartedDraftForReplay(t, read))
			case "terminal_before_result":
				f.appendBatch(mustDraft(t, EventTurnCompleted, testTurnID, TurnCompletedPayload{}))
			case "approval":
				if _, err := NewApprovalMetadata(inv); err == nil {
					t.Fatal("task admitted approval metadata")
				}
				request, _ := tool.ParseApprovalRequestID(string(inv.InvocationID()))
				p := ToolApprovalDecidedPayload{RequestID: request, InvocationID: inv.InvocationID(), Fingerprint: tool.NewCandidateFingerprint([]byte("candidate")), Outcome: tool.ApprovalOutcomeAllowOnce, Source: ApprovalSourceInteractive, Metadata: ApprovalMetadataPayload{Capability: tool.CapabilityWrite, RelativePath: "answer.txt"}}
				draft, err := NewToolApprovalDecidedDraft(testTurnID, p)
				requireNoError(t, err)
				f.appendBatch(draft)
			case "pre_hook", "post_hook":
				event := domain.HookPreToolUse
				if phase == "post_hook" {
					event = domain.HookPostToolUse
				}
				declaration, err := domain.NewHookDeclaration(domain.HookSourceProject, event, domain.HookFeedbackNone, strings.Repeat("a", 64), "check-task", 7, "true", 5)
				requireNoError(t, err)
				refs := []domain.HookReference{declaration.Reference}
				if _, err := domain.NewHookPlan(string(inv.InvocationID()), domain.HookCapability(tool.CapabilityTaskProgress), refs); err == nil {
					t.Fatal("task entered Hook capability set")
				}
				var draft RecordDraft
				if phase == "pre_hook" {
					plan, err := domain.NewHookPlan(string(inv.InvocationID()), domain.HookPatch, refs)
					requireNoError(t, err)
					draft, err = NewToolHookGatePlannedDraft(testTurnID, plan)
					requireNoError(t, err)
				} else {
					branch, _ := domain.NewPostHookBranch(event, refs, 0)
					plan, err := domain.NewPostHookPlan(string(inv.InvocationID()), domain.HookPatch, []domain.PostHookBranch{branch}, 1024, 0)
					requireNoError(t, err)
					draft, err = NewToolPostHookPlannedDraft(testTurnID, plan)
					requireNoError(t, err)
				}
				f.appendBatch(draft)
			}
			plan, err := NewReplayPlanner().Plan(f.loaded())
			valid := phase == "ready" || phase == "started" || phase == "result" || phase == "failure_before_start" || phase == "cancel_before_start" || phase == "uncertain_started"
			if !valid {
				if err == nil {
					t.Fatal("illegal task replay accepted")
				}
				return
			}
			if err != nil || len(plan.Tools) != 1 || plan.ToolRecovery == nil || plan.ToolRecovery.Calls()[0].ApprovalSequence != 0 || plan.Tools[0].PreHooks != nil || plan.Tools[0].PostHooks != nil {
				t.Fatal("valid task no-approval ledger rejected", err)
			}
		})
	}
}

func TestTaskProgressUsesActualResultPrefixAcrossReplacementsAndFailedTurns(t *testing.T) {
	f := newReplayFixture(t)
	f.appendMetadata()
	f.appendBatch(mustDraft(t, EventTurnStarted, testTurnID, TurnStartedPayload{}))
	f.appendInitialInputs(testTurnID)
	invocations := []tool.Invocation{
		sessionTaskInvocation(t, 10, tool.TaskProfileUpdatePlan, "第一项", domain.TaskCompleted),
		sessionTaskInvocation(t, 11, tool.TaskProfileUpdatePlan, "第二项", domain.TaskInProgress),
		sessionTaskInvocation(t, 12, tool.TaskProfileUpdatePlan, "第一项", domain.TaskPending),
		sessionTaskInvocation(t, 13, tool.TaskProfileUpdatePlan, "", domain.TaskPending),
	}
	batch := []RecordDraft{mustDraft(t, EventProviderNativeCommit, testTurnID, validNativeCommit()), mustDraft(t, EventSampleUsage, testTurnID, testSampleUsage(t))}
	for i, inv := range invocations {
		batch = append(batch, mustReadyDraftForReplay(t, inv, 0, uint32(i)))
	}
	f.appendBatch(batch...)
	resultSequences := make([]uint64, 0, len(invocations))
	for _, inv := range invocations {
		f.appendBatch(mustStartedDraftForReplay(t, inv))
		resultSequences = append(resultSequences, f.next)
		f.appendBatch(taskResultDraft(t, inv))
	}
	f.appendBatch(mustDraft(t, EventProviderNativeCommit, testTurnID, validToolOutputsCommit()))
	f.appendBatch(mustDraft(t, EventTurnFailed, testTurnID, TurnFailedPayload{Code: "user_cancelled"}))
	f.appendBatch(mustDraft(t, EventTurnStarted, domain.TurnID("018f1d8a-7b5c-7def-8123-456789abcded"), TurnStartedPayload{}))
	f.appendInitialInputs(domain.TurnID("018f1d8a-7b5c-7def-8123-456789abcded"))
	last := sessionTaskInvocation(t, 14, tool.TaskProfileUpdatePlan, "不得应用", domain.TaskInProgress)
	ready, _ := NewToolCallReadyDraft(domain.TurnID("018f1d8a-7b5c-7def-8123-456789abcded"), last, 0, 0)
	f.appendBatch(mustDraft(t, EventProviderNativeCommit, domain.TurnID("018f1d8a-7b5c-7def-8123-456789abcded"), validNativeCommit()), mustDraft(t, EventSampleUsage, domain.TurnID("018f1d8a-7b5c-7def-8123-456789abcded"), testSampleUsage(t)), ready)
	failure := tool.NewInvocationErrorResult(last, tool.ResultError, "result_budget_exceeded", "Task not updated")
	failedDraft, _ := NewToolCallResultDraft(domain.TurnID("018f1d8a-7b5c-7def-8123-456789abcded"), failure)
	f.appendBatch(failedDraft)
	f.appendBatch(mustDraft(t, EventProviderNativeCommit, domain.TurnID("018f1d8a-7b5c-7def-8123-456789abcded"), validToolOutputsCommit()))
	f.appendBatch(mustDraft(t, EventTurnFailed, domain.TurnID("018f1d8a-7b5c-7def-8123-456789abcded"), TurnFailedPayload{Code: "user_cancelled"}))
	plan, err := NewReplayPlanner().Plan(f.loaded())
	requireNoError(t, err)
	if len(plan.Tools) != 5 || len(plan.Turns) != 2 {
		t.Fatal("task replacements lost failed-turn history")
	}
	progressAt := func(sequence uint64) domain.TaskProgressState {
		t.Helper()
		loaded := f.loaded()
		loaded.Records = loaded.Records[:sequence]
		loaded.NextSequence = sequence + 1
		prefix, err := NewReplayPlanner().Plan(loaded)
		if err != nil {
			t.Fatal("valid record prefix rejected", sequence, err)
		}
		state, err := prefix.TaskProgress()
		if err != nil {
			t.Fatal("committed task prefix rejected", sequence, err)
		}
		return state
	}
	none := progressAt(resultSequences[0] - 1)
	if none.Kind() != domain.TaskProgressNone {
		t.Fatal("success source was included in its own preceding prefix")
	}
	for i, sequence := range resultSequences {
		state := progressAt(sequence)
		source, found := state.Source()
		update, applied := state.Update()
		inv, _ := invocations[i].Task()
		if !found || !applied || source.ResultSequence() != sequence || source.ResultSequence() == plan.Tools[i].Sequence || source.ResultSequence() == plan.Tools[i].NativeSequence || source.InvocationID() != string(invocations[i].InvocationID()) || !update.Equal(inv.Input().Update()) {
			t.Fatal("task prefix did not select exact full replacement", i)
		}
		if i == 0 && (state.Kind() != domain.TaskProgressSnapshot || state.Counts().Completed != 1) {
			t.Fatal("completed-only list was discarded")
		}
		if i == 2 && state.Counts().Pending != 1 {
			t.Fatal("valid regression to pending was lost")
		}
		if bytes.Contains(state.CanonicalJSON(), []byte(`"profile"`)) {
			t.Fatal("tool facade profile leaked into domain task state")
		}
	}
	current, err := plan.TaskProgress()
	source, _ := current.Source()
	if err != nil || current.Kind() != domain.TaskProgressClear || source.ResultSequence() != resultSequences[3] || source.TurnID() != testTurnID {
		t.Fatal("failed later turn changed earlier explicit clear", err)
	}
}

func TestInvalidTaskResultWithValidChecksumFailsBeforeTailRepair(t *testing.T) {
	fixture := readCurrentFixture(t, "task_openai_result_tail.jsonl")
	lines := bytes.Split(bytes.TrimSuffix(fixture, []byte{'\n'}), []byte{'\n'})
	record := decodeFixtureRecord(t, lines[12])
	record.Payload = bytes.Replace(record.Payload, []byte(`"content":"读取代码"`), []byte(`"content":"伪造正文"`), 1)
	checksum, err := recordChecksum(record)
	requireNoError(t, err)
	record.Checksum = checksum
	encoded, err := codec.MarshalStable(record)
	requireNoError(t, err)
	lines[12] = encoded
	content := append(bytes.Join(lines, []byte{'\n'}), []byte("\n{\"seq\":")...)
	repo, lease := installCurrentFixture(t, content)
	defer repo.Close()
	defer lease.Close()
	if _, err := NewLoader().Load(context.Background(), lease); err == nil {
		t.Fatal("checksum-valid forged task input accepted")
	}
	assertJournalBytes(t, repo, content)
}
