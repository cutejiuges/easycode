package tui

import (
	"context"
	"easycode/internal/domain"
	"easycode/internal/protocol"
	"errors"
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

type markdownTerminalModel struct {
	gate     chan struct{}
	released bool
	model    Model
	ack      *os.File
	sequence int
	approval protocol.Event
	code     string
}

func (m markdownTerminalModel) Init() tea.Cmd { return nil }
func (m markdownTerminalModel) View() string  { return m.model.View() }
func (m markdownTerminalModel) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	key, isKey := message.(tea.KeyMsg)
	var pending tea.Cmd
	if isKey {
		switch key.Type {
		case tea.KeyF3:
			m.model.nextTextID++
			m.model.transcript = []transcriptMessage{{local: m.model.nextTextID, role: "Assistant", text: "```go\n" + strings.Repeat("x;", 32760) + "\n```\n"}}
			m.model.state = stateStreaming
			m.model.activeTurnID = m.approval.TurnID
		case tea.KeyF4:
			if !m.released {
				close(m.gate)
				m.released = true
			}
		case tea.KeyF9:
			next, cmd := m.model.commitMainTranscript(100)
			m.model = next.(Model)
			pending = cmd
		case tea.KeyF5:
			m.model.transcript = []transcriptMessage{{role: "Assistant", text: m.code}}
			m.model.state = stateStreaming
			m.model.activeTurnID = m.approval.TurnID
		case tea.KeyF6:
			m.model.transcript = []transcriptMessage{{role: "Assistant", text: m.code + "```\n"}}
			m.model.state = stateIdle
		case tea.KeyF7:
			message = RuntimeEventMsg{Event: m.approval}
		case tea.KeyF8:
			m.model.approval = nil
			m.model.activeTurnID = ""
			m.model.cancelling = false
			m.model.state = stateIdle
		case tea.KeyF10:
			fmt.Fprintln(m.ack, "QUIT")
			return m, tea.Quit
		}
	}
	next, cmd := m.model.Update(message)
	m.model = next.(Model)
	if isKey {
		m.sequence++
		fmt.Fprintf(m.ack, "ACK %d draft=%d detached=%t approval=%t detail=%t raw=%t handoff=%t cancelling=%t\n", m.sequence, len(m.model.composer.text), m.model.reading.detached, m.model.approval != nil, m.model.detailOpen, m.model.browser.markdown.raw, m.model.main.handoff != nil, m.model.cancelling)
	}
	switch value := message.(type) {
	case tea.WindowSizeMsg:
		fmt.Fprintf(m.ack, "SIZE %d %d\n", value.Width, value.Height)
	case transcriptReply:
		if m.model.browser.detail {
			fmt.Fprintln(m.ack, "DETAIL_READY")
		} else {
			fmt.Fprintln(m.ack, "HISTORY_READY")
		}
	case mainTranscriptReply:
		fmt.Fprintln(m.ack, "PROJECTION_READY")
	}
	return m, tea.Batch(cmd, pending)
}

// TestMarkdownTerminalHelper 仅由真实 PTY 验收脚本显式启动，不访问外网。
func TestMarkdownTerminalHelper(t *testing.T) {
	if os.Getenv("EASYCODE_MARKDOWN_TERMINAL_SMOKE") != "1" {
		return
	}
	fd, err := strconv.Atoi(os.Getenv("EASYCODE_MARKDOWN_ACK_FD"))
	if err != nil {
		t.Fatal(err)
	}
	ack := os.NewFile(uintptr(fd), "markdown-ack")
	defer ack.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	model := NewModel(ctx, "markdown-smoke", &fakeChatSession{}).WithMarkdownColor(os.Getenv("NO_COLOR") == "")
	for i := 0; i < 49; i++ {
		model.main.items = append(model.main.items, transcriptMessage{cursor: protocol.TranscriptCursor{Sequence: uint64(i + 1)}, role: "Assistant", text: "Completed **answer**\n"})
	}
	code := "```go\n" + strings.Repeat("var value = 123\n", 4000)
	model.transcript = []transcriptMessage{{role: "Assistant", text: code + "```\n"}}
	model.width = 80
	model.height = 24
	model.composer.resize(80, 24)
	model.prepareMarkdownView()
	approval := mustApprovalRequested(t)
	model.browser.thread = domain.ThreadID("01900000-0000-7000-8000-000000000001")
	gate := make(chan struct{})
	model.browser.queries = &markdownTerminalQueries{gate: gate, text: "# History answer\n\n```go\nvar value = 42\n```\n", turn: approval.TurnID}
	program := tea.NewProgram(markdownTerminalModel{model: model, ack: ack, approval: approval, code: code, gate: gate}, tea.WithContext(ctx), tea.WithoutSignalHandler())
	fmt.Fprintln(ack, "READY")
	if _, err := program.Run(); err != nil {
		t.Fatal(err)
	}
	fmt.Fprintln(ack, "CLOSED")
}

type markdownTerminalQueries struct {
	gate <-chan struct{}
	text string
	turn domain.TurnID
}

func (q *markdownTerminalQueries) ListTranscript(ctx context.Context, query protocol.TranscriptListQuery) (protocol.TranscriptListPage, error) {
	if query.Through > 0 {
		select {
		case <-q.gate:
		case <-ctx.Done():
			return protocol.TranscriptListPage{}, ctx.Err()
		}
	}
	return protocol.TranscriptListPage{Items: []protocol.TranscriptListItem{{Cursor: protocol.TranscriptCursor{Sequence: 100}, Kind: domain.TranscriptAssistant, TurnID: q.turn, Text: q.text}}}, nil
}
func (q *markdownTerminalQueries) ReadTranscriptBody(_ context.Context, _ protocol.TranscriptBodyQuery) (protocol.TranscriptBodyPage, error) {
	return protocol.TranscriptBodyPage{Text: q.text, NextOffset: uint64(len(q.text)), TotalBytes: uint64(len(q.text))}, nil
}
func (q *markdownTerminalQueries) ReadToolOutput(context.Context, protocol.ToolOutputQuery) (protocol.ToolOutputPage, error) {
	return protocol.ToolOutputPage{}, errors.New("unexpected tool output query")
}
