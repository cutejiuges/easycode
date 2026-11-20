package tui

import (
	"context"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"easycode/internal/domain"
	"easycode/internal/protocol"
)

type contextSession interface {
	QueryContext(context.Context, protocol.RequestID) (protocol.CommandResult, error)
	CompactContext(context.Context, string) (protocol.CommandResult, error)
	CancelCompaction(context.Context, domain.CompactionID) (protocol.CommandResult, error)
}

type localContextCommand struct {
	kind         protocol.CommandKind
	instructions string
}

type compactionCancelFailedMsg struct {
	id      domain.CompactionID
	summary string
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
	case "/cache":
		if remainder != "" {
			return localContextCommand{}, true, fmt.Errorf("/cache does not accept arguments")
		}
		return localContextCommand{kind: protocol.CommandQueryCache}, true, nil
	case "/context":
		if remainder != "" {
			return localContextCommand{}, true, fmt.Errorf("/context does not accept arguments")
		}
		return localContextCommand{kind: protocol.CommandQueryContext}, true, nil
	case "/compact":
		if !utf8.ValidString(remainder) || len(remainder) > domain.MaxCompactionInstructionsBytes {
			return localContextCommand{}, true, fmt.Errorf("compaction instructions require valid UTF-8 and at most %d bytes", domain.MaxCompactionInstructionsBytes)
		}
		return localContextCommand{kind: protocol.CommandCompactContext, instructions: remainder}, true, nil
	default:
		return localContextCommand{}, false, nil
	}
}

func (model Model) submitContextCommand(command localContextCommand) (tea.Model, tea.Cmd) {
	if command.kind == protocol.CommandQueryCache {
		return model.submitCacheCommand()
	}
	target, ok := model.session.(contextSession)
	if !ok {
		model.errorSummary = "invalid_command: context control is unavailable"
		return model, nil
	}
	model.queryOperationGeneration = model.effortOperationGeneration
	model.queryGeneration++
	model.queryRequestID = protocol.RequestID(fmt.Sprintf("tui-context-%d", model.queryGeneration))
	model.queryKind = command.kind
	snapshot := model.composer
	model.pending = &snapshot
	model.pendingContext = command.kind
	model.state = stateSubmitting
	model.errorSummary = ""
	ctx, requestID := model.submitContext, model.queryRequestID
	generation := model.queryGeneration
	return model, func() tea.Msg {
		var err error
		if command.kind == protocol.CommandQueryContext {
			_, err = target.QueryContext(ctx, requestID)
		} else {
			_, err = target.CompactContext(ctx, command.instructions)
		}
		if err != nil {
			if command.kind == protocol.CommandQueryContext {
				return cacheQueryFailedMsg{generation: generation, err: err}
			}
			return submissionFailedMsg{err: err}
		}
		return nil
	}
}

func (model Model) cancelActiveCompaction() tea.Cmd {
	if model.activeTurnID.Valid() {
		return interruptSession(model.session)
	}
	target, ok := model.session.(contextSession)
	if !ok {
		return nil
	}
	ctx, id := model.submitContext, model.activeCompactionID
	return func() tea.Msg {
		result, err := target.CancelCompaction(ctx, id)
		if err != nil {
			return compactionCancelFailedMsg{id, safeErrorSummary(err)}
		}
		if summary, rejected := result.Error(); rejected {
			return compactionCancelFailedMsg{id, string(summary.Code()) + ": " + summary.Message()}
		}
		return nil
	}
}

