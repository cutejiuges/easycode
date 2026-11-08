package app

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"easycode/internal/domain"
	"easycode/internal/protocol"
	"easycode/internal/session"
	"easycode/internal/tool"
)

func transcriptListItem(node transcriptNode, entry session.ToolHistoryEntry) protocol.TranscriptListItem {
	if node.compaction != nil {
		return protocol.TranscriptListItem{Cursor: node.cursor, Kind: domain.TranscriptCompaction, CompactionID: node.compaction.id, Text: "Conversation compacted", More: true}
	}

	item := protocol.TranscriptListItem{Cursor: node.cursor, Kind: node.item.Kind, TurnID: node.item.TurnID, InputID: node.item.InputID}
	if node.item.Kind == domain.TranscriptReasoning && node.item.Reasoning != nil {
		body := node.item.Reasoning
		item.Reasoning = &protocol.ReasoningSummaryPayload{Source: body.Source(), Availability: body.Availability(), TotalBytes: body.Len()}
		if body.Availability() == domain.ReasoningReadable {
			page, err := body.ReadRange(0, 512, false)
			if err == nil {
				item.Text = page.Text
				item.More = page.NextOffset < page.TotalBytes
			}
		}
		return item
	}
	if node.toolIndex < 0 {
		item.Text = tool.ActivityText(node.item.Text, 512)
		item.More = len(item.Text) < len(node.item.Text) || node.item.HasSkill
		return item
	}
	item.DetailSequence = entry.DetailSequence
	item.Activity = tool.ProjectActivity(entry.Invocation)
	if g := entry.PostHooks; g != nil && entry.HasResult && (g.Terminal != nil || entry.Result.Status() == tool.ResultSuccess) {
		ref := g.Plan.Plan.Hooks[min(len(g.Finished), len(g.Plan.Plan.Hooks)-1)]
		summary := protocol.HookActivityPayload{InvocationID: string(entry.Invocation.InvocationID()), Reference: ref, State: protocol.HookActivityChecking, Code: domain.HookOK}
		if g.Terminal != nil {
			summary.Code = g.Terminal.Code
			switch g.Terminal.Disposition {
			case domain.PostHookCompleted:
				summary.State = protocol.HookActivityCompleted
				for _, f := range g.Finished {
					if f.Result.Code == summary.Code {
						summary.Reference = f.Feedback.Reference
						break
					}
				}
			case domain.PostHookSkipped:
				summary.State = protocol.HookActivitySkipped
			case domain.PostHookCancelled:
				summary.State = protocol.HookActivityState(domain.HookCancel)
			case domain.PostHookUncertain:
				summary.State = protocol.HookActivityState(domain.HookUncertain)
			}
		}
		item.PostHook = summary
	}
	if entry.HasResult {
		r := entry.Result
		item.Preview = tool.ActivityText(r.Preview().Text(), 4096)
		item.Activity.Status, item.Activity.Code = string(r.Status()), r.Code()
		item.ModelShortened = r.Budget().Reason() != tool.BudgetPreserved
		switch r.Capability() {
		case tool.CapabilityQuestion:
			item.Preview = tool.QuestionResultSummary(r)
		case tool.CapabilitySkill:
			item.Preview = tool.SkillResultSummary(r)
		case tool.CapabilityRead:
			item.Activity.Target = r.ReadMetadata().RelativePath()
			item.ModelShortened = item.ModelShortened || r.ReadMetadata().OutputTruncated() || r.ReadMetadata().LongLineTruncated()
		case tool.CapabilityGlob:
			item.Search = toolOutputSummary(r.SearchSaving(), 0)
			m := r.GlobMetadata()
			item.CoverageIncomplete = m.IncompleteReason() != tool.SearchComplete || m.OmittedMatches() > m.ModelOmittedMatches()
		case tool.CapabilityGrep:
			item.Search = toolOutputSummary(r.SearchSaving(), 0)
			m := r.GrepMetadata()
			item.CoverageIncomplete = m.IncompleteReason() != tool.SearchComplete || m.OmittedMatches() > m.ModelOmittedMatches()
		case tool.CapabilityExec:
			m := r.ExecMetadata()
			item.Activity.ExitCode, item.Activity.ExitKnown = m.ExitCode()
			item.Activity.ElapsedMillis = m.DurationMillis()
			item.Activity.StdoutBytes = m.Stdout().ObservedBytes()
			item.Activity.StderrBytes = m.Stderr().ObservedBytes()
			item.Activity.Truncated = m.OutputTruncated()
			item.ModelShortened = item.ModelShortened || m.OutputTruncated()
			item.Stdout = toolOutputSummary(m.StdoutSaving(), m.Stdout().ObservedBytes())
			item.Stderr = toolOutputSummary(m.StderrSaving(), m.Stderr().ObservedBytes())
		case tool.CapabilityPatch, tool.CapabilityWrite:
			item.SummaryOnly = true
		}
	}
	return item
}

