package tui

import (
	"context"
	"fmt"
	"strings"

	"easycode/internal/domain"
	"easycode/internal/protocol"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

type cacheSession interface {
	QueryCache(context.Context, protocol.RequestID) (protocol.CommandResult, error)
}

func (model Model) submitCacheCommand() (tea.Model, tea.Cmd) {
	target, ok := model.session.(cacheSession)
	if !ok {
		model.errorSummary = "invalid_command: cache control is unavailable"
		return model, nil
	}
	model.queryGeneration++
	generation := model.queryGeneration
	model.queryRequestID = protocol.RequestID(fmt.Sprintf("tui-cache-%d", generation))
	model.queryKind = protocol.CommandQueryCache
	snapshot := model.composer
	model.pending = &snapshot
	model.pendingContext = protocol.CommandQueryCache
	model.state = stateSubmitting
	model.errorSummary = ""
	ctx, requestID := model.submitContext, model.queryRequestID
	return model, func() tea.Msg {
		_, err := target.QueryCache(ctx, requestID)
		if err != nil {
			return cacheQueryFailedMsg{generation: generation, err: err}
		}
		return nil
	}
}

type cacheQueryFailedMsg struct {
	generation uint64
	err        error
}

func (model Model) projectCacheEvent(event protocol.Event) (tea.Model, tea.Cmd) {
	payload, err := protocol.DecodeCacheSnapshot(event)
	if err != nil {
		model.errorSummary = "stream_protocol_error: invalid cache snapshot"
		return model, nil
	}
	model.cachePanel = &payload
	model.cacheScroll = 0
	model.contextPanel = nil
	return model, nil
}
func cacheMetric(metric protocol.UsageMetricPayload) string {
	if metric.Value == nil {
		return string(metric.State)
	}
	return fmt.Sprint(*metric.Value)
}
func cacheUsage(label string, usage protocol.UsagePayload) string {
	return fmt.Sprintf("%s: uncached %s · read %s · write %s · output %s · reasoning %s", label, cacheMetric(usage.InputUncached), cacheMetric(usage.CacheRead), cacheMetric(usage.CacheWrite), cacheMetric(usage.Output), cacheMetric(usage.ReasoningOutput))
}
func (model Model) renderCachePanel() string {
	p := model.cachePanel
	if p == nil {
		return ""
	}
	lines := []string{"Cache · process snapshot", fmt.Sprintf("%s/%s · %s · %s", p.Family, p.Wire, safeComposerText(protocol.CacheModelIdentity(p.Model)), p.Activity), "Baseline: " + p.Baseline, fmt.Sprintf("Configured: key %s · %s", p.Policy.Key, p.Policy.Transport), "Observed transport: not observed"}
	if p.Family == domain.ProviderAnthropic {
		lines[3] = "Configured: " + string(p.Policy.Anthropic)
	}
	if p.LastSample != nil {
		lines[4] = fmt.Sprintf("Observed transport: %s · %s · %s", p.LastSample.Transport, p.LastSample.Mode, p.LastSample.Reason)
	}
	lines = append(lines, "Change: "+p.ChangeReason+" "+p.ChangedSource, fmt.Sprintf("Policy changed: %t", p.PolicyChanged))

	for _, entry := range []struct {
		label string
		value *protocol.CacheObservationPayload
	}{{"Last sample", p.LastSample}, {"Last summary", p.LastSummary}} {
		if entry.value == nil {
			lines = append(lines, entry.label+": not observed")
			continue
		}
		o := entry.value
		lines = append(lines, cacheUsage(entry.label, o.Usage))
		ratio := "unknown"
		if o.ReadRatio != nil {
			ratio = fmt.Sprintf("%.1f%%", *o.ReadRatio*100)
		}
		lines = append(lines, fmt.Sprintf("Read ratio: %s · read observed: %t · markers: %d", ratio, o.ReadObserved, o.Markers), "Request: "+o.RequestFingerprint, "Native: "+o.NativeFingerprint+" · "+string(o.Relation), "Fallback: "+string(o.Fallback))
	}
	if p.LastTurn != nil {
		lines = append(lines, cacheUsage("Last successful turn", *p.LastTurn))
	} else {
		lines = append(lines, "Last successful turn: not observed")
	}
	lines = append(lines, "Stable prefix: "+p.StableFingerprint, "Content: "+p.ContentFingerprint)
	for _, s := range p.Sources {
		if s.Present {
			lines = append(lines, s.Kind+" · "+s.Revision+" · "+s.Fingerprint)
		}
	}
	if p.UnexplainedReadDrop {
		lines = append(lines, "unexplained_cache_read_drop · server reason unknown")
	}
	if p.LastFailure != "" {
		lines = append(lines, "Last failure: "+p.LastFailure)
	}
	lines = append(lines, "Equal fingerprints do not prove a server cache hit.", "↑/↓: scroll · Esc: close")
	width, height := max(1, model.widthOrDefault()), max(1, model.heightOrDefault())
	rendered := make([]string, 0, height)
	for _, line := range lines {
		rendered = append(rendered, strings.Split(ansi.Hardwrap(line, width, true), "\n")...)
	}
	offset := min(model.cacheScroll, max(0, len(rendered)-max(1, height-1)))
	rendered = rendered[offset:]
	if len(rendered) > height {
		rendered = rendered[:height]
		rendered[height-1] = ansi.Truncate("Esc: close", width, "")
	}
	for index := range rendered {
		rendered[index] = strings.TrimRight(rendered[index], " \t")
	}
	return strings.Join(rendered, "\n")
}
