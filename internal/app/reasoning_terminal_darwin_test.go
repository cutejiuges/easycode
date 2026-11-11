//go:build darwin

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
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"easycode/internal/config"
	"easycode/internal/domain"
	"easycode/internal/protocol"
	"easycode/internal/secret"
	"easycode/internal/tui"
	tea "github.com/charmbracelet/bubbletea"
)

type reasoningTerminalQueries struct {
	protocol.TranscriptQueries
	ack *os.File
}

func (q reasoningTerminalQueries) ListTranscript(ctx context.Context, query protocol.TranscriptListQuery) (protocol.TranscriptListPage, error) {
	page, err := q.TranscriptQueries.ListTranscript(ctx, query)
	fmt.Fprintln(q.ack, "LIST", query.Mode)
	return page, err
}
func (q reasoningTerminalQueries) ReadTranscriptBody(ctx context.Context, query protocol.TranscriptBodyQuery) (protocol.TranscriptBodyPage, error) {
	page, err := q.TranscriptQueries.ReadTranscriptBody(ctx, query)
	fmt.Fprintf(q.ack, "READ %d %d %d\n", query.Cursor.Sequence, query.Cursor.Item, query.Offset)
	return page, err
}

type reasoningTerminalModel struct {
	inner   tea.Model
	ack     *os.File
	release chan struct{}
	once    *sync.Once
	turn    domain.TurnID
	source  domain.ReasoningSource
}

func (m reasoningTerminalModel) Init() tea.Cmd { return m.inner.Init() }
func (m reasoningTerminalModel) View() string  { return m.inner.View() }
func (m reasoningTerminalModel) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	key, isKey := message.(tea.KeyMsg)
	if isKey {
		switch key.Type {
		case tea.KeyF5:
			m.once.Do(func() { close(m.release) })
		case tea.KeyF8:
			// 保存不确定的终端投影使用已通过 Runtime 故障注入测试的固定内部事实。
			turn := domain.TurnID("01900000-0000-7000-8000-000000000099")
			started := protocol.NewTurnStarted()
			started.TurnID = turn
			m.inner, _ = m.inner.Update(tui.RuntimeEventMsg{Event: started})
			start, _ := protocol.NewReasoningStarted(0, m.source)
			start.TurnID = turn
			m.inner, _ = m.inner.Update(tui.RuntimeEventMsg{Event: start})
			delta, _ := protocol.NewReasoningDelta(0, m.source, 0, "uncertain excerpt")
			delta.TurnID = turn
			m.inner, _ = m.inner.Update(tui.RuntimeEventMsg{Event: delta})
			failed, _ := protocol.NewTurnSampleFailed("session_write_failed", "session write failed", false, protocol.NewUncertainSampleResolution(0))
			failed.TurnID = turn
			message = tui.RuntimeEventMsg{Event: failed}
		case tea.KeyF10:
			return m, tea.Quit
		}
	}
	next, cmd := m.inner.Update(message)
	m.inner = next
	if item, ok := message.(protocol.ControlItem); ok {
		if event, ok := item.TurnEvent(); ok {
			m.turn = event.TurnID
			switch event.Kind {
			case protocol.EventAssistantReasoning:
				p, _ := protocol.DecodeAssistantReasoning(event)
				if p.Stage == protocol.ReasoningDelta {
					fmt.Fprintln(m.ack, "LIVE")
				}
			case protocol.EventTranscriptCommitted:
				p, _ := protocol.DecodeTranscriptCommitted(event)
				if p.Origin == protocol.TranscriptCommitSample {
					fmt.Fprintln(m.ack, "COMMIT", p.Sequence, p.Sample.Bindings[0].Item)
				}
			case protocol.EventTurnCompleted:
				if os.Getenv("EASYCODE_REASONING_CONTROLS_SMOKE") == "1" {
					p, _ := protocol.DecodeTurnCompleted(event)
					u, _ := p.Usage.Domain()
					thinking, _ := u.ReasoningOutput().Value()
					output, _ := u.Output().Value()
					fmt.Fprintln(m.ack, "USAGE", thinking, output)
				}
				fmt.Fprintln(m.ack, "COMPLETE")
			case protocol.EventTurnFailed:
				fmt.Fprintln(m.ack, "FAILED")
			}
		}
	}
	if item, ok := message.(protocol.ControlItem); ok && os.Getenv("EASYCODE_REASONING_CONTROLS_SMOKE") == "1" {
		if result, ok := item.CommandResult(); ok && result.CommandKind() == protocol.CommandCompactContext {
			if failure, rejected := result.Error(); rejected {
				fmt.Fprintln(m.ack, "BUDGET", failure.Code())
			}
		}
		if event, ok := item.ContextEvent(); ok {
			switch event.Kind {
			case protocol.EventCacheSnapshot:
				view := m.inner.View()
				snapshot, _ := protocol.DecodeCacheSnapshot(event)
				known := snapshot.LastTurn != nil && snapshot.LastTurn.ReasoningOutput.Value != nil && *snapshot.LastTurn.ReasoningOutput.Value == 60
				fmt.Fprintf(m.ack, "CACHE known=%t observed=%t\n", known, strings.Contains(strings.ReplaceAll(view, "\n", ""), "reasoning 60"))
			case protocol.EventContextSnapshot:
				view := m.inner.View()
				snapshot, _ := protocol.DecodeContextSnapshot(event)
				fmt.Fprintf(m.ack, "CONTEXT frozen=%t explicit=%t cap=%t idle=%t\n", strings.Contains(view, "frozen at startup"), strings.Contains(view, "explicit"), strings.Contains(view, "16384"), snapshot.Activity == "idle")
			case protocol.EventCompactionCompleted:
				fmt.Fprintln(m.ack, "SUMMARY")
			case protocol.EventCompactionFailed:
				fmt.Fprintln(m.ack, "SUMMARY_FAILED")
			}
		}
	}
	if isKey {
		view := m.inner.View()
		fmt.Fprintf(m.ack, "ACK detail=%t thinking=%t body=%t uncertain=%t cancelling=%t raw=%t observed=%t\n", strings.Contains(view, "Transcript") || strings.HasPrefix(view, "Reasoning ·"), strings.Contains(view, "Thinking"), strings.Contains(view, "reason()"), strings.Contains(view, "save status uncertain"), strings.Contains(view, "Cancelling"), strings.Contains(view, "Raw ·"), strings.Contains(strings.ReplaceAll(view, "\n", ""), "reasoning 60"))
	}
	if size, ok := message.(tea.WindowSizeMsg); ok {
		fmt.Fprintln(m.ack, "SIZE", size.Width, size.Height)
	}
	return m, cmd
}

