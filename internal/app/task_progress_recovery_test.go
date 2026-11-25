package app

import (
	"bytes"
	"context"
	"os"
	"testing"

	"easycode/internal/config"
	"easycode/internal/domain"
	"easycode/internal/secret"
	"easycode/internal/session"
	"easycode/internal/tool"
)

func TestTaskRecoveryUsesCommittedResultAndNeverRepeatsCompensation(t *testing.T) {
	fixed := readAppTestFile(t, "../session/testdata/current/task_openai_result_tail.jsonl")
	// 固定事实源只在完整批次边界切前缀，不用当前 encoder 生成正输入。
	lines := bytes.Split(bytes.TrimSuffix(fixed, []byte{'\n'}), []byte{'\n'})
	for _, cut := range []string{"ready", "started", "result"} {
		t.Run(cut, func(t *testing.T) {
			end := len(lines)
			tail, err := session.DecodeRecord(lines[len(lines)-1])
			if err != nil {
				t.Fatal(err)
			}
			taskResult, err := session.DecodeToolCallResultPayload(tail)
			if err != nil {
				t.Fatal(err)
			}
			for i, line := range lines {
				record, err := session.DecodeRecord(line)
				if err != nil {
					t.Fatal(err)
				}
				if cut == "ready" && record.EventKind == session.EventToolExecutionStarted {
					started, err := session.DecodeToolExecutionStartedPayload(record)
					if err != nil {
						t.Fatal(err)
					}
					if started.InvocationID == taskResult.InvocationID {
						end = i
						break
					}
				}
				if cut == "started" && i == len(lines)-1 {
					end = i
					break
				}
			}
			original := append(bytes.Join(lines[:end], []byte{'\n'}), '\n')
			root, path, thread := installPreviewFixture(t, original)
			_, plan := loadToolLoopPlan(t, root, thread)
			expected, err := plan.TaskProgress()
			if err != nil {
				t.Fatal(err)
			}
			cfg := config.Config{Provider: config.Provider{Family: domain.ProviderOpenAI, Model: "gpt-test", BaseURL: "https://example.invalid", APIKey: secret.New("fixture-key")}}
			var previous []byte
			for attempt := range 2 {
				resources := mustOpenChatResources(t, context.Background(), cfg, root, string(thread), privateAppTempDir(t))
				if !resources.runtime.TaskProgress().Equal(expected) {
					t.Fatal("recovery applied an uncommitted task candidate")
				}
				closeAppTestResources(t, resources)
				current, err := os.ReadFile(path)
				if err != nil || !bytes.HasPrefix(current, original) {
					t.Fatal("recovery rewrote committed facts", err)
				}
				if attempt == 1 && !bytes.Equal(current, previous) {
					t.Fatal("recovery repeated compensation")
				}
				previous = current
			}
			_, replayed := loadToolLoopPlan(t, root, thread)
			if replayed.ToolRecovery != nil {
				t.Fatal("recovery left outputs unpaired")
			}
			last := replayed.Tools[len(replayed.Tools)-1]
			want := tool.ResultCancelled
			if cut == "started" {
				want = tool.ResultOutcomeUncertain
			}
			if cut == "result" {
				want = tool.ResultSuccess
			}
			if last.Result.Status() != want || last.ResultSequence <= last.Sequence {
				t.Fatal("task recovery changed the frozen boundary", cut, last.Result.Status())
			}
		})
	}
}
