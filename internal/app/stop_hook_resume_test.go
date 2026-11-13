package app

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"testing"

	"easycode/internal/config"
	"easycode/internal/domain"
	"easycode/internal/session"
)

// testStopRecoveryWindows 使用真实 Provider journal 的每个 durable 边界模拟崩溃，绝不重跑 Hook。
func testStopRecoveryWindows(t *testing.T, cfg config.Config, root, workspace, path string, thread domain.ThreadID, baseline []byte) {
	t.Helper()
	loaded, _ := loadToolLoopPlan(t, root, thread)
	var checkpoints []session.Record
	inStop := false
	for _, record := range loaded.Records {
		if record.EventKind == session.EventTurnStopHookPlanned {
			inStop = true
		}
		if inStop && record.BatchIndex+1 == record.BatchSize {
			checkpoints = append(checkpoints, record)
		}
	}
	for _, checkpoint := range checkpoints {
		t.Run(fmt.Sprintf("recovery-%d-%s", checkpoint.Sequence, checkpoint.EventKind), func(t *testing.T) {
			writeJournalPrefix(t, path, baseline, checkpoint.Sequence)
			prefix, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			loads := 0
			load := func(context.Context, string, string) (domain.HookSnapshot, error) {
				loads++
				raw, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				lines := bytes.Split(bytes.TrimSuffix(raw, []byte("\n")), []byte("\n"))
				last, err := session.DecodeRecord(lines[len(lines)-1])
				if err != nil || last.EventKind != session.EventTurnCompleted && last.EventKind != session.EventTurnFailed {
					t.Fatal("current configuration loaded before recovery closed", err)
				}
				return domain.NewHookSnapshot(nil, []domain.HookDiagnostic{{Source: domain.HookSourceProject, Code: domain.HookConfigInvalid}})
			}
			resumed, err := openChatResourcesWithHooks(context.Background(), cfg, dataPaths{sessionRoot: root}, string(thread), false, workspace, chatResourceHooks{loadHooks: load})
			if err != nil {
				t.Fatal(err)
			}
			if err := resumed.close(context.Background()); err != nil {
				t.Fatal(err)
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.HasPrefix(after, prefix) || loads != 1 {
				t.Fatal("recovery changed prefix or load order", err)
			}
			_, plan := loadToolLoopPlan(t, root, thread)
			if plan.StopRecovery != nil || plan.ToolRecovery != nil || plan.InterruptedTail != nil || len(plan.Turns) != 1 {
				t.Fatal("recovery left an open turn")
			}
			for _, state := range plan.Stops {
				if state.Decision == nil || state.Decision.Disposition == domain.StopContinued && state.DeliverySequence == 0 {
					t.Fatal("frozen feedback obligation lost")
				}
			}
			if checkpoint.EventKind == session.EventHookExecutionStarted {
				state := plan.Stops[len(plan.Stops)-1]
				if state.Decision.Disposition != domain.StopUncertain || state.Decision.Origin != domain.HookOriginRecovery || len(state.Finished) != 1 || state.Finished[0].Result.Origin != "recovery" {
					t.Fatal("started outcome was invented")
				}
			}
			// 补偿交付后、终态前再次崩溃，下一次只允许补终态。
			for _, line := range bytes.Split(bytes.TrimSuffix(after, []byte("\n")), []byte("\n")) {
				record, err := session.DecodeRecord(line)
				if err != nil {
					t.Fatal(err)
				}
				if record.Sequence <= checkpoint.Sequence || record.BatchIndex+1 != record.BatchSize {
					continue
				}
				writeJournalPrefix(t, path, after, record.Sequence)
				crashPrefix, _ := os.ReadFile(path)
				second, err := openChatResourcesWithHooks(context.Background(), cfg, dataPaths{sessionRoot: root}, string(thread), false, workspace, chatResourceHooks{loadHooks: load})
				if err != nil {
					t.Fatal(err)
				}
				if err := second.close(context.Background()); err != nil {
					t.Fatal(err)
				}
				final, _ := os.ReadFile(path)
				expectedLines := bytes.Split(bytes.TrimSuffix(after, []byte("\n")), []byte("\n"))
				actualLines := bytes.Split(bytes.TrimSuffix(final, []byte("\n")), []byte("\n"))
				if len(expectedLines) != len(actualLines) {
					t.Fatal("second crash duplicated compensation")
				}
				if !bytes.HasPrefix(final, crashPrefix) {
					t.Fatal("second crash rewrote durable prefix")
				}
				for i := 0; i < len(expectedLines); i++ {
					expected, err := session.DecodeRecord(expectedLines[i])
					if err != nil {
						t.Fatal(err)
					}
					actual, err := session.DecodeRecord(actualLines[i])
					if err != nil {
						t.Fatal(err)
					}
					if expected.Sequence != actual.Sequence || expected.EventKind != actual.EventKind || expected.TurnID != actual.TurnID || expected.BatchID != actual.BatchID || expected.BatchIndex != actual.BatchIndex || expected.BatchSize != actual.BatchSize || !bytes.Equal(expected.Payload, actual.Payload) {
						t.Fatal("second crash changed frozen compensation")
					}
				}

				after = final
			}
			again, err := openChatResourcesWithHooks(context.Background(), cfg, dataPaths{sessionRoot: root}, string(thread), false, workspace, chatResourceHooks{loadHooks: load})
			if err != nil {
				t.Fatal(err)
			}
			if err := again.close(context.Background()); err != nil {
				t.Fatal(err)
			}
			repeated, _ := os.ReadFile(path)
			if !bytes.Equal(repeated, after) {
				t.Fatal("repeat recovery changed bytes")
			}
		})
	}
	if err := os.WriteFile(path, baseline, 0600); err != nil {
		t.Fatal(err)
	}
}
