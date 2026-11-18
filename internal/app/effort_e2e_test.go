package app

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"easycode/internal/codec"
	"easycode/internal/config"
	"easycode/internal/domain"
	"easycode/internal/protocol"
	"easycode/internal/secret"
	"easycode/internal/tool"
)

func appEffortChoice(t *testing.T, level string) domain.EffortChoice {
	t.Helper()
	c, err := domain.NewExplicitEffortChoice(domain.EffortLevel(level))
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func appEffortSnapshot(t *testing.T, r *chatResources, id protocol.RequestID) protocol.EffortSnapshotPayload {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, err := r.session.QueryEffort(ctx, id)
	if err != nil || result.Status() != protocol.CommandStatusAccepted {
		t.Fatal("effort query failed", err)
	}
	for {
		select {
		case item := <-r.session.Outputs():
			if event, ok := item.ContextEvent(); ok && event.Kind == protocol.EventEffortSnapshot && item.RequestID() == id {
				p, err := protocol.DecodeEffortSnapshot(event)
				if err != nil {
					t.Fatal(err)
				}
				return p
			}
		case <-ctx.Done():
			t.Fatal("effort query timeout")
		}
	}
}
func appEffortSet(t *testing.T, r *chatResources, id protocol.RequestID, choice domain.EffortChoice) {
	t.Helper()
	if result, err := r.session.SetEffort(context.Background(), id, choice); err != nil || result.Status() != protocol.CommandStatusAccepted {
		t.Fatal("effort set failed", result, err)
	}
}
func appEffortIdle(t *testing.T, r *chatResources) {
	t.Helper()
	for i := 0; ; i++ {
		p := appEffortSnapshot(t, r, protocol.RequestID(fmt.Sprintf("idle-%d-%d", time.Now().UnixNano(), i)))
		if p.Activity == "idle" {
			return
		}
	}
}
func appEffortTurn(t *testing.T, r *chatResources, text string, wantFailure bool) {
	t.Helper()
	result, err := appendInteractiveText(context.Background(), r.session, text)
	if err != nil || result.Status() != protocol.CommandStatusAccepted {
		t.Fatal("append failed", err)
	}
	appEffortTerminal(t, r, wantFailure)
}
func appEffortTerminal(t *testing.T, r *chatResources, wantFailure bool) {
	t.Helper()
	for item := range r.session.Outputs() {
		if event, ok := item.TurnEvent(); ok && (event.Kind == protocol.EventTurnCompleted || event.Kind == protocol.EventTurnFailed) {
			if (event.Kind == protocol.EventTurnFailed) != wantFailure {
				t.Fatalf("unexpected turn terminal %s: %s", event.Kind, event.Payload)
			}
			return
		}
	}
	t.Fatal("missing terminal")
}
func assertAppEffort(t *testing.T, family domain.ProviderFamily, body []byte, want string) {
	t.Helper()
	var wire struct {
		Reasoning struct {
			Effort  string `json:"effort"`
			Summary string `json:"summary"`
		} `json:"reasoning"`
		Output struct {
			Effort string `json:"effort"`
		} `json:"output_config"`
		Thinking struct {
			Type    string `json:"type"`
			Display string `json:"display"`
		} `json:"thinking"`
		MaxOutput uint64 `json:"max_output_tokens"`
		MaxTokens uint64 `json:"max_tokens"`
	}
	if err := codec.Unmarshal(body, &wire); err != nil {
		t.Fatal(err)
	}
	actual := wire.Reasoning.Effort
	if family == domain.ProviderAnthropic {
		actual = wire.Output.Effort
		if wire.Thinking.Type != "adaptive" || wire.Thinking.Display != "omitted" {
			t.Fatal("thinking changed")
		}
	} else if wire.Reasoning.Summary != "auto" {
		t.Fatal("summary changed")
	}
	if actual != want || wire.MaxOutput+wire.MaxTokens != 16384 {
		t.Fatalf("wrong captured policy: %s, want %s", body, want)
	}
	if bytes.Contains(body, []byte("selection_revision")) || bytes.Contains(body, []byte("configuration_update")) {
		t.Fatal("process control entered native request")
	}
}

func TestRuntimeEffortDualProviderToolsRecoveryAndRestart(t *testing.T) {
	for _, family := range []domain.ProviderFamily{domain.ProviderOpenAI, domain.ProviderAnthropic} {
		t.Run(string(family), func(t *testing.T) {
			cwd := t.TempDir()
			if err := os.WriteFile(filepath.Join(cwd, "README.md"), []byte("read exactly once"), 0600); err != nil {
				t.Fatal(err)
			}
			bodies := make(chan []byte, 16)
			firstRelease := make(chan struct{})
			var count atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				n := int(count.Add(1))
				bodies <- body
				if n == 1 {
					<-firstRelease
				}
				if n == 6 {
					http.Error(w, "endpoint fixture", http.StatusBadRequest)
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = w.Write(activeReasoningFixture(family, n, n <= 2))
			}))
			defer server.Close()
			cfg := reasoningFixtureConfig(t, config.Config{Provider: config.Provider{Family: family, BaseURL: server.URL, Model: "fixture-model", APIKey: secret.New("effort-fixture")}})
			root := filepath.Join(t.TempDir(), "sessions")
			r, err := openChatResources(context.Background(), cfg, root, "", cwd)
			if err != nil {
				t.Fatal(err)
			}
			if err := r.session.StartInteractive(context.Background()); err != nil {
				t.Fatal(err)
			}
			if p := appEffortSnapshot(t, r, "initial"); p.SelectionRevision != 0 || p.Selected.Value != "high" || p.Source != "startup" {
				t.Fatal("startup selection wrong")
			}
			if result, err := appendInteractiveText(context.Background(), r.session, "Read @README.md then answer"); err != nil || result.Status() != protocol.CommandStatusAccepted {
				t.Fatal(err)
			}
			first := <-bodies
			assertAppEffort(t, family, first, "high")
			appEffortSet(t, r, "low", appEffortChoice(t, "low"))
			p := appEffortSnapshot(t, r, "pending")
			if !p.Pending || p.Active == nil || p.Active.Effort().Value != "high" || p.Selected.Value != "low" {
				t.Fatal("active request policy changed")
			}
			close(firstRelease)
			appEffortTerminal(t, r, false)
			assertAppEffort(t, family, <-bodies, "high")
			appEffortIdle(t, r)
			appEffortTurn(t, r, "next task", false)
			assertAppEffort(t, family, <-bodies, "low")
			appEffortIdle(t, r)
			appEffortSet(t, r, "auto", domain.NewAutomaticEffortChoice())
			appEffortTurn(t, r, "auto task", false)
			assertAppEffort(t, family, <-bodies, "")
			appEffortIdle(t, r)
			appEffortSet(t, r, "reset", domain.NewResetEffortChoice())
			appEffortTurn(t, r, "reset task", false)
			assertAppEffort(t, family, <-bodies, "high")
			appEffortIdle(t, r)
			appEffortSet(t, r, "failure-low", appEffortChoice(t, "low"))
			appEffortTurn(t, r, "endpoint failure", true)
			assertAppEffort(t, family, <-bodies, "low")
			appEffortIdle(t, r)
			if p := appEffortSnapshot(t, r, "after-400"); p.Selected.Value != "low" {
				t.Fatal("endpoint downgraded or reset selection")
			}
			appEffortTurn(t, r, "explicit retry", false)
			assertAppEffort(t, family, <-bodies, "low")
			appEffortIdle(t, r)
			if count.Load() != 7 {
				t.Fatal("implicit request retry", count.Load())
			}
			thread := r.identity.ThreadID
			journalPath, pathErr := r.service.repository.JournalPath(thread)
			if pathErr != nil {
				t.Fatal(pathErr)
			}
			before, readErr := os.ReadFile(journalPath)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if bytes.Contains(before, []byte("selection_revision")) || bytes.Contains(before, []byte("effort_snapshot")) {
				t.Fatal("process setting persisted")
			}
			if err := r.close(context.Background()); err != nil {
				t.Fatal(err)
			}
			r, err = openChatResources(context.Background(), cfg, root, string(thread), cwd)
			if err != nil {
				t.Fatal(err)
			}
			if count.Load() != 7 {
				t.Fatal("restore performed network")
			}
			if err := r.session.StartInteractive(context.Background()); err != nil {
				t.Fatal(err)
			}
			p = appEffortSnapshot(t, r, "restarted")
			if p.SelectionRevision != 0 || p.Source != "startup" || p.Selected.Value != "high" {
				t.Fatal("process override persisted")
			}
			after, readErr := os.ReadFile(journalPath)
			if readErr != nil || !bytes.Equal(before, after) {
				t.Fatal("restore/query rewrote journal", readErr)
			}
			if err := r.close(context.Background()); err != nil {
				t.Fatal(err)
			}
			for _, entry := range r.history.Turns {
				for _, message := range entry.Messages {
					if strings.Contains(message.Text, "configuration_update") {
						t.Fatal("control leaked into history")
					}
				}
			}
		})
	}
}