// TestReasoningTerminalHelper 只由真实 PTY 脚本启动，使用本地 HTTP Provider。
func TestReasoningTerminalHelper(t *testing.T) {
	if os.Getenv("EASYCODE_REASONING_TERMINAL_SMOKE") != "1" {
		return
	}
	fd, err := strconv.Atoi(os.Getenv("EASYCODE_REASONING_ACK_FD"))
	if err != nil {
		t.Fatal(err)
	}
	ack := os.NewFile(uintptr(fd), "reasoning-ack")
	defer ack.Close()
	family := domain.ProviderFamily(os.Getenv("EASYCODE_REASONING_FAMILY"))
	if family != domain.ProviderOpenAI && family != domain.ProviderAnthropic {
		t.Fatal("invalid family")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	controls := os.Getenv("EASYCODE_REASONING_CONTROLS_SMOKE") == "1"
	var requests atomic.Int32
	release := make(chan struct{})
	once := &sync.Once{}
	defer once.Do(func() { close(release) })
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := int(requests.Add(1))
		body, _ := io.ReadAll(r.Body)
		wire := string(reasoningFixtureStream(family, 1, false))
		if controls {
			summary := bytes.Contains(body, []byte(`"tool_choice"`))
			if family == domain.ProviderAnthropic {
				if summary || !bytes.Contains(body, []byte(`"thinking":{"type":"enabled","budget_tokens":8192,"display":"omitted"}`)) {
					t.Error("manual policy changed")
				}
			} else {
				assertReasoningRequest(t, family, body, summary)
			}
			wire = string(activeReasoningFixture(family, n, n == 1))
			if n > 1 {
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, wire)
				return
			}
		}
		marker := `data: {"type":"response.reasoning_summary_text.done"`
		if family == domain.ProviderAnthropic {
			marker = `data: {"type":"content_block_delta","index":3,"delta":{"type":"signature_delta"`
		}
		split := strings.Index(wire, marker)
		if split < 0 {
			t.Error("fixture boundary missing")
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, wire[:split])
		w.(http.Flusher).Flush()
		select {
		case <-release:
			fmt.Fprint(w, wire[split:])
		case <-r.Context().Done():
		}
	}))
	defer server.Close()
	model := "gpt-test"
	source := domain.ReasoningOpenAISummary
	if family == domain.ProviderAnthropic {
		model = "claude-test"
		source = domain.ReasoningAnthropicThinking
	}
	cfg := config.Config{Provider: config.Provider{Family: family, BaseURL: server.URL, Model: model, APIKey: secret.New("fixture-key")}}
	cwd := t.TempDir()
	if controls {
		if err := os.WriteFile(filepath.Join(cwd, "README.md"), []byte("terminal tool content"), 0600); err != nil {
			t.Fatal(err)
		}
		fields := reasoningFields(family)
		if family == domain.ProviderAnthropic {
			fields = `"anthropic_thinking":{"type":"enabled","budget_tokens":8192,"display":"omitted"},"anthropic_effort":"high","max_output_tokens":16384`
		}
		cfg, err = config.Load(withReasoningConfig(t, writeAppConfig(t, t.TempDir(), family, server.URL), fields))
		if err != nil {
			t.Fatal(err)
		}
	}
	resources, err := openChatResources(ctx, cfg, filepath.Join(t.TempDir(), "sessions"), "", cwd)
	if err != nil {
		t.Fatal(err)
	}
	defer resources.close(context.Background())
	control, err := startTUIControlSession(ctx, resources.runtime)
	if err != nil {
		t.Fatal(err)
	}
	defer control.Shutdown(context.Background())
	inner := tui.NewModel(ctx, "reasoning-smoke", control).WithInferenceSettings(resources.inference).WithTranscriptQueries(resources.identity.ThreadID, reasoningTerminalQueries{resources.transcriptQueries, ack}).WithMarkdownColor(os.Getenv("NO_COLOR") == "")
	program := tea.NewProgram(reasoningTerminalModel{inner: inner, ack: ack, release: release, once: once, source: source}, tea.WithContext(ctx), tea.WithoutSignalHandler())
	fmt.Fprintln(ack, "READY")
	if _, err := program.Run(); err != nil {
		t.Fatal(err)
	}
	if controls {
		fmt.Fprintln(ack, "REQUESTS", requests.Load())
	}
	fmt.Fprintln(ack, "CLOSED")
}
