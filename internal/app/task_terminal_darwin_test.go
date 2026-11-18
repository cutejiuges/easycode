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

	"easycode/internal/codec"
	"easycode/internal/config"
	"easycode/internal/domain"
	"easycode/internal/protocol"
	"easycode/internal/secret"
	tea "github.com/charmbracelet/bubbletea"
)

type taskTerminalAck struct {
	Kind   string `json:"kind"`
	View   string `json:"view,omitempty"`
	State  string `json:"state,omitempty"`
	Number int    `json:"number,omitempty"`
	Width  int    `json:"width,omitempty"`
	Height int    `json:"height,omitempty"`
}

func writeTaskTerminalAck(ack *os.File, value taskTerminalAck) {
	data, err := codec.MarshalStable(value)
	if err == nil {
		_, _ = ack.Write(append(data, '\n'))
	}
}

type taskTerminalQueries struct {
	protocol.TranscriptQueries
	ack *os.File
}

func (q taskTerminalQueries) ListTranscript(ctx context.Context, query protocol.TranscriptListQuery) (protocol.TranscriptListPage, error) {
	page, err := q.TranscriptQueries.ListTranscript(ctx, query)
	writeTaskTerminalAck(q.ack, taskTerminalAck{Kind: "list"})
	return page, err
}
func (q taskTerminalQueries) ReadTranscriptBody(ctx context.Context, query protocol.TranscriptBodyQuery) (protocol.TranscriptBodyPage, error) {
	page, err := q.TranscriptQueries.ReadTranscriptBody(ctx, query)
	writeTaskTerminalAck(q.ack, taskTerminalAck{Kind: "detail", View: page.Text})
	return page, err
}

type taskTerminalModel struct {
	inner   tea.Model
	ack     *os.File
	release chan struct{}
	once    *sync.Once
}

func (m taskTerminalModel) Init() tea.Cmd { return m.inner.Init() }
func (m taskTerminalModel) View() string  { return m.inner.View() }
func (m taskTerminalModel) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	key, isKey := message.(tea.KeyMsg)
	if isKey && key.Type == tea.KeyF10 {
		return m, tea.Quit
	}
	if isKey && key.Type == tea.KeyF5 {
		m.once.Do(func() { close(m.release) })
	}
	beforeView := m.inner.View()
	next, cmd := m.inner.Update(message)
	m.inner = next
	if view := m.inner.View(); view != beforeView {
		writeTaskTerminalAck(m.ack, taskTerminalAck{Kind: "view", View: view})
	}
	if item, ok := message.(protocol.ControlItem); ok {
		event, exists := item.TurnEvent()
		if !exists {
			event, exists = item.ContextEvent()
		}
		if exists {
			value := taskTerminalAck{View: m.inner.View()}
			switch event.Kind {
			case protocol.EventTaskProgressUpdated:
				p, err := protocol.DecodeTaskProgressUpdated(event)
				if err == nil {
					value.Kind = "updated"
					value.State = string(p.Progress.Kind())
				}
			case protocol.EventTaskProgressSnapshot:
				p, err := protocol.DecodeTaskProgressSnapshot(event)
				if err == nil {
					value.Kind = "tasks"
					value.State = string(p.Progress.Kind())
				}
			case protocol.EventUserQuestionRequested:
				value.Kind = "question"
			case protocol.EventHookTrustRequested:
				value.Kind = "hook"
			case protocol.EventToolApprovalRequested:
				value.Kind = "approval"
			case protocol.EventTurnCompleted:
				value.Kind = "complete"
			case protocol.EventTurnFailed:
				value.Kind = "failed"
			}
			if value.Kind != "" {
				writeTaskTerminalAck(m.ack, value)
			}
		}
	}
	if isKey {
		writeTaskTerminalAck(m.ack, taskTerminalAck{Kind: "key", View: m.inner.View()})
	}
	if size, ok := message.(tea.WindowSizeMsg); ok {
		writeTaskTerminalAck(m.ack, taskTerminalAck{Kind: "size", Width: size.Width, Height: size.Height, View: m.inner.View()})
	}
	return m, cmd
}

func taskManyScenarioCall(t *testing.T, family domain.ProviderFamily) writeScenarioCall {
	t.Helper()
	var input strings.Builder
	if family == domain.ProviderOpenAI {
		input.WriteString(`{"plan":[`)
	} else {
		input.WriteString(`{"todos":[`)
	}
	for i := 1; i <= 20; i++ {
		if i > 1 {
			input.WriteByte(',')
		}
		status := "pending"
		if i == 18 {
			status = "in_progress"
		}
		if family == domain.ProviderOpenAI {
			fmt.Fprintf(&input, `{"step":"步骤%02d 中文检查结果","status":%q}`, i, status)
		} else {
			fmt.Fprintf(&input, `{"content":"步骤%02d 中文检查结果","activeForm":"正在处理步骤%02d","status":%q}`, i, i, status)
		}
	}
	input.WriteString(`]}`)
	name := "update_plan"
	if family == domain.ProviderAnthropic {
		name = "TodoWrite"
	}
	return writeScenarioCall{name, input.String()}
}