func (q *transcriptQueries) ListTranscript(ctx context.Context, query protocol.TranscriptListQuery) (protocol.TranscriptListPage, error) {
	if query.Validate() != nil || query.ThreadID != q.thread {
		return protocol.TranscriptListPage{}, tool.ArtifactReadUnauthorized
	}
	ctx, done, err := q.begin(ctx)
	if err != nil {
		return protocol.TranscriptListPage{}, err
	}
	defer done()
	q.mu.Lock()
	nodes := q.nodes
	if query.Mode == protocol.TranscriptMain {
		nodes = q.mainNodes
	}
	end := len(nodes)
	if query.Through > 0 {
		end = sort.Search(end, func(i int) bool { return nodes[i].cursor.Sequence > query.Through })
	}
	lower := 0
	if query.Newer && query.Before.Valid() {
		lower = sort.Search(end, func(i int) bool { return query.Before.Before(nodes[i].cursor) })
		end = min(end, lower+int(query.Limit))
	} else if query.Before.Valid() {
		end = sort.Search(end, func(i int) bool { return !nodes[i].cursor.Before(query.Before) })
	}
	lower = max(lower, end-int(query.Limit))
	type selectedNode struct {
		node  transcriptNode
		entry session.ToolHistoryEntry
	}
	selected := make([]selectedNode, 0, query.Limit)
	for i := end - 1; i >= lower && len(selected) < int(query.Limit); i-- {
		node := nodes[i]
		var entry session.ToolHistoryEntry
		if node.toolIndex >= 0 {
			entry = q.entries[node.toolIndex]
			if entry.PostHooks != nil {
				g := entry.PostHooks.Clone()
				entry.PostHooks = &g
			}
		}
		selected = append(selected, selectedNode{node, entry})
	}
	q.mu.Unlock()
	items := make([]protocol.TranscriptListItem, len(selected))
	for i, value := range selected {
		items[i] = transcriptListItem(value.node, value.entry)
	}
	if err := ctx.Err(); err != nil {
		return protocol.TranscriptListPage{}, err
	}
	page := protocol.TranscriptListPage{Items: items}
	if end > len(items) {
		page.NextBefore = items[len(items)-1].Cursor
	}
	return protocol.NewTranscriptListPage(page)
}

