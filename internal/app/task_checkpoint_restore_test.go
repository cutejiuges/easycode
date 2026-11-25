package app

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"

	"easycode/internal/codec"
	"easycode/internal/config"
	"easycode/internal/domain"
	"easycode/internal/provider"
	"easycode/internal/secret"
	"easycode/internal/session"
)

type taskCheckpointFixturePayload struct {
	Summary      json.RawMessage          `json:"summary"`
	TaskProgress domain.TaskProgressState `json:"task_progress"`
	Generation   json.RawMessage          `json:"generation"`
}

func TestTaskCheckpointFixedSourcesAndReadOnlyPreview(t *testing.T) {
	for _, family := range []domain.ProviderFamily{domain.ProviderOpenAI, domain.ProviderAnthropic} {
		t.Run(string(family), func(t *testing.T) {
			data := readAppTestFile(t, "../session/testdata/current/task_"+string(family)+"_checkpoints.jsonl")
			root, path, thread := installPreviewFixture(t, data)
			_, plan := loadToolLoopPlan(t, root, thread)
			if len(plan.Compactions) != 4 || len(plan.Tools) != 3 {
				t.Fatal("fixed task checkpoint chain is incomplete")
			}
			records, err := orderedRestoreRecords(context.Background(), plan)
			if err != nil {
				t.Fatal(err)
			}
			checkpointIndex := 0
			for _, record := range records {
				if !record.IsCheckpoint() {
					continue
				}
				expected, ok := record.ExpectedTaskProgress()
				if !ok {
					t.Fatal("offline checkpoint lost expected task evidence")
				}
				prefix, err := plan.Compactions[checkpointIndex].TaskProgressAtStart()
				if err != nil || !prefix.Equal(expected) {
					t.Fatal("checkpoint did not bind its own source prefix", err)
				}
				if checkpointIndex > 0 {
					source, present := expected.Source()
					call := plan.Tools[checkpointIndex-1]
					if !present || source.ResultSequence() != call.ResultSequence || source.InvocationID() != string(call.Invocation.InvocationID()) {
						t.Fatal("checkpoint task source did not use the successful result")
					}
				}
				checkpoint, err := record.Checkpoint()
				if err != nil {
					t.Fatal(err)
				}
				var payload taskCheckpointFixturePayload
				if err := codec.DecodeObject(checkpoint.Payload(), &payload, "summary", "task_progress", "generation"); err != nil || !payload.TaskProgress.Equal(expected) {
					t.Fatal("fixed task snapshot differs from committed source", err)
				}
				switch checkpointIndex {
				case 0:
					if expected.Kind() != domain.TaskProgressNone {
						t.Fatal("initial checkpoint invented tasks")
					}
				case 1:
					if expected.Counts().Completed != 0 || expected.Counts().InProgress != 1 {
						t.Fatal("partial checkpoint changed progress")
					}
				case 2:
					if expected.Kind() != domain.TaskProgressSnapshot || expected.Counts().Completed != 2 {
						t.Fatal("all-completed checkpoint became clear")
					}
				case 3:
					if expected.Kind() != domain.TaskProgressClear {
						t.Fatal("clear checkpoint resurrected old tasks")
					}
				}
				checkpointIndex++
			}
			preview, cleanup, err := previewSession(context.Background(), root, thread, defaultPreviewDependencies())
			if err != nil || cleanup || len(preview.Messages) == 0 {
				t.Fatal("legitimate task checkpoints could not preview", err)
			}
			model := "gpt-test"
			if family == domain.ProviderAnthropic {
				model = "claude-test"
			}
			cfg := config.Config{Provider: config.Provider{Family: family, Model: model, BaseURL: "https://example.invalid", APIKey: secret.New("fixture-key")}}
			resources := mustOpenChatResources(t, context.Background(), cfg, root, string(thread), privateAppTempDir(t))
			want, err := plan.TaskProgress()
			if err != nil || !resources.runtime.TaskProgress().Equal(want) {
				t.Fatal("restore used historical checkpoint as a new task update", err)
			}
			closeAppTestResources(t, resources)
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(after, data) {
				t.Fatal("complete checkpoint resume mutated journal")
			}
		})
	}
}

