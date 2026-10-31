package tui

import (
	"context"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"easycode/internal/domain"
	"easycode/internal/protocol"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

type contextSession interface {
	QueryContext(context.Context) (protocol.CommandResult, error)
	CompactContext(context.Context, string) (protocol.CommandResult, error)
	CancelCompaction(context.Context, domain.CompactionID) (protocol.CommandResult, error)
}

type localContextCommand struct {
	kind         protocol.CommandKind
	instructions string
}

// parseContextCommand 仅识别完整首词，未知 slash 和绝对路径保留为普通正文。
func parseContextCommand(text string) (localContextCommand, bool, error) {
	trimmed := strings.TrimSpace(text)
	split := strings.IndexFunc(trimmed, unicode.IsSpace)
	word, remainder := trimmed, ""
	if split >= 0 {
		word = trimmed[:split]
		remainder = strings.TrimSpace(trimmed[split:])
	}
	switch word {
	case "/context":
		if remainder != "" {
			return localContextCommand{}, true, fmt.Errorf("/context does not accept arguments")
		}
		return localContextCommand{kind: protocol.CommandQueryContext}, true, nil
	case "/compact":
		if !utf8.ValidString(remainder) || len(remainder) > 16<<10 {
			return localContextCommand{}, true, fmt.Errorf("compaction instructions exceed 16384 UTF-8 bytes")
		}
		return localContextCommand{kind: protocol.CommandCompactContext, instructions: remainder}, true, nil
	default:
		return localContextCommand{}, false, nil
	}
}

func (model Model) submitContextCommand(command localContextCommand) (tea.Model, tea.Cmd) {
	target, ok := model.session.(contextSession)
	if !ok {
		model.errorSummary = "invalid_command: context control is unavailable"
		return model, nil
	}
	snapshot := model.composer
	model.pending = &snapshot
	model.pendingContext = command.kind
	model.state = stateSubmitting
	model.errorSummary = ""
	ctx := model.submitContext
	return model, func() tea.Msg {
		var err error
		if command.kind == protocol.CommandQueryContext {
			_, err = target.QueryContext(ctx)
		} else {
			_, err = target.CompactContext(ctx, command.instructions)
		}
		if err != nil {
			return submissionFailedMsg{err: err}
		}
		return nil
	}
}

func (model Model) cancelActiveCompaction() tea.Cmd {
	target, ok := model.session.(contextSession)
	if !ok {
		return nil
	}
	ctx, id := model.submitContext, model.activeCompactionID
	return func() tea.Msg {
		_, err := target.CancelCompaction(ctx, id)
		if err != nil {
			return approvalSubmitErrorMsg{err: err}
		}
		return nil
	}
}

func (model Model) projectContextResult(result protocol.CommandResult) (tea.Model, tea.Cmd) {
	if result.CommandKind() == protocol.CommandCancelCompaction {
		if summary, rejected := result.Error(); rejected {
			model.errorSummary = string(summary.Code()) + ": " + summary.Message()
		}
		return model, nil
	}
	if model.pending == nil || model.pendingContext != result.CommandKind() {
		return model, nil
	}
	if result.Status() == protocol.CommandStatusAccepted {
		model.composer.history.add(model.pending.text)
		model.composer.clear()
		if result.CommandKind() == protocol.CommandCompactContext {
			model.activeCompactionID = result.CompactionID()
			model.compactionStatus = "Compacting conversation…"
		}
	} else {
		model.composer = *model.pending
		summary, _ := result.Error()
		model.errorSummary = string(summary.Code()) + ": " + summary.Message()
	}
	model.pending = nil
	model.pendingContext = ""
	model.refreshState()
	if model.interruptNext && model.activeCompactionID.Valid() {
		model.interruptNext = false
		model.compactionStatus = "Cancelling compaction — waiting for cleanup"
		return model, model.cancelActiveCompaction()
	}
	model.interruptNext = false
	return model, nil
}

func (model Model) projectContextEvent(event protocol.Event) (tea.Model, tea.Cmd) {
	switch event.Kind {
	case protocol.EventContextSnapshot:
		payload, err := protocol.DecodeContextSnapshot(event)
		if err != nil {
			model.errorSummary = "stream_protocol_error: invalid context snapshot"
			return model, nil
		}
		model.contextPanel = &payload
	case protocol.EventCompactionStarted:
		if event.CompactionID != model.activeCompactionID {
			return model, nil
		}
		model.compactionStatus = "Compacting conversation…"
	case protocol.EventCompactionCompleted:
		if event.CompactionID != model.activeCompactionID {
			return model, nil
		}
		payload, err := protocol.DecodeCompactionCompleted(event)
		if err != nil {
			model.errorSummary = "stream_protocol_error: invalid compaction completion"
			return model, nil
		}
		model.compactionStatus = fmt.Sprintf("Conversation compacted · estimated %s → %s tokens · Ctrl+O for history", formatContextEstimate(payload.Before), formatContextEstimate(payload.After))
		model.activeCompactionID = ""
		model.refreshState()
		if model.browser.queries != nil {
			model.main.generation++
			return model, model.queryMainTranscript(model.main.generation, 0, model.submitContext)
		}
	case protocol.EventCompactionFailed:
		if event.CompactionID != model.activeCompactionID {
			return model, nil
		}
		payload, err := protocol.DecodeCompactionFailed(event)
		if err != nil {
			model.errorSummary = "stream_protocol_error: invalid compaction failure"
			return model, nil
		}
		model.compactionStatus = "Compaction failed: " + string(payload.Code)
		model.activeCompactionID = ""
	}
	model.refreshState()
	return model, nil
}

func formatContextEstimate(value protocol.ContextEstimatePayload) string {
	if value.Tokens == nil {
		return "unknown"
	}
	return fmt.Sprintf("%d", *value.Tokens)
}

func (model Model) renderContextPanel() string {
	payload := model.contextPanel
	if payload == nil {
		return ""
	}
	lines := []string{"Context · committed snapshot", fmt.Sprintf("Model: %s · revision %d · %s", safeComposerText(payload.Model), payload.Revision, payload.Activity), ""}
	for _, source := range payload.Sources {
		lines = append(lines, strings.ReplaceAll(source.Kind, "_", " ")+": "+formatContextEstimate(source.Estimate)+" tokens ("+string(source.Estimate.State)+")")
	}
	lines = append(lines, "", "Total: "+formatContextEstimate(payload.Total)+" tokens · "+payload.Total.Method)
	if payload.WindowConfigured {
		lines = append(lines, fmt.Sprintf("Window: %d · output reserve: %d · safety: %d", payload.Window, payload.ReservedOutput, payload.SafetyMargin))
	} else {
		lines = append(lines, "Context window not configured")
	}
	lines = append(lines, "Budget: "+payload.BudgetState, "Esc: close")
	width, height := max(1, model.widthOrDefault()), max(1, model.heightOrDefault())
	rendered := make([]string, 0, height)
	for _, line := range lines {
		rendered = append(rendered, strings.Split(ansi.Hardwrap(line, width, true), "\n")...)
	}
	if len(rendered) > height {
		rendered = rendered[:height]
		rendered[height-1] = ansi.Truncate("Esc: close", width, "")
	}
	return strings.Join(rendered, "\n")
}