func (q *transcriptQueries) ReadTranscriptBody(ctx context.Context, query protocol.TranscriptBodyQuery) (protocol.TranscriptBodyPage, error) {
	if query.Validate() != nil || query.ThreadID != q.thread {
		return protocol.TranscriptBodyPage{}, tool.ArtifactReadUnauthorized
	}
	ctx, done, err := q.begin(ctx)
	if err != nil {
		return protocol.TranscriptBodyPage{}, err
	}
	defer done()
	q.mu.Lock()
	index := sort.Search(len(q.nodes), func(i int) bool { return !q.nodes[i].cursor.Before(query.Cursor) })
	if index == len(q.nodes) || q.nodes[index].cursor != query.Cursor {
		q.mu.Unlock()
		return protocol.TranscriptBodyPage{}, tool.ArtifactReadUnavailable
	}
	node := q.nodes[index]
	var entry session.ToolHistoryEntry
	if node.toolIndex >= 0 {
		entry = q.entries[node.toolIndex]
		if entry.PostHooks != nil {
			g := entry.PostHooks.Clone()
			entry.PostHooks = &g
		}
	}
	q.mu.Unlock()
	if query.Source == protocol.TranscriptPostHooks && query.DetailSequence != entry.DetailSequence {
		return protocol.TranscriptBodyPage{}, protocol.TranscriptChanged
	}
	if err := ctx.Err(); err != nil {
		return protocol.TranscriptBodyPage{}, err
	}
	if node.item.Kind == domain.TranscriptReasoning {
		if query.Source != protocol.TranscriptText {
			return protocol.TranscriptBodyPage{}, tool.ArtifactReadInvalidRange
		}
		if node.item.Reasoning == nil || node.item.Reasoning.Availability() != domain.ReasoningReadable {
			return protocol.TranscriptBodyPage{}, tool.ArtifactReadUnavailable
		}
		page, err := node.item.Reasoning.ReadRange(query.Offset, query.Limit, query.Backward)
		if err != nil {
			return protocol.TranscriptBodyPage{}, tool.ArtifactReadInvalidRange
		}
		return protocol.NewTranscriptBodyPage(protocol.TranscriptBodyPage{Offset: page.Offset, NextOffset: page.NextOffset, TotalBytes: page.TotalBytes, Text: page.Text})
	}
	text := ""
	if node.toolIndex < 0 {
		if query.Source != protocol.TranscriptText {
			return protocol.TranscriptBodyPage{}, tool.ArtifactReadInvalidRange
		}
		text = node.item.Text
		if node.item.HasSkill {
			text += "\n\n--- Skill attachment ---\n" + node.item.Skill.Content
		}
		if node.compaction != nil {
			text = node.compaction.summary.Text()
		}
	} else {
		switch query.Source {
		case protocol.TranscriptInput:
			if question, ok := entry.Invocation.Question(); ok {
				text = tool.QuestionInputText(question.Input())
			} else if invocation, ok := entry.Invocation.Exec(); ok {
				text = invocation.Input().Command()
			} else {
				activity := tool.ProjectActivity(entry.Invocation)
				text = string(activity.Tool) + ": " + activity.Summary
			}
		case protocol.TranscriptResult:
			if !entry.HasResult {
				return protocol.TranscriptBodyPage{}, tool.ArtifactReadUnavailable
			}
			text = entry.Result.Preview().Text()
			if question, ok := entry.Invocation.Question(); ok {
				text = tool.QuestionResultText(question.Input(), entry.Result)
			}
			for _, h := range entry.Hooks {
				if h.Result.Identity.Event != domain.HookPreToolUse {
					continue
				}
				text += fmt.Sprintf("\n\nCheck %s:%s · %s (%s) · timeout %ds · stdout %d bytes · stderr %d bytes\nDetailed hook output was not recorded.", h.Reference.Source, h.Reference.ID, h.Result.Status, h.Result.Code, h.Reference.TimeoutSeconds, h.Result.StdoutBytes, h.Result.StderrBytes)
			}
		case protocol.TranscriptPostHooks:
			if entry.PostHooks == nil {
				return protocol.TranscriptBodyPage{}, tool.ArtifactReadUnavailable
			}
			g := entry.PostHooks
			text = "PostToolUse checks · feedback delivery: " + string(g.Plan.Delivery)
			for _, f := range g.Finished {
				text += fmt.Sprintf("\n\nCheck %s:%s · %s (%s) · stdout %d bytes · stderr %d bytes", f.Feedback.Reference.Source, f.Feedback.Reference.ID, f.Result.Status, f.Result.Code, f.Result.StdoutBytes, f.Result.StderrBytes)
			}
			if g.Started != nil {
				text += "\n\nCheck in progress"
			}
			if g.Terminal != nil {
				text += fmt.Sprintf("\n\nPostToolUse: %s (%s) · processed %d/%d", g.Terminal.Disposition, g.Terminal.Code, g.Terminal.Processed, len(g.Plan.Plan.Hooks))
			}
			feedback, err := tool.RenderPostFeedback(g.Feedback())
			if err != nil {
				return protocol.TranscriptBodyPage{}, err
			}
			text += feedback
		default:
			return protocol.TranscriptBodyPage{}, tool.ArtifactReadInvalidRange
		}
	}
	if query.Offset > uint64(len(text)) || (query.Offset < uint64(len(text)) && !utf8.RuneStart(text[query.Offset])) {
		return protocol.TranscriptBodyPage{}, tool.ArtifactReadInvalidRange
	}
	start := query.Offset
	end := min(uint64(len(text)), start+uint64(query.Limit))
	if query.Backward {
		end = start
		start -= min(start, uint64(query.Limit))
		for start < end && !utf8.RuneStart(text[start]) {
			start++
		}
	}
	for end > start && end < uint64(len(text)) && !utf8.RuneStart(text[end]) {
		end--
	}
	if end == start && end < uint64(len(text)) {
		return protocol.TranscriptBodyPage{}, tool.ArtifactReadInvalidRange
	}
	detailSequence := uint64(0)
	if query.Source == protocol.TranscriptPostHooks {
		detailSequence = entry.DetailSequence
	}
	return protocol.NewTranscriptBodyPage(protocol.TranscriptBodyPage{DetailSequence: detailSequence, Offset: start, NextOffset: end, TotalBytes: uint64(len(text)), Text: strings.Clone(text[start:end])})
}
