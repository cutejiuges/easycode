package app

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/protocol"
	"easycode/internal/tui"
	tea "github.com/charmbracelet/bubbletea"
)

type countedFileTranscript struct {
	*transcriptQueries
	reads int
}

func (q *countedFileTranscript) ReadTranscriptBody(ctx context.Context, query protocol.TranscriptBodyQuery) (protocol.TranscriptBodyPage, error) {
	q.reads++
	return q.transcriptQueries.ReadTranscriptBody(ctx, query)
}

func TestFileTranscriptAllOmittedSummariesStillExposeEveryFrozenPage(t *testing.T) {
	var attachments []domain.FileAttachment
	var refs []string
	for i := 0; i < 8; i++ {
		path := fmt.Sprintf("%d/", i) + strings.Repeat("p", 4094)
		ref, err := domain.NewFileReference(path, domain.NewWholeFileSelection())
		if err != nil {
			t.Fatal(err)
		}
		a, err := domain.FreezeFileAttachment(ref, fmt.Sprintf("BODY_%d:", i)+strings.Repeat("中", 30000))
		if err != nil {
			t.Fatal(err)
		}
		attachments = append(attachments, a)
		refs = append(refs, ref.String())
	}
	files, err := domain.NewFileAttachments(attachments)
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Join(refs, " ")
	turn := domain.TurnID("01900000-0000-7000-8000-000000000010")
	thread := domain.ThreadID("01900000-0000-7000-8000-000000000001")
	item, err := domain.NewTranscriptUser(domain.TranscriptPosition{}, turn, domain.InputID(turn), text)
	if err != nil {
		t.Fatal(err)
	}
	item.Files = files
	if err := item.Validate(); err != nil {
		t.Fatal(err)
	}
	q := newTranscriptQueries(thread, nil, nil)
	q.nodes = []transcriptNode{{cursor: protocol.TranscriptCursor{Sequence: 5}, item: item, toolIndex: -1}}
	q.mainNodes = q.nodes
	defer q.Close()
	list, err := q.ListTranscript(context.Background(), protocol.TranscriptListQuery{Mode: protocol.TranscriptMain, ThreadID: thread, Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Items) != 1 || list.Items[0].Files.Total() != 8 || list.Items[0].Files.Omitted() != 8 || len(list.Items[0].Files.Items()) != 0 || !list.Items[0].More || strings.Contains(list.Items[0].Text, "BODY_") {
		t.Fatal("oversized summary was confused with complete attachments")
	}
	query := protocol.TranscriptBodyQuery{ThreadID: thread, Cursor: q.nodes[0].cursor, Source: protocol.TranscriptText, Limit: 64 << 10}
	var body strings.Builder
	pages := 0
	for {
		page, err := q.ReadTranscriptBody(context.Background(), query)
		if err != nil || page.Validate() != nil {
			t.Fatal("invalid file detail page", err)
		}
		pages++
		body.WriteString(page.Text)
		if page.NextOffset == page.TotalBytes {
			break
		}
		query.Offset = page.NextOffset
	}
	if pages <= 4 {
		t.Fatal("fixture did not exceed detail cache")
	}
	for i := 0; i < 8; i++ {
		if strings.Count(body.String(), fmt.Sprintf("BODY_%d:", i)) != 1 || !strings.Contains(body.String(), refs[i]) {
			t.Fatal("pagination lost source or body", i)
		}
	}
	if !strings.Contains(body.String(), "truncated: true; partial last line: true") {
		t.Fatal("details lost partial line semantics")
	}
	counted := &countedFileTranscript{transcriptQueries: q}
	var model tea.Model = tui.NewModel(context.Background(), "test", nil).WithTranscriptQueries(thread, counted)
	completionKey(t, &model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("kept draft")})
	completionKey(t, &model, tea.KeyMsg{Type: tea.KeyCtrlO})
	completionKey(t, &model, tea.KeyMsg{Type: tea.KeyEnter})
	for i := 0; i < 7; i++ {
		completionKey(t, &model, tea.KeyMsg{Type: tea.KeyPgDown})
	}
	before := counted.reads
	completionKey(t, &model, tea.KeyMsg{Type: tea.KeyPgUp})
	completionKey(t, &model, tea.KeyMsg{Type: tea.KeyPgUp})
	if counted.reads != before {
		t.Fatal("recent file pages left four-page cache")
	}
	completionKey(t, &model, tea.KeyMsg{Type: tea.KeyPgUp})
	if counted.reads != before+1 {
		t.Fatal("file detail cache retained unbounded pages")
	}
	completionKey(t, &model, tea.KeyMsg{Type: tea.KeyCtrlO})
	if !strings.Contains(model.View(), "> kept draft|") {
		t.Fatal("file details changed composer")
	}
}
