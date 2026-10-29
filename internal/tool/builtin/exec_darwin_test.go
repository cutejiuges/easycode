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

	"easycode/internal/tool"
	"golang.org/x/sys/unix"
)

func TestDarwinExecSandboxAndOutput(t *testing.T) {
	root := t.TempDir()
	workspace, err := OpenWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = workspace.Close() }()
	environment := filterCommandEnvironment([]string{"PATH=/usr/bin:/bin", "API_KEY=private-secret", "HTTPS_PROXY=private-proxy"})
	environment.executable, _ = os.Executable()
	executor, err := NewExecExecutor(workspace, environment, tool.RenderExecOutput, testCommandOutputStore{})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		command  string
		exit     int
		contains string
	}{
		{"printf hello; printf warning >&2; exit 2", 2, "warning"},
		{"test -z \"$API_KEY$HTTPS_PROXY\" && printf clean", 0, "clean"},
		{"printf inside > local-output; cat local-output", 0, "inside"},
		{"printf signalled; kill -TERM $$", 143, "signalled"},
	} {
		input, _ := tool.NewExecInput(test.command, "", 0)
		call, _ := tool.NewExecReadyCall("exec", input)
		id, _ := tool.ParseInvocationID("018f1d8a-7b5c-7def-8abc-456789abcdef")
		invocation, _ := tool.NewExecInvocation(id, call)
		candidate, _ := NewExecPlanner().Prepare(invocation)
		result := executor.Execute(context.Background(), candidate, nil)
		exit, known := result.ExecMetadata().ExitCode()
		if result.Status() != tool.ResultSuccess || !known || exit != test.exit || !strings.Contains(result.Preview().Text(), test.contains) {
			t.Fatalf("command failed: %s %s", result.Code(), result.Preview().Text())
		}
	}
	if _, err := os.Stat(filepath.Join(root, "local-output")); err != nil {
		t.Fatal(err)
	}
}

func TestDarwinCommandSandboxProfileGolden(t *testing.T) {
	want, err := os.ReadFile("testdata/command_sandbox.golden")
	if err != nil {
		t.Fatal(err)
	}
	plan := sandboxTestPlan(t, "/workspace/project")
	if got := commandSandboxProfile(plan); got != string(want) {
		t.Fatalf("sandbox profile changed:\n%s", got)
	}
	root := "/workspace/quote\"(allow network*)"
	profile := commandSandboxProfile(sandboxTestPlan(t, root))
	if strings.Count(profile, strconv.Quote(root)) != 3 {
		t.Fatal("workspace path not safely quoted in every rule")
	}
}