func TestRuntimeEffortQuestionAndApprovalKeepCapturedPolicy(t *testing.T) {
	for _, family := range []domain.ProviderFamily{domain.ProviderOpenAI, domain.ProviderAnthropic} {
		t.Run(string(family), func(t *testing.T) {
			bodies := make(chan []byte, 8)
			var requests atomic.Int32
			questionName := "request_user_input"
			if family == domain.ProviderAnthropic {
				questionName = "AskUserQuestion"
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				bodies <- body
				n := int(requests.Add(1))
				var calls []writeScenarioCall
				if n == 1 {
					calls = []writeScenarioCall{{questionName, questionScenarioInput}}
				}
				if n == 2 {
					calls = []writeScenarioCall{{"Write", `{"file_path":"answer.txt","content":"chosen"}`}}
				}
				var response bytes.Buffer
				serveWriteScenarioSample(&response, family, n, calls)
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = w.Write(withReasoningFixture(family, response.Bytes()))
			}))
			defer server.Close()
			cfg := reasoningFixtureConfig(t, config.Config{Provider: config.Provider{Family: family, BaseURL: server.URL, Model: "fixture-model", APIKey: secret.New("effort-fixture")}})
			cwd := t.TempDir()
			r, err := openChatResourcesWithHooks(context.Background(), cfg, dataPaths{sessionRoot: filepath.Join(t.TempDir(), "sessions")}, "", false, cwd, chatResourceHooks{interactiveApprovals: true})
			if err != nil {
				t.Fatal(err)
			}
			defer r.close(context.Background())
			if err := r.session.StartInteractive(context.Background()); err != nil {
				t.Fatal(err)
			}
			if _, err := appendInteractiveText(context.Background(), r.session, "ask then write"); err != nil {
				t.Fatal(err)
			}
			questions, approvals := 0, 0
			for item := range r.session.Outputs() {
				event, ok := item.TurnEvent()
				if !ok {
					continue
				}
				switch event.Kind {
				case protocol.EventUserQuestionRequested:
					questions++
					appEffortSet(t, r, "question-low", appEffortChoice(t, "low"))
					p := appEffortSnapshot(t, r, "question-state")
					if !p.Pending || p.Active.Effort().Value != "high" {
						t.Fatal("question changed operation")
					}
					request, err := protocol.DecodeUserQuestionRequested(event)
					if err != nil {
						t.Fatal(err)
					}
					answers, err := tool.NewQuestionAnswerSet([]tool.QuestionAnswer{{QuestionID: "storage", SelectedOptions: []int{0}}})
					if err != nil {
						t.Fatal(err)
					}
					if err := r.session.ResolveQuestion(context.Background(), event.TurnID, request, answers); err != nil {
						t.Fatal(err)
					}
				case protocol.EventToolApprovalRequested:
					approvals++
					appEffortSet(t, r, "approval-auto", domain.NewAutomaticEffortChoice())
					p := appEffortSnapshot(t, r, "approval-state")
					if !p.Pending || p.Active.Effort().Value != "high" || p.Selected.State != "omitted" {
						t.Fatal("approval changed operation")
					}
					request, err := protocol.DecodeToolApprovalRequested(event)
					if err != nil {
						t.Fatal(err)
					}
					if err := r.session.ResolveApproval(context.Background(), event.TurnID, request, protocol.ApprovalAllowOnce, ""); err != nil {
						t.Fatal(err)
					}
				case protocol.EventTurnFailed:
					t.Fatalf("interactive turn failed: %s", event.Payload)
				case protocol.EventTurnCompleted:
					goto completed
				}
			}
		completed:
			if questions != 1 || approvals != 1 {
				t.Fatal("interactive tool repeated", questions, approvals)
			}
			for range 3 {
				assertAppEffort(t, family, <-bodies, "high")
			}
			appEffortIdle(t, r)
			content, err := os.ReadFile(filepath.Join(cwd, "answer.txt"))
			if err != nil || string(content) != "chosen" {
				t.Fatal("approved effect missing")
			}
			appEffortTurn(t, r, "next task", false)
			assertAppEffort(t, family, <-bodies, "")
			appEffortIdle(t, r)
			if requests.Load() != 4 {
				t.Fatal("settings repeated tools or model")
			}
		})
	}
}

