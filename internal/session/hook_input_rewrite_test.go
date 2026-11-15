package session

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/tool"
)

func TestInputRewriteFixedCurrentBytesAndOriginalEffectiveEvidence(t *testing.T) {
	for _, phase := range []string{"prefix", "next_started", "continued", "allowed", "started", "result", "recovery", "reverted", "rejected"} {
		t.Run(phase, func(t *testing.T) {
			fixture := readCurrentFixture(t, "rewrite_"+phase+".jsonl")
			for _, line := range bytes.Split(bytes.TrimSuffix(fixture, []byte("\n")), []byte("\n")) {
				r, err := DecodeRecord(line)
				if err != nil {
					t.Fatal(err)
				}
				_, encoded, err := EncodeRecord(r)
				if err != nil || !bytes.Equal(encoded, line) {
					t.Fatal("fixed current bytes changed", err)
				}
			}
			repo, lease := installCurrentFixture(t, fixture)
			defer repo.Close()
			defer lease.Close()
			loaded, err := NewLoader().Load(context.Background(), lease)
			if err != nil {
				t.Fatal(err)
			}
			plan, err := NewReplayPlanner().Plan(loaded)
			if err != nil {
				t.Fatal(err)
			}
			if plan.ToolRecovery == nil || len(plan.Tools) != 1 {
				t.Fatal("lost current rewrite plan")
			}
			entry := plan.Tools[0]
			original, _ := entry.Invocation.Patch()
			if original.Input().FilePath() != "main.go" {
				t.Fatal("native original overwritten")
			}
			if entry.PreHooks == nil || entry.PreDetailSequence == 0 {
				t.Fatal("missing committed detail sequence")
			}
			call := plan.ToolRecovery.Calls()[0]
			candidate, err := tool.CheckingHookInput(call.Invocation, call.Hooks.Plan, call.Hooks.Finished)
			if err != nil {
				t.Fatal(err)
			}
			path, _, _ := candidate.Patch()
			want := "effective.txt"
			if phase == "reverted" {
				want = "main.go"
			}
			if path != want {
				t.Fatal("lost intermediate candidate")
			}
			if phase == "prefix" || phase == "next_started" {
				if _, err := call.InputSelection(); err == nil || (phase == "next_started") != (call.Hooks.Started != nil) {
					t.Fatal("partial candidate selected as effective")
				}
			} else {
				selection, err := call.InputSelection()
				if err != nil {
					t.Fatal(err)
				}
				effective, err := selection.ResultInvocation()
				if err != nil {
					t.Fatal(err)
				}
				v, _ := effective.Patch()
				expected := "effective.txt"
				if phase == "rejected" || phase == "reverted" {
					expected = "main.go"
				}
				if v.Input().FilePath() != expected {
					t.Fatal("wrong selected result metadata")
				}
				if phase == "rejected" && selection.Kind() != tool.InputGateRejected {
					t.Fatal("rejected candidate became effective")
				}
				if entry.HasResult {
					evidence, err := call.InputResultEvidence()
					if err != nil || evidence.ValidateResult(entry.Result) != nil {
						t.Fatal("saved result proof invalid", err)
					}
					if phase == "result" && (!strings.HasPrefix(entry.Result.Preview().Text(), "[PreToolUse input rewrite]\n") || evidence.Admission().Sequence() != 16) {
						t.Fatal("missing actual admission/full input note")
					}
					if phase == "recovery" && (evidence.Origin() != tool.InputResultRecovery || entry.Result.Budget().ActualBytes() > tool.SafetyResultBytes || strings.Contains(entry.Result.Preview().Text(), `"file_path"`)) {
						t.Fatal("recovery retransmitted full input or hid uncertainty")
					}
					if phase == "reverted" || phase == "rejected" {
						if strings.Contains(entry.Result.Preview().Text(), "PreToolUse changed input") || strings.HasPrefix(entry.Result.Preview().Text(), "[PreToolUse") {
							t.Fatal("unselected/unchanged input changed model bytes")
						}
					}
				}
			}
			copy := call.Hooks.Clone()
			copy.Finished[0] = domain.HookFinished{}
			if plan.ToolRecovery.Calls()[0].Hooks.Finished[0].Validate() != nil {
				t.Fatal("getter leaked mutable replacement facts")
			}
		})
	}
}

func TestInputRewriteSemanticCorruptionPrecedesRepairAndKeepsBytes(t *testing.T) {
	for _, name := range []string{"digest", "profile", "expansion", "partial_candidate", "late_cancel", "recovery_disguise", "old_shape", "version", "metadata", "admission"} {
		t.Run(name, func(t *testing.T) {
			content := append(readCurrentFixture(t, "rewrite_invalid_"+name+".jsonl"), []byte(`{"seq":`)...)
			repo, lease := installCurrentFixture(t, content)
			defer repo.Close()
			defer lease.Close()
			loaded, err := NewLoader().Load(context.Background(), lease)
			if err == nil {
				_, err = NewReplayPlanner().Plan(loaded)
			}
			if err == nil {
				t.Fatal("forged current input evidence accepted")
			}
			path, _ := repo.JournalPath(testThreadID)
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(content, after) {
				t.Fatal("semantic failure repaired append-only bytes", err)
			}
		})
	}
}