// TestTaskTerminalHelper 只由显式 PTY 脚本启用，所有时序由实际事件和 owner 信号驱动。
func TestTaskTerminalHelper(t *testing.T) {
	if os.Getenv("EASYCODE_TASK_TERMINAL_SMOKE") != "1" {
		return
	}
	family := domain.ProviderFamily(os.Getenv("EASYCODE_TASK_FAMILY"))
	if !family.Valid() {
		t.Fatal("invalid task terminal family")
	}
	fd, err := strconv.Atoi(os.Getenv("EASYCODE_TASK_ACK_FD"))
	if err != nil {
		t.Fatal(err)
	}
	ack := os.NewFile(uintptr(fd), "task-ack")
	defer ack.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 85*time.Second)
	defer cancel()
	release := make(chan struct{})
	once := &sync.Once{}
	defer once.Do(func() { close(release) })
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		n := int(requests.Add(1))
		if err := validateTaskServiceSchema(family, body); err != nil {
			t.Error(err)
			http.Error(w, "invalid task schema", http.StatusBadRequest)
			return
		}
		writeTaskTerminalAck(ack, taskTerminalAck{Kind: "body", Number: n})
		if bytes.Contains(body, []byte("same-name-task-skill-marker")) {
			writeTaskTerminalAck(ack, taskTerminalAck{Kind: "skill", Number: n})
		}
		w.Header().Set("Content-Type", "text/event-stream")
		if n == 2 || n == 7 {
			if family == domain.ProviderOpenAI {
				_, _ = fmt.Fprintf(w, "data: {\"type\":\"response.created\",\"response\":{\"id\":\"write-%d\"}}\n\n", n)
			} else {
				_, _ = fmt.Fprintf(w, "data: {\"type\":\"message_start\",\"message\":{\"id\":\"write-%d\",\"model\":\"claude-test\"}}\n\n", n)
			}
			w.(http.Flusher).Flush()
			writeTaskTerminalAck(ack, taskTerminalAck{Kind: "live", Number: n})
			if n == 7 {
				<-r.Context().Done()
				writeTaskTerminalAck(ack, taskTerminalAck{Kind: "cancelled", Number: n})
				return
			}
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
			call := writeScenarioCall{"request_user_input", questionScenarioInput}
			if family == domain.ProviderAnthropic {
				call.name = "AskUserQuestion"
			}
			var data bytes.Buffer
			serveWriteScenarioSample(&data, family, n, []writeScenarioCall{call})
			// 已发送开始事实，继续时删除第二份开始行，仍使用生产 reducer 的完整工具终态。
			raw := data.Bytes()
			at := bytes.Index(raw, []byte("\n\n"))
			if at < 0 {
				t.Error("missing stream boundary")
				return
			}
			_, _ = w.Write(raw[at+2:])
			return
		}
		var calls []writeScenarioCall
		if n == 1 {
			calls = []writeScenarioCall{taskManyScenarioCall(t, family)}
		}
		if n == 5 {
			calls = []writeScenarioCall{{"Write", `{"file_path":"task-approval.txt","content":"approved independently"}`}}
		}
		serveWriteScenarioSample(w, family, n, calls)
	}))
	defer server.Close()
	workspace := privateAppTempDir(t)
	skillPath := filepath.Join(workspace, ".easycode", "skills", "tasks", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(skillPath), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(skillPath, []byte("---\nname: tasks\ndescription: same-name task skill\n---\nsame-name-task-skill-marker\n"), 0600); err != nil {
		t.Fatal(err)
	}
	writeHookConfig(t, workspace, "cat >/dev/null; printf '{}'")
	model := "gpt-test"
	if family == domain.ProviderAnthropic {
		model = "claude-test"
	}
	resources, err := openChatResourcesWithHooks(ctx, config.Config{Provider: config.Provider{Family: family, BaseURL: server.URL, Model: model, APIKey: secret.New("task-pty-fixture")}}, dataPaths{sessionRoot: filepath.Join(privateAppTempDir(t), "sessions")}, "", false, workspace, chatResourceHooks{interactiveApprovals: true})
	if err != nil {
		t.Fatal(err)
	}
	defer resources.close(context.Background())
	inner, err := assembleChatModel(ctx, resources)
	if err != nil {
		t.Fatal(err)
	}
	inner = inner.WithTranscriptQueries(resources.identity.ThreadID, taskTerminalQueries{resources.transcriptQueries, ack})
	program := tea.NewProgram(taskTerminalModel{inner: inner, ack: ack, release: release, once: once}, tea.WithContext(ctx), tea.WithoutSignalHandler())
	writeTaskTerminalAck(ack, taskTerminalAck{Kind: "ready"})
	if _, err := program.Run(); err != nil {
		t.Fatal(err)
	}
	if resources.runtime.TaskProgress().Counts().Total != 20 {
		t.Fatal("terminal actions changed durable task list")
	}
	writeTaskTerminalAck(ack, taskTerminalAck{Kind: "total", Number: int(requests.Load())})
}