func (model Model) projectContextResult(result protocol.CommandResult) (tea.Model, tea.Cmd) {
	if result.CommandKind() == protocol.CommandCancelCompaction {
		return model, nil
	}
	if (result.CommandKind() == protocol.CommandQueryCache || result.CommandKind() == protocol.CommandQueryContext) && result.RequestID() != model.queryRequestID {
		return model, nil
	}
	if model.pending == nil || model.pendingContext != result.CommandKind() {
		return model, nil
	}
	if result.Status() == protocol.CommandStatusAccepted {
		if result.CommandKind() != protocol.CommandQueryCache {
			model.composer.history.add(model.pending.text)
		}
		if result.CommandKind() == protocol.CommandQueryCache || result.CommandKind() == protocol.CommandQueryContext {
			model.queryRequestID = result.RequestID()
		}
		model.composer.clear()
		if result.CommandKind() == protocol.CommandCompactContext {
			model.prepareCompaction(result.CompactionID(), domain.ManualCompactionScope())
		}
	} else {
		model.restoreComposer(*model.pending)
		summary, _ := result.Error()
		model.errorSummary = string(summary.Code()) + ": " + summary.Message()
	}
	model.pending = nil
	model.pendingContext = ""
	model.refreshState()
	if model.interruptNext && model.activeCompactionID.Valid() {
		model.interruptNext = false
		return model.requestCompactionCancellation()
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
		if !model.observeEffort(payload.Effort, true) {
			return model, nil
		}
		model.contextPanel = &payload
		model.contextScroll = 0
	case protocol.EventCompactionStarted:
		payload, err := protocol.DecodeCompactionStarted(event)
		if err != nil {
			model.errorSummary = "stream_protocol_error: invalid compaction start"
			return model, nil
		}
		if model.activeCompactionID == "" && model.freshAutomaticCompaction(payload.Scope) {
			model.prepareCompaction(event.CompactionID, payload.Scope)
			model.lastAutomaticCompaction = payload.Scope
		}
		if !model.currentCompaction(event, payload.Scope) || model.compaction.sessionID.Valid() {
			return model, nil
		}
		model.effortOperationGeneration++
		model.compaction.sessionID, model.compaction.threadID = event.SessionID, event.ThreadID
	case protocol.EventCompactionCompleted:
		if event.CompactionID != model.activeCompactionID {
			return model, nil
		}
		payload, err := protocol.DecodeCompactionCompleted(event)
		if err != nil {
			model.errorSummary = "stream_protocol_error: invalid compaction completion"
			return model, nil
		}
		if !model.currentCompaction(event, payload.Scope) || !model.compaction.sessionID.Valid() {
			return model, nil
		}
		model.effortOperationGeneration++
		model.compactionStatus = fmt.Sprintf("Conversation compacted · estimated %s → %s tokens · Ctrl+O for history", formatContextEstimate(payload.Before), formatContextEstimate(payload.After))
		model.clearCompaction()
		model.refreshState()
		if model.browser.queries != nil {
			model.main.generation++
			return model, model.queryMainTranscript(model.main.generation, 0, model.submitContext)
		}
	case protocol.EventCompactionFailed:
		payload, err := protocol.DecodeCompactionFailed(event)
		if err != nil {
			model.errorSummary = "stream_protocol_error: invalid compaction failure"
			return model, nil
		}
		if !model.currentCompaction(event, payload.Scope) {
			if model.activeCompactionID != "" || payload.Durable || !model.freshAutomaticCompaction(payload.Scope) {
				return model, nil
			}
			model.lastAutomaticCompaction = payload.Scope
		}
		model.effortOperationGeneration++
		model.compactionStatus = "Compaction failed: " + string(payload.Code)
		model.clearCompaction()
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

func (model Model) contextPanelLines() []string {
	payload := model.contextPanel
	if payload == nil {
		return nil
	}
	lines := []string{"Context · committed snapshot", fmt.Sprintf("Model: %s · revision %d · %s", safeComposerText(payload.Model), payload.Revision, payload.Activity), ""}
	lines = append(lines, model.inferenceLines()...)
	lines = append(lines, effortSummary(payload.Effort)...)
	lines = append(lines, fmt.Sprintf("Selection revision: %d", payload.Effort.SelectionRevision))
	if payload.Effort.Active != nil {
		lines = append(lines, fmt.Sprintf("Captured revision: %d", payload.Effort.Active.SelectionRevision()))
	}
	allowed := make([]string, len(payload.Effort.AllowedLevels))
	for i, level := range payload.Effort.AllowedLevels {
		allowed[i] = string(level)
	}
	lines = append(lines, "Locally allowed levels: "+strings.Join(allowed, " "), "Captured configuration; transmission not implied")
	lines = append(lines, "")
	for _, source := range payload.Sources {
		lines = append(lines, strings.ReplaceAll(source.Kind, "_", " ")+": "+formatContextEstimate(source.Estimate)+" tokens ("+string(source.Estimate.State)+")")
	}
	lines = append(lines, "", "Total: "+formatContextEstimate(payload.Total)+" tokens · "+payload.Total.Method)
	if payload.WindowConfigured {
		lines = append(lines, fmt.Sprintf("Window: %d · output reserve: %d · safety: %d", payload.Window, payload.ReservedOutput, payload.SafetyMargin))
	} else {
		lines = append(lines, "Context window not configured")
	}
	auto := "Auto compact: " + payload.AutoCompactionState
	if payload.AutoCompactionReason != "" {
		auto += " (" + payload.AutoCompactionReason + ")"
	}
	if payload.AutoCompactionThreshold != nil {
		auto += fmt.Sprintf(" at %d tokens", *payload.AutoCompactionThreshold)
	}
	lines = append(lines, auto, "Budget: "+payload.BudgetState, fmt.Sprintf("Cache: %s · key %s · %s", payload.CachePolicy.Anthropic, payload.CachePolicy.Key, payload.CachePolicy.Transport))
	width := max(1, model.widthOrDefault())
	rendered := make([]string, 0, len(lines))
	for _, line := range lines {
		for _, wrapped := range strings.Split(ansi.Hardwrap(line, width, true), "\n") {
			rendered = append(rendered, strings.TrimRight(wrapped, " "))
		}
	}
	return rendered
}

func (model Model) contextScrollLimit() int {
	return max(0, len(model.contextPanelLines())-max(0, model.heightOrDefault()-1))
}

func (model Model) renderContextPanel() string {
	lines := model.contextPanelLines()
	if lines == nil {
		return ""
	}
	width, height := max(1, model.widthOrDefault()), max(1, model.heightOrDefault())
	capacity := height - 1
	start := min(model.contextScroll, max(0, len(lines)-capacity))
	end := min(len(lines), start+capacity)
	rendered := append([]string(nil), lines[start:end]...)
	footer := "Esc: close"
	if len(lines) > capacity {
		footer += " · ↑/↓ PgUp/PgDn: scroll"
	}
	rendered = append(rendered, ansi.Truncate(footer, width, ""))
	return strings.Join(rendered, "\n")
}

func (model Model) handleContextPanelKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if model.contextPanel != nil {
		switch key.Type {
		case tea.KeyDown:
			model.contextScroll = min(model.contextScroll+1, model.contextScrollLimit())
		case tea.KeyUp:
			model.contextScroll = max(0, model.contextScroll-1)
		case tea.KeyPgDown:
			model.contextScroll = min(model.contextScroll+min(model.contextScrollLimit(), max(1, model.heightOrDefault()-2)), model.contextScrollLimit())
		case tea.KeyPgUp:
			model.contextScroll = max(0, model.contextScroll-max(1, model.heightOrDefault()-2))
		case tea.KeyHome:
			model.contextScroll = 0
		case tea.KeyEnd:
			model.contextScroll = model.contextScrollLimit()
		}
	}
	if model.cachePanel != nil {
		switch key.Type {
		case tea.KeyDown:
			model.cacheScroll++
		case tea.KeyUp:
			model.cacheScroll = max(0, model.cacheScroll-1)
		case tea.KeyPgDown:
			model.cacheScroll += max(1, model.heightOrDefault()-2)
		case tea.KeyPgUp:
			model.cacheScroll = max(0, model.cacheScroll-max(1, model.heightOrDefault()-2))
		}
	}
	if key.Type == tea.KeyEsc || key.Type == tea.KeyCtrlC {
		model.contextPanel = nil
		model.cachePanel = nil
		model.queryGeneration++
		model.queryRequestID = ""
		model.queryKind = ""
		if key.Type == tea.KeyCtrlC {
			return model.handleKey(key)
		}
	}
	return model, nil
}
