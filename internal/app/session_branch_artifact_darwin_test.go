//go:build darwin

package app

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"easycode/internal/config"
	"easycode/internal/domain"
	"easycode/internal/protocol"
	"easycode/internal/secret"
	"easycode/internal/tool"
)

func TestBranchArtifactHistoryKeepsOutputAuthorityWithItsProducer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var samples atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		n := int(samples.Add(1))
		var calls []writeScenarioCall
		if n == 1 || n == 3 {
			calls = []writeScenarioCall{{"exec_command", `{"command":"/usr/bin/awk 'BEGIN {for (i=0;i<20000;i++) printf \"0123456789\"}'; printf warning >&2"}`}}
		}
		serveWriteScenarioSample(w, domain.ProviderOpenAI, n, calls)
	}))
	defer server.Close()
	workspace, home := privateAppTempDir(t), privateAppTempDir(t)
	paths := dataPaths{sessionRoot: filepath.Join(home, "sessions"), catalogPath: filepath.Join(home, "catalog.json")}
	cfg := config.Config{Provider: config.Provider{
		Family: domain.ProviderOpenAI, Model: "fixture-model", BaseURL: server.URL, APIKey: secret.New("artifact-branch-fixture"),
	}}
	owner, source, candidates := startBranchFixture(t, ctx, cfg, paths, workspace)
	submitBranchArtifactTurn(t, ctx, source)
	sourceThread := source.identity.ThreadID
	sourceList := branchArtifactList(t, ctx, source)
	inherited := sourceList[0]
	if !inherited.Stdout.Readable || !inherited.Stderr.Readable || inherited.Stdout.Status != "complete" {
		t.Fatalf("source did not save separate output streams: %+v", inherited)
	}
	query := protocol.ToolOutputQuery{ThreadID: sourceThread, InvocationID: inherited.Activity.InvocationID, Stream: protocol.ToolStderr, Limit: 512}
	before, err := source.transcriptQueries.ReadToolOutput(ctx, query)
	if err != nil || before.Text != "warning" {
		t.Fatalf("source output unavailable: %+v, %v", before, err)
	}

	branch := activateBranchFixture(t, ctx, owner, source, candidates, "")
	branchThread := branch.identity.ThreadID
	list := branchArtifactList(t, ctx, branch)
	if len(list) != 1 || list[0].Preview != inherited.Preview ||
		list[0].Stdout.Status != inherited.Stdout.Status || list[0].Stdout.SavedBytes != inherited.Stdout.SavedBytes ||
		list[0].Stdout.Readable || list[0].Stdout.SourceThreadID != sourceThread ||
		list[0].Stderr.Readable || list[0].Stderr.SourceThreadID != sourceThread || samples.Load() != 2 {
		t.Fatalf("branch altered historical output or its authority: %+v", list)
	}
	query.ThreadID = branchThread
	if _, err := branch.transcriptQueries.ReadToolOutput(ctx, query); !errors.Is(err, tool.ArtifactReadUnauthorized) {
		t.Fatalf("inherited raw output query returned %v", err)
	}
	submitBranchArtifactTurn(t, ctx, branch)
	list = branchArtifactList(t, ctx, branch)
	if len(list) != 2 || !list[0].Stderr.Readable || list[0].Stderr.SourceThreadID != "" ||
		list[1].Stderr.SourceThreadID != sourceThread || list[1].Stderr.Readable {
		t.Fatalf("current branch and ancestor output authorities were merged: %+v", list)
	}
	query.InvocationID = list[0].Activity.InvocationID
	own, err := branch.transcriptQueries.ReadToolOutput(ctx, query)
	if err != nil || own.Text != "warning" {
		t.Fatalf("branch-owned output unavailable: %+v, %v", own, err)
	}
	restored, err := openChatResources(ctx, cfg, paths.sessionRoot, string(sourceThread), workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer closeResumeTestResources(t, restored)
	query.ThreadID, query.InvocationID = sourceThread, inherited.Activity.InvocationID
	after, err := restored.transcriptQueries.ReadToolOutput(ctx, query)
	if err != nil || !bytes.Equal([]byte(before.Text), []byte(after.Text)) {
		t.Fatalf("branch activity touched the source object: %+v, %v", after, err)
	}
}

func submitBranchArtifactTurn(t *testing.T, ctx context.Context, resources *chatResources) {
	t.Helper()
	events, err := submitInteractiveFixture(t, resources.session, ctx, "run fixture command")
	if err != nil {
		t.Fatal(err)
	}
	var approved, completed bool
	for event := range events {
		switch event.Kind {
		case protocol.EventToolApprovalRequested:
			p, err := protocol.DecodeToolApprovalRequested(event)
			if err != nil {
				t.Fatal(err)
			}
			if err := resources.session.ResolveApproval(ctx, event.TurnID, p, protocol.ApprovalAllowOnce, ""); err != nil {
				t.Fatal(err)
			}
			approved = true
		case protocol.EventTurnCompleted:
			completed = true
		}
	}
	if !approved || !completed {
		t.Fatal("fixture command did not complete its actual approval and execution")
	}
	waitCompactionIdle(t, resources)
}

func branchArtifactList(t *testing.T, ctx context.Context, resources *chatResources) []protocol.TranscriptListItem {
	t.Helper()
	page, err := resources.transcriptQueries.ListTranscript(ctx, protocol.TranscriptListQuery{
		Mode: protocol.TranscriptDetail, ThreadID: resources.identity.ThreadID, Limit: 50,
	})
	if err != nil {
		t.Fatal(err)
	}
	var calls []protocol.TranscriptListItem
	for _, item := range page.Items {
		if item.Activity.Tool == protocol.ToolShell {
			calls = append(calls, item)
		}
	}
	if len(calls) == 0 {
		t.Fatal("commands were not projected")
	}
	return calls
}
