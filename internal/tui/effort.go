package tui

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"easycode/internal/domain"
	"easycode/internal/protocol"
)

type effortSession interface {
	QueryEffort(context.Context, protocol.RequestID) (protocol.CommandResult, error)
	SetEffort(context.Context, protocol.RequestID, domain.EffortChoice) (protocol.CommandResult, error)
}

type localEffortCommand struct {
	choice domain.EffortChoice
	help   bool
	query  bool
}

// effortSelectionObservation 只保留权威选择水位，不缓存活动身份或窗口数值。
type effortSelectionObservation struct {
	selected protocol.EffortValuePayload
	source   string
	revision uint64
}

type effortFeedback struct {
	requestID           protocol.RequestID
	generation          uint64
	draft               composer
	kind                protocol.CommandKind
	accepted            bool
	operationGeneration uint64
}
type effortFailedMsg struct {
	generation uint64
	err        error
}

// parseEffortCommand 仅规范化参数；首词必须精确匹配内置命令。
func parseEffortCommand(text string) (localEffortCommand, bool, error) {
	words := strings.Fields(text)
	if len(words) == 0 || words[0] != "/effort" {
		return localEffortCommand{}, false, nil
	}
	if len(words) > 2 {
		return localEffortCommand{}, true, fmt.Errorf("/effort accepts one argument")
	}
	argument := ""
	if len(words) == 2 {
		for _, character := range words[1] {
			if character > 127 {
				return localEffortCommand{}, true, fmt.Errorf("unknown effort argument")
			}
		}
		argument = strings.ToLower(words[1])
	}
	command := localEffortCommand{}
	switch argument {
	case "", "current", "status":
		command.query = true
	case "help", "-h", "--help":
		command.help = true
	case "auto", "unset":
		command.choice = domain.NewAutomaticEffortChoice()
	case "reset":
		command.choice = domain.NewResetEffortChoice()
	default:
		choice, err := domain.NewExplicitEffortChoice(domain.EffortLevel(argument))
		if err != nil {
			return command, true, fmt.Errorf("unknown effort argument")
		}
		command.choice = choice
	}
	return command, true, nil
}

func (model *Model) clearEffortFeedback() {
	model.effortNotice = nil
	model.effortFeedback = nil
	model.effortGeneration++
}

func (model Model) submitEffortCommand(command localEffortCommand) (tea.Model, tea.Cmd) {
	if command.help {
		model.errorSummary = ""
		model.effortNotice = []string{"/effort [current|status] · inspect captured and next effort", "/effort <level> · select for the next operation", "OpenAI: none minimal low medium high xhigh max", "Anthropic: low medium high xhigh max", "auto / unset: omit effort; reset: startup configuration", "Process only · thinking, output cap and model stay fixed"}
		model.composer.history.add(model.composer.text)
		model.composer.clear()
		return model, nil
	}
	target, ok := model.session.(effortSession)
	if !ok {
		model.errorSummary = "invalid_command: effort control is unavailable"
		return model, nil
	}
	model.effortGeneration++
	generation := model.effortGeneration
	requestID := protocol.RequestID(fmt.Sprintf("tui-effort-%d", generation))
	kind := protocol.CommandSetEffort
	if command.query {
		kind = protocol.CommandQueryEffort
	}
	model.effortFeedback = &effortFeedback{requestID: requestID, generation: generation, draft: model.composer, kind: kind, operationGeneration: model.effortOperationGeneration}
	model.errorSummary = ""
	ctx := model.submitContext
	return model, func() tea.Msg {
		var err error
		if command.query {
			_, err = target.QueryEffort(ctx, requestID)
		} else {
			_, err = target.SetEffort(ctx, requestID, command.choice)
		}
		if err != nil {
			return effortFailedMsg{generation: generation, err: err}
		}
		// 成功只消费 owner 发布的 ControlItem，方法返回不生成第二份回执。
		return nil
	}
}

func (model Model) projectEffortResult(result protocol.CommandResult) (tea.Model, tea.Cmd) {
	feedback := model.effortFeedback
	if feedback == nil || feedback.requestID != result.RequestID() || feedback.kind != result.CommandKind() || feedback.accepted {
		return model, nil
	}
	if summary, rejected := result.Error(); rejected {
		model.errorSummary = string(summary.Code()) + ": " + summary.Message()
		model.effortFeedback = nil
		return model, nil
	}
	owned := *feedback
	owned.accepted = true
	model.effortFeedback = &owned
	return model, nil
}

func effortValueText(value protocol.EffortValuePayload) string {
	if value.State == "omitted" {
		return "omitted (server default unknown)"
	}
	return string(value.Value)
}
func effortSummary(payload protocol.EffortSnapshotPayload) []string {
	first := fmt.Sprintf("Effort: %s · %s · revision %d", effortValueText(payload.Selected), payload.Source, payload.SelectionRevision)
	if payload.Active == nil {
		return []string{first, "Next operation · process only"}
	}
	active := payload.Active
	second := fmt.Sprintf("Captured: %s · %s/%s", effortValueText(active.Effort()), active.Kind(), active.Phase())
	if active.CancelRequested() {
		second += " · cancellation requested"
	}
	if payload.Pending {
		first = "Next effort: " + effortValueText(payload.Selected) + " · " + payload.Source + " · pending"
	}
	return []string{first, second}
}

// observeEffort 分离权威选择水位和局部反馈，迟到快照不能恢复活动状态或焦点。
func (model *Model) observeEffort(payload protocol.EffortSnapshotPayload, local bool) bool {
	if previous := model.effortSelection; previous != nil {
		if payload.SelectionRevision < previous.revision {
			return false
		}
		if payload.SelectionRevision == previous.revision && (payload.Selected != previous.selected || payload.Source != previous.source) {
			return false
		}
	}
	if local || model.effortSelection == nil || payload.SelectionRevision > model.effortSelection.revision {
		model.effortSelection = &effortSelectionObservation{selected: payload.Selected, source: payload.Source, revision: payload.SelectionRevision}
	}
	return local
}

func (model Model) projectEffortEvent(requestID protocol.RequestID, event protocol.Event) (tea.Model, tea.Cmd) {
	payload, err := protocol.DecodeEffortSnapshot(event)
	if err != nil {
		model.errorSummary = "stream_protocol_error: invalid effort snapshot"
		return model, nil
	}
	feedback := model.effortFeedback
	local := feedback != nil && feedback.requestID == requestID && feedback.accepted && feedback.operationGeneration == model.effortOperationGeneration
	if model.observeEffort(payload, local) && local {
		model.composer.history.add(feedback.draft.text)
		model.composer.clear()
		model.effortNotice = effortSummary(payload)
		model.effortFeedback = nil
	}
	return model, nil
}

func (model Model) effortNoticeLines() []string {
	// 输入区优先；每个逻辑通知行最多一屏宽，帮助最多六行、状态最多两行。
	count := model.auxiliaryLayout(model.candidateRows()).effortRows
	lines := make([]string, 0, count)
	for _, line := range model.effortNotice[:count] {
		lines = append(lines, ansi.Truncate(line, max(1, model.widthOrDefault()), "…"))
	}
	return lines
}
