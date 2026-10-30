//go:build darwin

package app

import (
	"context"
	"strings"
	"testing"

	"easycode/internal/domain"
	"easycode/internal/protocol"
	"easycode/internal/session"
	"easycode/internal/session/artifact"
	"easycode/internal/tool"
	"easycode/internal/tool/builtin"
)

func TestExecOmittedOutputCanBeReadByToolAndHost(t *testing.T) {
	ctx := context.Background()
	workspacePath, root := privateAppTempDir(t), privateAppTempDir(t)
	layout, err := openCommandArtifactBoundary(ctx, root, workspacePath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = layout.Close() }()
	thread := domain.ThreadID("018f1d8a-7b5c-7def-8123-456789abcdef")
	store, err := artifact.Open(ctx, layout, thread)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	workspace, err := builtin.OpenWorkspace(workspacePath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = workspace.Close() }()
	executor, err := builtin.NewExecExecutor(workspace, builtin.OpenCommandEnvironment(), tool.RenderExecOutput, artifactOutputStore{store})
	if err != nil {
		t.Fatal(err)
	}
	command := `/usr/bin/awk 'BEGIN {for (i=0;i<60000;i++) printf "0123456789"}'; /usr/bin/awk 'BEGIN {for (i=0;i<60000;i++) printf "abcdefghij"}' >&2; exit 2`
	input, _ := tool.NewExecInput(command, "", 0)
	ready, _ := tool.NewExecReadyCall("command", input)
	id, _ := tool.GenerateInvocationID()
	invocation, _ := tool.NewInvocation(id, ready)
	execInvocation, _ := invocation.Exec()
	candidate, err := builtin.NewExecPlanner().Prepare(execInvocation)
	if err != nil {
		t.Fatal(err)
	}
	result := executor.Execute(ctx, candidate, nil)
	meta := result.ExecMetadata()
	exit, known := meta.ExitCode()
	if result.Status() != tool.ResultSuccess || !known || exit != 2 || !meta.OutputTruncated() || len(result.Preview().Text()) > tool.MaxModelPreviewBytes {
		t.Fatalf("execution: %s %s", result.Code(), result.Preview().Text())
	}
	for _, saving := range []tool.ArtifactSaving{meta.StdoutSaving(), meta.StderrSaving()} {
		d, ok := saving.Descriptor()
		if !ok || !d.Complete() || d.SavedBytes() != 600000 || d.ObservedBytes() != 600000 {
			t.Fatalf("lost stream: %+v", d)
		}
	}
	refs, _ := tool.NewArtifactReferences(thread, nil)
	reader := artifactRangeReader{store, refs}
	d, _ := meta.StdoutSaving().Descriptor()
	rangeInput, _ := tool.NewReadArtifactInput(d.ID(), 300000, 20)
	if _, err := reader.ReadArtifact(ctx, rangeInput); err == nil {
		t.Fatal("publication granted authorization")
	}
	if err := refs.AuthorizeCommitted(result); err != nil {
		t.Fatal(err)
	}
	readExecutor, _ := builtin.NewReadArtifactExecutor(reader)
	readReady, _ := tool.NewReadArtifactReadyCall("read-artifact", rangeInput)
	readID, _ := tool.GenerateInvocationID()
	readInvocation, _ := tool.NewReadArtifactInvocation(readID, readReady)
	readResult, err := readExecutor.Execute(ctx, readInvocation).Finalize(tool.MaxModelPreviewBytes)
	if err != nil {
		t.Fatal(err)
	}
	page, ok := readResult.ArtifactPage()
	if !ok || page.Text() != "01234567890123456789" {
		t.Fatalf("omitted middle unavailable: %s", readResult.Preview().Text())
	}
	entry := session.ToolHistoryEntry{Sequence: 1, Invocation: invocation, Result: result, HasResult: true}
	queries := newTranscriptQueries(thread, reader, []session.ToolHistoryEntry{entry})
	defer func() { _ = queries.Close() }()
	hostPage, err := queries.ReadToolOutput(ctx, protocol.ToolOutputQuery{ThreadID: thread, InvocationID: string(id), Stream: protocol.ToolStderr, Offset: 300000, Limit: 20})
	if err != nil || hostPage.Text != "abcdefghijabcdefghij" || !hostPage.SourceComplete {
		t.Fatalf("stderr page: %+v %v", hostPage, err)
	}
	if !strings.Contains(result.Preview().Text(), string(d.ID())) || !strings.Contains(result.Preview().Text(), "ReadArtifact") {
		t.Fatal("frozen preview omitted retrieval hint")
	}
}