func TestRuntimeEffortDisconnectAndCancelKeepFutureSelection(t *testing.T) {
	for _, family := range []domain.ProviderFamily{domain.ProviderOpenAI, domain.ProviderAnthropic} {
		for _, cancelled := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/cancel%t", family, cancelled), func(t *testing.T) {
				entered, release := make(chan struct{}), make(chan struct{})
				var requests atomic.Int32
				var body []byte
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					body, _ = io.ReadAll(r.Body)
					requests.Add(1)
					wire := activeReasoningFixture(family, 1, false)
					marker := `data: {"type":"response.completed"`
					if family == domain.ProviderAnthropic {
						marker = `data: {"type":"message_delta"`
					}
					at := bytes.Index(wire, []byte(marker))
					if at < 0 {
						t.Error("missing fixture boundary")
						return
					}
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = w.Write(wire[:at])
					w.(http.Flusher).Flush()
					close(entered)
					select {
					case <-release:
					case <-r.Context().Done():
					}
				}))
				defer server.Close()
				cfg := reasoningFixtureConfig(t, config.Config{Provider: config.Provider{Family: family, BaseURL: server.URL, Model: "fixture-model", APIKey: secret.New("effort-fixture")}})
				r, err := openChatResources(context.Background(), cfg, filepath.Join(t.TempDir(), "sessions"), "", t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
				defer r.close(context.Background())
				if err := r.session.StartInteractive(context.Background()); err != nil {
					t.Fatal(err)
				}
				if _, err := appendInteractiveText(context.Background(), r.session, "work"); err != nil {
					t.Fatal(err)
				}
				<-entered
				assertAppEffort(t, family, body, "high")
				appEffortSet(t, r, "low", appEffortChoice(t, "low"))
				p := appEffortSnapshot(t, r, "before-failure")
				if !p.Pending || p.Active.Effort().Value != "high" {
					t.Fatal("failure fixture changed capture")
				}
				if cancelled {
					r.session.Interrupt()
				} else {
					close(release)
				}
				appEffortTerminal(t, r, true)
				appEffortIdle(t, r)
				p = appEffortSnapshot(t, r, "after-failure")
				if p.Selected.Value != "low" || requests.Load() != 1 {
					t.Fatal("failure rolled back or resent")
				}
				path, _ := r.service.repository.JournalPath(r.identity.ThreadID)
				raw, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if bytes.Contains(raw, []byte(`"sample_usage"`)) || bytes.Contains(raw, []byte("opaque-reasoning-sentinel")) {
					t.Fatal("partial sample committed native or usage")
				}
			})
		}
	}
}
