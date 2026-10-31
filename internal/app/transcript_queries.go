package app

import (
	"context"
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
	if node.toolIndex < 0 {
		item.Text = tool.ActivityText(node.item.Text, 512)
		item.More = len(item.Text) < len(node.item.Text)
		return item
	}
	item.Activity = tool.ProjectActivity(entry.Invocation)
	if entry.HasResult {
		r := entry.Result
		item.Preview = tool.ActivityText(r.Preview().Text(), 4096)
		item.Activity.Status, item.Activity.Code = string(r.Status()), r.Code()
		item.ModelShortened = r.Budget().Reason() != tool.BudgetPreserved
		switch r.Capability() {
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
	end := len(q.nodes)
	if query.Through > 0 {
		end = sort.Search(end, func(i int) bool { return q.nodes[i].cursor.Sequence > query.Through })
	}
	if query.Newer && query.Before.Valid() {
		start := sort.Search(end, func(i int) bool { return query.Before.Before(q.nodes[i].cursor) })
		end = min(end, start+int(query.Limit))
	} else if query.Before.Valid() {
		end = sort.Search(end, func(i int) bool { return !q.nodes[i].cursor.Before(query.Before) })
	}
	type selectedNode struct {
		node  transcriptNode
		entry session.ToolHistoryEntry
	}
	selected := make([]selectedNode, 0, query.Limit)
	for i := end - 1; i >= 0 && len(selected) < int(query.Limit); i-- {
		node := q.nodes[i]
		var entry session.ToolHistoryEntry
		if node.toolIndex >= 0 {
			entry = q.entries[node.toolIndex]
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
	}
	q.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return protocol.TranscriptBodyPage{}, err
	}
	text := ""
	if node.toolIndex < 0 {
		if query.Source != protocol.TranscriptText {
			return protocol.TranscriptBodyPage{}, tool.ArtifactReadInvalidRange
		}
		text = node.item.Text
		if node.compaction != nil {
			text = node.compaction.summary.Text()
		}
	} else {
		switch query.Source {
		case protocol.TranscriptInput:
			if invocation, ok := entry.Invocation.Exec(); ok {
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
	return protocol.NewTranscriptBodyPage(protocol.TranscriptBodyPage{Offset: start, NextOffset: end, TotalBytes: uint64(len(text)), Text: strings.Clone(text[start:end])})
}
