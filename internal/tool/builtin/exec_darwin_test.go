//go:build darwin

package builtin

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"easycode/internal/executor/process"
	"easycode/internal/tool"
)

func TestMain(m *testing.M) {
	if handled, err := RunCommandChild(os.Args[1:]); handled {
		if err != nil {
			os.Exit(125)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestDarwinExecWorkdirDescriptorSurvivesSymlinkSwap(t *testing.T) {
	for _, afterOpen := range []bool{false, true} {
		t.Run(strconv.FormatBool(afterOpen), func(t *testing.T) {
			root, outside := t.TempDir(), t.TempDir()
			if err := os.Mkdir(filepath.Join(root, "work"), 0700); err != nil {
				t.Fatal(err)
			}
			workspace, err := OpenWorkspace(root)
			if err != nil {
				t.Fatal(err)
			}
			defer workspace.Close()
			swapped := false
			swap := func(relative string) {
				if relative != "work" || swapped {
					return
				}
				swapped = true
				if err := os.Rename(filepath.Join(root, "work"), filepath.Join(root, "original")); err != nil {
					t.Error(err)
					return
				}
				if err := os.Symlink(outside, filepath.Join(root, "work")); err != nil {
					t.Error(err)
				}
			}
			if afterOpen {
				workspace.hooks.afterOpenComponent = swap
			} else {
				workspace.hooks.beforeOpenComponent = swap
			}
			executor, err := NewExecExecutor(workspace, OpenCommandEnvironment(), tool.RenderExecOutput, testCommandOutputStore{})
			if err != nil {
				t.Fatal(err)
			}
			input, _ := tool.NewExecInput("printf anchored > marker", "work", 1000)
			call, _ := tool.NewExecReadyCall("call-cwd", input)
			id, _ := tool.ParseInvocationID("018f1d8a-7b5c-7def-8abc-456789abcdef")
			invocation, _ := tool.NewExecInvocation(id, call)
			candidate, _ := NewExecPlanner().Prepare(invocation)
			result := executor.Execute(context.Background(), candidate, nil)
			if !swapped {
				t.Fatal("swap hook did not run")
			}
			if _, err := os.Stat(filepath.Join(outside, "marker")); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("command escaped through swapped symlink")
			}
			if afterOpen {
				content, err := os.ReadFile(filepath.Join(root, "original", "marker"))
				if err != nil || string(content) != "anchored" || result.Status() != tool.ResultSuccess {
					t.Fatalf("opened workdir identity was not retained: %s %v", result.Code(), err)
				}
			} else if result.Status() != tool.ResultError {
				t.Fatalf("pre-open symlink accepted: %s", result.Code())
			}
		})
	}
}

func TestDarwinSandboxDeniesOutsideWritesAndNetwork(t *testing.T) {
	outside := filepath.Join(t.TempDir(), "protected")
	if err := os.WriteFile(outside, []byte("unchanged"), 0600); err != nil {
		t.Fatal(err)
	}
	workspace, err := OpenWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = workspace.Close() }()
	executor, err := NewExecExecutor(workspace, OpenCommandEnvironment(), tool.RenderExecOutput, testCommandOutputStore{})
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	accepted := make(chan bool, 1)
	go func() {
		connection, err := listener.Accept()
		if err == nil {
			_ = connection.Close()
		}
		accepted <- err == nil
	}()
	port := listener.Addr().(*net.TCPAddr).Port
	for _, command := range []string{"printf changed > " + strconv.Quote(outside), "/usr/bin/nc -z -w 1 127.0.0.1 " + strconv.Itoa(port)} {
		input, _ := tool.NewExecInput(command, "", 1000)
		call, _ := tool.NewExecReadyCall("call-denied", input)
		id, _ := tool.ParseInvocationID("018f1d8a-7b5c-7def-8abc-456789abcdef")
		invocation, _ := tool.NewExecInvocation(id, call)
		candidate, _ := NewExecPlanner().Prepare(invocation)
		result := executor.Execute(context.Background(), candidate, nil)
		exit, known := result.ExecMetadata().ExitCode()
		if result.Status() != tool.ResultSuccess || !known || exit == 0 {
			t.Fatalf("sandbox denial not a completed nonzero command: %s", result.Code())
		}
	}
	_ = listener.Close()
	if <-accepted {
		t.Fatal("sandbox allowed network connection")
	}
	content, err := os.ReadFile(outside)
	if err != nil || string(content) != "unchanged" {
		t.Fatal("sandbox changed outside file")
	}
}

type tracedDarwinLauncher struct {
	launcher processLauncher
	process  *tracedDarwinProcess
}

func (launcher *tracedDarwinLauncher) start(ctx context.Context, candidate tool.ExecCandidate) (process.Handle, error) {
	process, err := launcher.launcher.start(ctx, candidate)
	if err != nil {
		return nil, err
	}
	launcher.process = &tracedDarwinProcess{Handle: process}
	return launcher.process, nil
}

type tracedDarwinProcess struct {
	process.Handle
	signals, probes []error
	observation     error
	alive           []bool
}

func (process *tracedDarwinProcess) Terminate(force bool) error {
	err := process.Handle.Terminate(force)
	process.signals = append(process.signals, err)
	return err
}
func (process *tracedDarwinProcess) GroupAlive() (bool, error) {
	alive, err := process.Handle.GroupAlive()
	process.probes = append(process.probes, err)
	process.alive = append(process.alive, alive)
	return alive, err
}

func TestDarwinExecTimeoutAndCancellationCleanOriginalGroup(t *testing.T) {
	for _, cancelOnReady := range []bool{false, true} {
		t.Run(strconv.FormatBool(cancelOnReady), func(t *testing.T) {
			workspace, err := OpenWorkspace(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer workspace.Close()
			trace := &tracedDarwinLauncher{launcher: newPlatformProcessLauncher(workspace, OpenCommandEnvironment())}
			executor := &ExecExecutor{launcher: trace, renderer: tool.RenderExecOutput}
			input, _ := tool.NewExecInput("/bin/sleep 30 & printf ready; wait", "", 1000)
			call, _ := tool.NewExecReadyCall("call-group", input)
			id, _ := tool.ParseInvocationID("018f1d8a-7b5c-7def-8abc-456789abcdef")
			invocation, _ := tool.NewExecInvocation(id, call)
			candidate, _ := NewExecPlanner().Prepare(invocation)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			ready := false
			result := executor.Execute(ctx, candidate, func(progress tool.ExecProgress) {
				if strings.Contains(progress.Recent(), "ready") {
					ready = true
					if cancelOnReady {
						cancel()
					}
				}
			})
			want := "timed_out"
			if cancelOnReady {
				want = "cancelled"
			}
			if !ready || result.Code() != want || result.Status() == tool.ResultOutcomeUncertain {
				t.Fatalf("group cleanup = %s, ready=%v", result.Code(), ready)
			}
			process := trace.process
			if process == nil || len(process.signals) == 0 || len(process.alive) == 0 || process.alive[len(process.alive)-1] {
				t.Fatal("original group was not confirmed stopped before Wait")
			}
			if result.ExecMetadata().TimedOut() == cancelOnReady || result.ExecMetadata().Cancelled() != cancelOnReady {
				t.Fatal("timeout and cancellation metadata conflated")
			}
		})
	}
}
func (process *tracedDarwinProcess) ObserveExit() error {
	process.observation = process.Handle.ObserveExit()
	return process.observation
}