func TestMain(m *testing.M) {
	if handled, code := runDetachedCommandFixture(os.Args[1:]); handled {
		os.Exit(code)
	}
	if handled, err := RunCommandChild(os.Args[1:]); handled {
		if err != nil {
			os.Exit(125)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestDarwinExitObservationDoesNotReleaseIdentity(t *testing.T) {
	workspace, err := OpenWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = workspace.Close() }()
	environment := filterCommandEnvironment([]string{"PATH=/usr/bin:/bin"})
	environment.executable, err = os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	input, _ := tool.NewExecInput("exit 7", "", 1000)
	call, _ := tool.NewExecReadyCall("call-exit", input)
	id, _ := tool.ParseInvocationID("018f1d8a-7b5c-7def-8abc-456789abcdef")
	invocation, _ := tool.NewExecInvocation(id, call)
	candidate, _ := NewExecPlanner().Prepare(invocation)
	handle, err := newPlatformProcessLauncher(workspace, environment).start(context.Background(), candidate)
	if err != nil {
		t.Fatal(err)
	}
	process := handle.(*darwinProcess)
	defer func() {
		if !process.reaped {
			_ = process.terminate(true)
			_, _ = process.wait()
		}
		_ = process.stdout().Close()
		_ = process.stderr().Close()
	}()
	if err := process.observeExit(); err != nil {
		t.Fatal(err)
	}
	retained, err := unix.SysctlKinfoProc("kern.proc.pid", process.group)
	if err != nil || retained.Proc.P_pid != int32(process.group) {
		t.Fatal("exit observation released process identity")
	}
	alive, err := process.groupAlive()
	if err != nil || alive {
		t.Fatalf("exited leader treated as live member: %v", err)
	}
	exit, err := process.wait()
	if err != nil || exit != 7 {
		t.Fatalf("final Wait lost exit: %d %v", exit, err)
	}
	if err := process.terminate(true); err == nil {
		t.Fatal("signal after final Wait accepted")
	}
	if _, err := process.groupAlive(); err == nil {
		t.Fatal("group probe after final Wait accepted")
	}
	if _, err := process.wait(); err == nil {
		t.Fatal("duplicate Wait accepted")
	}
}

func TestDarwinObserverRegistrationFailureDoesNotRunCommand(t *testing.T) {
	root := t.TempDir()
	workspace, err := OpenWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = workspace.Close() }()
	environment := OpenCommandEnvironment()
	launcher := &darwinProcessLauncher{workspace: workspace, environment: environment, registerExit: func(int, int) error { return errors.New("injected observer failure") }}
	input, _ := tool.NewExecInput("printf forbidden > marker", "", 1000)
	call, _ := tool.NewExecReadyCall("call-gate", input)
	id, _ := tool.ParseInvocationID("018f1d8a-7b5c-7def-8abc-456789abcdef")
	invocation, _ := tool.NewExecInvocation(id, call)
	candidate, _ := NewExecPlanner().Prepare(invocation)
	handle, err := launcher.start(context.Background(), candidate)
	if handle != nil || !errors.Is(err, errSandboxUnavailable) {
		t.Fatal("observer failure did not fail closed")
	}
	if _, err := os.Stat(filepath.Join(root, "marker")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("command crossed failed startup gate")
	}
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

func TestDarwinExecCancellationAfterOutput(t *testing.T) {
	workspace, err := OpenWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = workspace.Close() }()
	executor, err := NewExecExecutor(workspace, OpenCommandEnvironment(), tool.RenderExecOutput, testCommandOutputStore{})
	if err != nil {
		t.Fatal(err)
	}
	trace := &tracedDarwinLauncher{launcher: executor.launcher}
	executor.launcher = trace
	input, _ := tool.NewExecInput("printf ready; exec /bin/sleep 30", "", 1000)
	call, _ := tool.NewExecReadyCall("call-cancel", input)
	id, _ := tool.ParseInvocationID("018f1d8a-7b5c-7def-8abc-456789abcdef")
	invocation, _ := tool.NewExecInvocation(id, call)
	candidate, _ := NewExecPlanner().Prepare(invocation)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sawReady := false
	result := executor.Execute(ctx, candidate, func(progress tool.ExecProgress) {
		if strings.Contains(progress.Recent(), "ready") {
			sawReady = true
			cancel()
		}
	})
	if !sawReady || result.Status() != tool.ResultCancelled || result.Code() != "cancelled" || !result.ExecMetadata().Cancelled() {
		t.Fatalf("cancellation cleanup failed: %s %s; signals=%v probes=%v observation=%v", result.Status(), result.Code(), trace.process.signals, trace.process.probes, trace.process.observation)
	}
}

func TestDarwinSandboxInitializationFailureIsNotACommandExit(t *testing.T) {
	root := t.TempDir()
	workspace, err := OpenWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	defer workspace.Close()
	launcher := &darwinProcessLauncher{workspace: workspace, environment: OpenCommandEnvironment(), sandboxProfile: func(SandboxPlan) string {
		return "(version 1) (invalid-private-host-path)"
	}}
	executor := &ExecExecutor{launcher: launcher, renderer: tool.RenderExecOutput}
	input, _ := tool.NewExecInput("printf forbidden > marker", "", 1000)
	call, _ := tool.NewExecReadyCall("call-init", input)
	id, _ := tool.ParseInvocationID("018f1d8a-7b5c-7def-8abc-456789abcdef")
	invocation, _ := tool.NewExecInvocation(id, call)
	candidate, _ := NewExecPlanner().Prepare(invocation)
	result := executor.Execute(context.Background(), candidate, func(progress tool.ExecProgress) {
		if progress.Recent() != "" || progress.StdoutBytes() != 0 || progress.StderrBytes() != 0 {
			t.Error("sandbox diagnostics entered command progress")
		}
	})
	_, known := result.ExecMetadata().ExitCode()
	if result.Status() != tool.ResultError || result.Code() != "sandbox_unavailable" || known {
		t.Fatalf("sandbox failure treated as command exit: %s %s", result.Status(), result.Code())
	}
	if result.Preview().Text() != "Exec failed: sandbox_unavailable" {
		t.Fatalf("sandbox diagnostics escaped: %q", result.Preview().Text())
	}
	if _, err := os.Stat(filepath.Join(root, "marker")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("user command ran despite invalid sandbox")
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

func (launcher *tracedDarwinLauncher) start(ctx context.Context, candidate tool.ExecCandidate) (processHandle, error) {
	process, err := launcher.launcher.start(ctx, candidate)
	if err != nil {
		return nil, err
	}
	launcher.process = &tracedDarwinProcess{processHandle: process}
	return launcher.process, nil
}

type tracedDarwinProcess struct {
	processHandle
	signals, probes []error
	observation     error
	alive           []bool
}

func (process *tracedDarwinProcess) terminate(force bool) error {
	err := process.processHandle.terminate(force)
	process.signals = append(process.signals, err)
	return err
}
func (process *tracedDarwinProcess) groupAlive() (bool, error) {
	alive, err := process.processHandle.groupAlive()
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
func (process *tracedDarwinProcess) observeExit() error {
	process.observation = process.processHandle.observeExit()
	return process.observation
}