func TestTaskCheckpointForgeryRejectsBeforeRepairAndNetwork(t *testing.T) {
	for _, family := range []domain.ProviderFamily{domain.ProviderOpenAI, domain.ProviderAnthropic} {
		for _, fault := range []string{"future-result", "wrapper", "self-consistent", "missing-task"} {
			t.Run(string(family)+"/"+fault, func(t *testing.T) {
				original := readAppTestFile(t, "../session/testdata/current/task_"+string(family)+"_checkpoints.jsonl")
				root, path, thread := installPreviewFixture(t, original)
				loaded, plan := loadToolLoopPlan(t, root, thread)
				target := plan.Compactions[1].TerminalSequence
				lines := bytes.Split(bytes.TrimSuffix(original, []byte{'\n'}), []byte{'\n'})
				record := loaded.Records[target-1]
				completed, err := session.DecodeCompactionCompletedPayload(record)
				if err != nil {
					t.Fatal(err)
				}
				checkpoint := completed.Checkpoint
				var payload taskCheckpointFixturePayload
				if err := codec.DecodeObject(checkpoint.Payload(), &payload, "summary", "task_progress", "generation"); err != nil {
					t.Fatal(err)
				}
				update, _ := payload.TaskProgress.Update()
				changed := false
				switch fault {
				case "future-result":
					future, err := plan.Compactions[2].TaskProgressAtStart()
					if err != nil {
						t.Fatal(err)
					}
					futureSource, _ := future.Source()
					payload.TaskProgress, err = domain.NewTaskProgressState(futureSource, update)
					if err != nil {
						t.Fatal(err)
					}
					changed = true
				case "wrapper":
					payload.Summary = bytes.Replace(payload.Summary, []byte("检查中文代码"), []byte("伪造同长内容"), 1)
					changed = true
				}
				var payloadBytes []byte
				if changed {
					payloadBytes, err = codec.MarshalStable(payload)
				} else {
					payloadBytes = checkpoint.Payload()
				}
				if err != nil {
					t.Fatal(err)
				}
				switch fault {
				case "self-consistent":
					payloadBytes = bytes.ReplaceAll(payloadBytes, []byte("检查中文代码"), []byte("伪造同长内容"))
				case "missing-task":
					payloadBytes, err = codec.MarshalStable(struct {
						Summary    json.RawMessage `json:"summary"`
						Generation json.RawMessage `json:"generation"`
					}{payload.Summary, payload.Generation})
				}
				if err != nil {
					t.Fatal(err)
				}
				completed.Checkpoint, err = provider.NewCheckpointEnvelope(checkpoint.Scope(), checkpoint.ID(), checkpoint.Source(), payloadBytes)
				if err != nil {
					t.Fatal(err)
				}
				record.Payload, err = codec.MarshalStable(completed)
				if err != nil {
					t.Fatal(err)
				}
				_, lines[target-1], err = session.EncodeRecord(record)
				if err != nil {
					t.Fatal(err)
				}
				data := append(append(bytes.Join(lines, []byte{'\n'}), '\n'), []byte(`{"torn":`)...)
				writeAppTestFile(t, path, data, 0600)
				// 保留合法 checksum 与完整共享回放；必须由各 checkpoint 的原生证据拒绝。
				_, _ = loadToolLoopPlan(t, root, thread)
				var network atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					network.Add(1)
					http.Error(w, "unexpected network", http.StatusInternalServerError)
				}))
				defer server.Close()
				preview, cleanup, err := previewSession(context.Background(), root, thread, defaultPreviewDependencies())
				if err == nil || cleanup || len(preview.Messages) != 0 {
					t.Fatal("forged checkpoint preview exposed history", err)
				}
				model := "gpt-test"
				if family == domain.ProviderAnthropic {
					model = "claude-test"
				}
				resources, err := openChatResources(context.Background(), config.Config{Provider: config.Provider{Family: family, Model: model, BaseURL: server.URL, APIKey: secret.New("fixture-key")}}, root, string(thread), privateAppTempDir(t))
				if err == nil {
					_ = resources.close(context.Background())
					t.Fatal("forged checkpoint resumed")
				}
				after, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(after, data) || network.Load() != 0 {
					t.Fatalf("%s changed bytes, repaired tail or used network", fault)
				}
			})
		}
	}
}
