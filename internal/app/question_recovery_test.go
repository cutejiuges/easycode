package app

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"easycode/internal/config"
	"easycode/internal/domain"
	"easycode/internal/protocol"
	"easycode/internal/secret"
	"easycode/internal/tool"
)

func TestQuestionCatalogUsesOnlyMetadata(t *testing.T) {
	data, err := os.ReadFile("../session/testdata/current/question_openai_complete.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	root, _, _ := installPreviewFixture(t, data)
	var output, diagnostics bytes.Buffer
	result := ListSessions(context.Background(), SessionListOptions{All: true, Limit: 20, SessionDataRoot: root, SessionCatalogPath: filepath.Join(filepath.Dir(root), "state.sqlite"), Output: &output, ErrorOutput: &diagnostics})
	if result.ExitCode() != 0 || !bytes.Contains(output.Bytes(), []byte("gpt-test")) {
		t.Fatal("question fixture missing from catalog", result, output.String(), diagnostics.String())
	}
	if bytes.Contains(output.Bytes(), []byte("保留说明")) || bytes.Contains(output.Bytes(), []byte("选择存储")) {
		t.Fatal("answer text duplicated in catalog metadata")
	}
}

func TestQuestionRestoreRejectsNativeAndSharedFactMismatchReadOnly(t *testing.T) {
	for _, family := range []domain.ProviderFamily{domain.ProviderOpenAI, domain.ProviderAnthropic} {
		for _, fault := range []string{"native_input_mismatch", "native_answer_mismatch"} {
			t.Run(string(family)+"/"+fault, func(t *testing.T) {
				data, err := os.ReadFile("../session/testdata/current/question_" + string(family) + "_" + fault + ".jsonl")
				if err != nil {
					t.Fatal(err)
				}
				root, path, thread := installPreviewFixture(t, data)
				// Loader 和共享 grammar 均合法，必须由原生与共享事实的关联校验拒绝。
				loaded, plan := loadToolLoopPlan(t, root, thread)
				if len(loaded.Records) == 0 || !plan.Tools[1].HasResult {
					t.Fatal("fixture did not reach native association check")
				}
				preview, cleanup, err := previewSession(context.Background(), root, thread, defaultPreviewDependencies())
				if err == nil || cleanup || len(preview.Messages) != 0 {
					t.Fatal("offline preview accepted mismatched question", err)
				}
				cfg := config.Config{Provider: config.Provider{Family: family, BaseURL: "https://example.invalid", APIKey: secret.New("fixture-key"), Model: "gpt-test"}}
				if family == domain.ProviderAnthropic {
					cfg.Provider.Model = "claude-test"
				}
				resources, err := openChatResources(context.Background(), cfg, root, string(thread), t.TempDir())
				if err == nil {
					_ = resources.close(context.Background())
					t.Fatal("resume accepted mismatched question")
				}
				after, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(after, data) {
					t.Fatal("rejected restore rewrote journal", err)
				}
			})
		}
	}
}

func TestQuestionRecoveryNeverReasksAndRetainsFrozenAnswers(t *testing.T) {
	for _, family := range []domain.ProviderFamily{domain.ProviderOpenAI, domain.ProviderAnthropic} {
		for _, state := range []string{"ready", "started", "result", "complete", "skip", "unavailable"} {
			t.Run(string(family)+"/"+state, func(t *testing.T) {
				data, err := os.ReadFile("../session/testdata/current/question_" + string(family) + "_" + state + ".jsonl")
				if err != nil {
					t.Fatal(err)
				}
				root, path, thread := installPreviewFixture(t, data)
				cfg := config.Config{Provider: config.Provider{Family: family, BaseURL: "https://example.invalid", APIKey: secret.New("fixture-key"), Model: "gpt-test"}}
				if family == domain.ProviderAnthropic {
					cfg.Provider.Model = "claude-test"
				}
				resources, err := openChatResources(context.Background(), cfg, root, string(thread), t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
				query := resources.transcriptQueries
				page, err := query.ListTranscript(context.Background(), protocol.TranscriptListQuery{ThreadID: thread, Limit: 50})
				if err != nil {
					t.Fatal(err)
				}
				var found bool
				for _, item := range page.Items {
					if item.Activity.Tool != protocol.ToolQuestion {
						continue
					}
					found = true
					for _, source := range []protocol.TranscriptBodySource{protocol.TranscriptInput, protocol.TranscriptResult} {
						var text strings.Builder
						offset := uint64(0)
						for {
							body, err := query.ReadTranscriptBody(context.Background(), protocol.TranscriptBodyQuery{ThreadID: thread, Cursor: item.Cursor, Source: source, Offset: offset, Limit: 11})
							if err != nil || body.Validate() != nil {
								t.Fatal("question history page failed", err)
							}
							text.WriteString(body.Text)
							offset = body.NextOffset
							if offset == body.TotalBytes {
								break
							}
						}
						if source == protocol.TranscriptInput && !strings.Contains(text.String(), "PostgreSQL") {
							t.Fatal("question history omitted unselected options")
						}
						if source == protocol.TranscriptResult && (state == "complete" || state == "result") && !strings.Contains(text.String(), " 保留说明\n") {
							t.Fatal("question history lost exact answer")
						}
					}
				}
				if !found {
					t.Fatal("restored history omitted question")
				}
				if err := resources.close(context.Background()); err != nil {
					t.Fatal(err)
				}
				after, err := os.ReadFile(path)
				if err != nil || !bytes.HasPrefix(after, data) {
					t.Fatal("recovery rewrote current records", err)
				}
				_, plan := loadToolLoopPlan(t, root, thread)
				q := plan.Tools[1]
				if !q.HasResult {
					t.Fatal("recovery omitted paired result")
				}
				want := "ok"
				if state == "ready" {
					want = "session_interrupted_before_execution"
				}
				if state == "started" {
					want = "outcome_uncertain"
				}
				if state == "unavailable" {
					want = "user_input_unavailable"
				}
				if q.Result.Code() != want {
					t.Fatalf("recovery result=%s want=%s", q.Result.Code(), want)
				}
				if state == "started" && q.Result.Status() != tool.ResultOutcomeUncertain {
					t.Fatal("uncertain answer became success")
				}
				if state == "result" || state == "complete" {
					a, err := q.Result.QuestionAnswers()
					if err != nil || a.Answers()[0].Text != " 保留说明\n" {
						t.Fatal("recovery changed answer", err)
					}
				}
				if _, err := os.Stat(filepath.Join("/fixture/project", "answer.txt")); err == nil {
					t.Fatal("recovery executed Write")
				}
			})
		}
	}
}
