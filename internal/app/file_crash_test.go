package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"easycode/internal/config"
	"easycode/internal/domain"
	"easycode/internal/protocol"
	"easycode/internal/provider"
	"easycode/internal/secret"
	"easycode/internal/session"
)

type fileCrashAck struct {
	Thread domain.ThreadID `json:"thread"`
	Path   string          `json:"path"`
}

// TestFileInputCrashHelper 在真实 Sync 后、Finalize 前保留进程和 lease，供父进程确定性杀死。
func TestFileInputCrashHelper(t *testing.T) {
	if os.Getenv("EASYCODE_FILE_CRASH_HELPER") != "1" {
		return
	}
	family := domain.ProviderFamily(os.Getenv("EASYCODE_FILE_CRASH_FAMILY"))
	cfg := config.Config{Provider: config.Provider{Family: family, BaseURL: "https://example.invalid", Model: "fixture-model", APIKey: secret.New("fixture-key")}}
	r, err := openChatResources(context.Background(), cfg, os.Getenv("EASYCODE_FILE_CRASH_ROOT"), "", os.Getenv("EASYCODE_FILE_CRASH_WORKSPACE"))
	if err != nil {
		t.Fatal(err)
	}
	draft, _ := domain.NewInteractiveInputDraft("inspect @file#L2-4")
	command, _ := protocol.NewSubmitConversationalInputDraftCommand("crash-input", draft)
	command, err = r.runtime.PrepareInput(context.Background(), command)
	if err != nil {
		t.Fatal(err)
	}
	turn := domain.TurnID("01900000-0000-7000-8000-000000000010")
	input, _ := domain.NewUserInput(domain.InputID(turn), command.Text())
	input, err = input.WithFileAttachments(command.FileAttachments())
	if err != nil {
		t.Fatal(err)
	}
	a, _ := domain.NewInputAssociation(turn, domain.InputInitial, 0, []domain.InputID{input.ID()})
	c, err := r.provider.NewConversation(r.identity.ThreadID)
	if err != nil {
		t.Fatal(err)
	}
	p, err := c.(provider.UserInputPreparer).PrepareUserInputs(a, []domain.UserInput{input})
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := p.Envelope()
	if err != nil {
		t.Fatal(err)
	}
	started, _ := session.NewTurnStartedDraft(turn)
	if _, err := r.writer.AppendBatch(context.Background(), []session.RecordDraft{started}); err != nil {
		t.Fatal(err)
	}
	association, _ := session.NewTurnInputsCommittedDraft(a)
	native, _ := session.NewProviderNativeCommitDraft(turn, session.NativeCommitPayload{Provider: family, Wire: envelope.Wire(), Payload: envelope.Payload()})
	if _, err := r.writer.AppendBatch(context.Background(), []session.RecordDraft{association, native}); err != nil {
		t.Fatal(err)
	}
	path, _ := r.service.repository.JournalPath(r.identity.ThreadID)
	// 故障注入由当前独占 owner 追加半行，模拟输入提交后下一次写入中断。
	tail, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tail.WriteString(`{"torn":`); err != nil {
		t.Fatal(err)
	}
	if err := tail.Sync(); err != nil {
		t.Fatal(err)
	}
	if err := tail.Close(); err != nil {
		t.Fatal(err)
	}
	if err := json.NewEncoder(os.Stdout).Encode(fileCrashAck{r.identity.ThreadID, path}); err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, os.Stdin)
	t.Fatal("crash helper was not killed")
}

func TestFileInputRealProcessCrashLeaseRepairAndRepeatedOfflineRecovery(t *testing.T) {
	for _, family := range []domain.ProviderFamily{domain.ProviderOpenAI, domain.ProviderAnthropic} {
		t.Run(string(family), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			root := filepath.Join(privateAppTempDir(t), "sessions")
			workspace := privateAppTempDir(t)
			if err := os.WriteFile(filepath.Join(workspace, "file"), []byte("first\r\nFROZEN_CRASH\nlast"), 0600); err != nil {
				t.Fatal(err)
			}
			child := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestFileInputCrashHelper$")
			child.Env = append(os.Environ(), "EASYCODE_FILE_CRASH_HELPER=1", "EASYCODE_FILE_CRASH_ROOT="+root, "EASYCODE_FILE_CRASH_WORKSPACE="+workspace, "EASYCODE_FILE_CRASH_FAMILY="+string(family))
			child.Stderr = os.Stderr
			stdout, err := child.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			stdin, err := child.StdinPipe()
			if err != nil {
				t.Fatal(err)
			}
			defer stdin.Close()
			if err := child.Start(); err != nil {
				t.Fatal(err)
			}
			waited := false
			defer func() {
				if !waited {
					_ = child.Process.Kill()
					_ = child.Wait()
				}
			}()
			var ack fileCrashAck
			if err := json.NewDecoder(stdout).Decode(&ack); err != nil {
				t.Fatal("child did not sync input", err)
			}
			before, err := os.ReadFile(ack.Path)
			if err != nil {
				t.Fatal(err)
			}
			probe := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestHostLeaseProbe$")
			probe.Env = append(os.Environ(), "EASYCODE_HOST_LEASE_ROOT="+root, "EASYCODE_HOST_LEASE_THREAD="+string(ack.Thread), "EASYCODE_HOST_LEASE_EXPECT=busy")
			if output, err := probe.CombinedOutput(); err != nil {
				t.Fatalf("lease competition failed: %v %s", err, output)
			}
			cfg := config.Config{Provider: config.Provider{Family: family, BaseURL: "https://example.invalid", Model: "fixture-model", APIKey: secret.New("fixture-key")}}
			if r, err := openChatResources(ctx, cfg, root, string(ack.Thread), workspace); err == nil {
				_ = r.close(context.Background())
				t.Fatal("occupied journal admitted repair")
			}
			after, _ := os.ReadFile(ack.Path)
			if !bytes.Equal(before, after) {
				t.Fatal("occupied recovery changed bytes")
			}
			if err := child.Process.Kill(); err != nil {
				t.Fatal(err)
			}
			_ = child.Wait()
			waited = true
			if err := os.Remove(filepath.Join(workspace, "file")); err != nil {
				t.Fatal(err)
			}
			preview, cleanup, err := previewSession(ctx, root, ack.Thread, defaultPreviewDependencies())
			if err != nil || cleanup || !preview.Incomplete || preview.Messages[0].Files.Total() != 1 {
				t.Fatal("offline preview after crash failed", err)
			}
			var stable []byte
			for i := 0; i < 2; i++ {
				r, err := openChatResources(ctx, cfg, root, string(ack.Thread), workspace)
				if err != nil {
					t.Fatal(err)
				}
				message := r.history.Turns[0].Messages[0]
				if message.Text != "inspect @file#L2-4" || message.Files.Items()[0].Content() != "FROZEN_CRASH\nlast" {
					t.Fatal("crashed input lost frozen content")
				}
				if err := r.close(context.Background()); err != nil {
					t.Fatal(err)
				}
				data, _ := os.ReadFile(ack.Path)
				if i == 0 {
					stable = data
				} else if !bytes.Equal(stable, data) {
					t.Fatal("second restore appended duplicate recovery")
				}
				lines := bytes.Split(bytes.TrimSuffix(data, []byte("\n")), []byte("\n"))
				if len(lines) != 6 {
					t.Fatal("unexpected recovery record count", len(lines))
				}
				for index, line := range lines {
					record, err := session.DecodeRecord(line)
					if err != nil || record.Sequence != uint64(index+1) {
						t.Fatal("non-monotonic recovered sequence", err)
					}
				}
			}
		})
	}
}
