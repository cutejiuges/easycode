//go:build darwin

package app

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
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
		command := writeScenarioCall{"exec_command", `{"command":"/usr/bin/awk 'BEGIN {for (i=0;i<20000;i++) printf \"0123456789\"}'; printf warning >&2"}`}
		switch n {
		case 1:
			calls = []writeScenarioCall{command, {"Write", `{"file_path":"guarded","content":"original"}`}}
		case 2:
			calls = []writeScenarioCall{{"Read", `{"file_path":"guarded"}`}, {"Write", `{"file_path":"source-grant","content":"created with the session grant"}`}}
		case 5:
			calls = []writeScenarioCall{command, {"Write", `{"file_path":"guarded","content":"must not overwrite"}`}, {"Write", `{"file_path":"branch-grant","content":"must ask again"}`}}
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
	sourceActions := submitBranchArtifactTurn(t, ctx, source, "guarded", protocol.ApprovalAllowSession)
	if read := sourceActions["guarded"]; read.Tool != protocol.ToolRead || read.Code != "ok" || read.Truncated {
		t.Fatalf("source did not establish complete Read evidence: %+v", read)
	}
	if content, err := os.ReadFile(filepath.Join(workspace, "source-grant")); err != nil || string(content) != "created with the session grant" {
		t.Fatal("source did not exercise a session-scope Write grant", err)
	}
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

	const excluded = "source follow-up after command"
	appEffortTurn(t, source, excluded, false)
	waitCompactionIdle(t, source)
	branch := activateHistoricalBranchFixture(t, ctx, owner, source, candidates, excluded)
	branchThread := branch.identity.ThreadID
	list := branchArtifactList(t, ctx, branch)
	if len(list) != 1 || list[0].Preview != inherited.Preview ||
		list[0].Stdout.Status != inherited.Stdout.Status || list[0].Stdout.SavedBytes != inherited.Stdout.SavedBytes ||
		list[0].Stdout.Readable || list[0].Stdout.SourceThreadID != sourceThread ||
		list[0].Stderr.Readable || list[0].Stderr.SourceThreadID != sourceThread || samples.Load() != 4 {
		t.Fatalf("branch altered historical output or its authority: %+v", list)
	}
	query.ThreadID = branchThread
	if _, err := branch.transcriptQueries.ReadToolOutput(ctx, query); !errors.Is(err, tool.ArtifactReadUnauthorized) {
		t.Fatalf("inherited raw output query returned %v", err)
	}
	branchActions := submitBranchArtifactTurn(t, ctx, branch, "branch-grant", protocol.ApprovalDeny)
	list = branchArtifactList(t, ctx, branch)
	if len(list) != 2 || !list[0].Stderr.Readable || list[0].Stderr.SourceThreadID != "" ||
		list[1].Stderr.SourceThreadID != sourceThread || list[1].Stderr.Readable {
		t.Fatalf("current branch and ancestor output authorities were merged: %+v", list)
	}
	if branchActions["guarded"].Code != "write_requires_full_read" || branchActions["branch-grant"].Code != "approval_denied" {
		t.Fatalf("branch inherited source Read evidence or Write grant: %+v", branchActions)
	}
	if content, err := os.ReadFile(filepath.Join(workspace, "guarded")); err != nil || string(content) != "original" {
		t.Fatal("branch overwrote the source-read file", err)
	}
	if _, err := os.Stat(filepath.Join(workspace, "branch-grant")); !os.IsNotExist(err) {
		t.Fatal("branch created a file after fresh approval was denied", err)
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

func submitBranchArtifactTurn(t *testing.T, ctx context.Context, resources *chatResources, writePath string, writeDecision protocol.ApprovalDecision) map[string]protocol.ToolActivityPayload {
	t.Helper()
	events, err := submitInteractiveFixture(t, resources.session, ctx, "run fixture command")
	if err != nil {
		t.Fatal(err)
	}
	var execApprovals, writeApprovals int
	var completed bool
	fileActions := make(map[string]protocol.ToolActivityPayload)
	for event := range events {
		switch event.Kind {
		case protocol.EventToolApprovalRequested:
			p, err := protocol.DecodeToolApprovalRequested(event)
			if err != nil {
				t.Fatal(err)
			}
			var decision protocol.ApprovalDecision
			switch p.Capability {
			case string(tool.CapabilityExec):
				execApprovals++
				decision = protocol.ApprovalAllowOnce
			case string(tool.CapabilityWrite):
				writeApprovals++
				if p.RelativePath != writePath {
					t.Fatalf("unexpected Write approval for %q", p.RelativePath)
				}
				decision = writeDecision
			default:
				t.Fatalf("unexpected approval for %q", p.Capability)
			}
			if err := resources.session.ResolveApproval(ctx, event.TurnID, p, decision, ""); err != nil {
				t.Fatal(err)
			}
		case protocol.EventToolTerminal:
			p, err := protocol.DecodeToolTerminal(event)
			if err != nil {
				t.Fatal(err)
			}
			if p.Tool == protocol.ToolRead || p.Tool == protocol.ToolWrite {
				fileActions[p.Target] = p.ToolActivityPayload
			}
		case protocol.EventTurnCompleted:
			completed = true
		}
	}
	if execApprovals != 1 || writeApprovals != 1 || !completed {
		t.Fatalf("fixture turn approval and execution: Exec=%d Write=%d completed=%t", execApprovals, writeApprovals, completed)
	}
	waitCompactionIdle(t, resources)
	return fileActions
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
