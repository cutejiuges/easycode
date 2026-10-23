package tui

import (
	"context"
	"easycode/internal/protocol"
	tea "github.com/charmbracelet/bubbletea"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeApprovalEvent(t *testing.T, overwrite, empty bool, long bool) protocol.Event {
	t.Helper()
	base := mustApprovalRequested(t)
	p, _ := protocol.DecodeToolApprovalRequested(base)
	p.Capability = "fs.write"
	p.ProviderCallID = "call-write"
	p.Reason = "Write modifies workspace files."
	p.RelativePath = "新建/pkg/a.txt"
	preview := "+中文\\r\\n\n+\\u{001B}\\u{202E}\n"
	if long {
		preview = strings.Repeat("+完整内容中文\n", 40)
	}
	var presentation protocol.ApprovalPresentation
	var err error
	if overwrite {
		presentation, err = protocol.NewWriteOverwriteApprovalPresentation("--- a/新建/pkg/a.txt\n+++ b/新建/pkg/a.txt\n-old\n+中文\n", 7, 1)
	} else if empty {
		presentation, err = protocol.NewWriteCreateApprovalPresentation("(empty file)\n", 0, 0, nil)
	} else {
		presentation, err = protocol.NewWriteCreateApprovalPresentation(preview, 16, 2, []string{"新建", "新建/pkg"})
	}
	if err != nil {
		t.Fatal(err)
	}
	p.Presentation = &presentation
	event, err := protocol.NewToolApprovalRequested(p)
	if err != nil {
		t.Fatal(err)
	}
	event.TurnID = base.TurnID
	return event
}

func TestWriteApprovalSnapshots(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	for _, tt := range []struct {
		name                   string
		overwrite, empty, long bool
		width, height          int
		scroll                 bool
	}{
		{"write_create", false, false, false, 80, 24, false},
		{"write_overwrite", true, false, false, 80, 24, false},
		{"write_empty", false, true, false, 80, 24, false},
		{"write_narrow", false, false, false, 34, 12, false},
		{"write_scroll", false, false, true, 50, 18, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			model := snapshotStreamingModel(t)
			model = updateModel(t, model, RuntimeEventMsg{Event: writeApprovalEvent(t, tt.overwrite, tt.empty, tt.long)})
			model = updateModel(t, model, tea.WindowSizeMsg{Width: tt.width, Height: tt.height})
			if tt.scroll {
				model = updateModel(t, model, tea.KeyMsg{Type: tea.KeyPgDown})
			}
			got := model.View()
			want, err := os.ReadFile(filepath.Join("testdata", tt.name+".golden"))
			if err != nil {
				t.Fatal(err)
			}
			if got != string(want) {
				t.Fatalf("snapshot mismatch:\n--- got ---\n%s--- want ---\n%s", got, want)
			}
			if strings.Contains(got, "\x1b") {
				t.Fatal("无色模式输出终端控制符")
			}
		})
	}
}

func TestWriteApprovalInputAndResolvedRemainSeparateFromExecution(t *testing.T) {
	for _, overwrite := range []bool{false, true} {
		session := &fakeChatSession{}
		model := NewModel(context.Background(), "test", session)
		model.draft = []rune("保留草稿")
		event := writeApprovalEvent(t, overwrite, false, false)
		model = updateModel(t, model, RuntimeEventMsg{Event: event})
		if model.approval.selected != 2 {
			t.Fatal("默认选项不是 No")
		}
		model = updateModel(t, model, tea.KeyMsg{Type: tea.KeyTab})
		model = updateModel(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("保留文件")})
		model = updateModel(t, model, tea.KeyMsg{Type: tea.KeyEsc})
		model = updateModel(t, model, tea.WindowSizeMsg{Width: 32, Height: 12})
		updated, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
		model = updated.(Model)
		if command == nil {
			t.Fatal("未提交拒绝")
		}
		_ = command()
		_, duplicate := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
		if duplicate != nil || len(session.approvals) != 1 || session.approvals[0].feedback != "保留文件" {
			t.Fatal("重复提交或反馈丢失")
		}
		resolved, err := protocol.NewToolApprovalResolved(protocol.ToolApprovalResolvedPayload{RequestID: eventApprovalRequestID, InvocationID: eventApprovalInvocationID, Fingerprint: strings.Repeat("a", 64), Decision: protocol.ApprovalDeny})
		if err != nil {
			t.Fatal(err)
		}
		resolved.TurnID = event.TurnID
		model = updateModel(t, model, RuntimeEventMsg{Event: resolved})
		if model.approval != nil || string(model.draft) != "保留草稿" || strings.Contains(model.View(), "Created") || strings.Contains(model.View(), "success") {
			t.Fatal("批准结束被误显示为写入成功或草稿丢失")
		}
		model = updateModel(t, model, RuntimeEventMsg{Event: event})
		_, interrupt := model.Update(tea.KeyMsg{Type: tea.KeyEsc})
		if interrupt == nil {
			t.Fatal("Esc 未取消活动 turn")
		}
		_ = interrupt()
		if session.interrupts != 1 {
			t.Fatal("未调用 interrupt")
		}
	}
}
