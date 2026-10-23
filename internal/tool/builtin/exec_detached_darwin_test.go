//go:build darwin

package builtin

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"easycode/internal/tool"
	"golang.org/x/sys/unix"
)

// runDetachedCommandFixture 只处理测试入口；独立控制fd让测试始终能够回收并Wait夹具。
func runDetachedCommandFixture(arguments []string) (bool, int) {
	if len(arguments) == 0 || arguments[0] != "--test-detached-command" {
		return false, 0
	}
	if len(arguments) != 4 {
		return true, 120
	}
	if _, err := unix.Setsid(); err != nil {
		return true, 121
	}
	if arguments[1] == "closed" {
		_ = os.Stdout.Close()
		_ = os.Stderr.Close()
	}
	// setsid后的子进程仍继承同一Seatbelt策略，文件写入与网络均须失败。
	child := exec.Command("/bin/sh", "-c", `printf forbidden > "$1"`, "fixture", arguments[2])
	if child.Run() == nil {
		return true, 122
	}
	connection, err := net.DialTimeout("tcp", arguments[3], time.Second)
	if err == nil {
		_ = connection.Close()
		return true, 123
	}
	control, status := os.NewFile(3, "fixture-control"), os.NewFile(4, "fixture-status")
	defer control.Close()
	defer status.Close()
	if _, err := status.Write([]byte{1}); err != nil {
		return true, 124
	}
	var release [1]byte
	if _, err := control.Read(release[:]); err != io.EOF {
		return true, 125
	}
	if _, err := status.Write([]byte{2}); err != nil {
		return true, 126
	}
	return true, 0
}

func copyDetachedFixtureExecutable(t *testing.T, root string) string {
	t.Helper()
	path, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	source, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	destination := filepath.Join(root, "fixture-executable")
	target, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0700)
	if err != nil {
		t.Fatal(err)
	}
	_, copyErr := io.Copy(target, source)
	if err := target.Close(); err != nil {
		t.Fatal(err)
	}
	if copyErr != nil {
		t.Fatal(copyErr)
	}
	return destination
}

func TestDarwinDetachedWritersExposeLimitsAndRetainSandbox(t *testing.T) {
	for _, outputs := range []string{"retained", "closed"} {
		t.Run(outputs, func(t *testing.T) {
			workspaceRoot := t.TempDir()
			workspace, err := OpenWorkspace(workspaceRoot)
			if err != nil {
				t.Fatal(err)
			}
			defer workspace.Close()
			// 固定句柄路径以兼容macOS临时目录的/private别名。
			root, err := filepath.EvalSymlinks(workspaceRoot)
			if err != nil {
				t.Fatal(err)
			}
			binary := copyDetachedFixtureExecutable(t, root)
			outside := filepath.Join(t.TempDir(), "protected")
			if err := os.WriteFile(outside, []byte("unchanged"), 0600); err != nil {
				t.Fatal(err)
			}
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			controlRead, controlWrite, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			defer controlRead.Close()
			defer controlWrite.Close()
			statusRead, statusWrite, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			defer statusRead.Close()
			defer statusWrite.Close()
			plan := sandboxTestPlan(t, root)
			var helper *exec.Cmd
			defer func() {
				_ = controlWrite.Close()
				if helper != nil && helper.Process != nil && helper.ProcessState == nil {
					if err := helper.Wait(); err != nil {
						t.Errorf("fixture cleanup failed: %v", err)
					}
				}
			}()
			launcher := &darwinProcessLauncher{workspace: workspace, environment: OpenCommandEnvironment()}
			launcher.beforeRelease = func(group int, stdout, stderr *os.File) error {
				// 夹具由测试直接拥有，先加入真实命令组再主动setsid；产品owner不获得它的PID或控制fd。
				helper = exec.Command("/usr/bin/sandbox-exec", "-p", commandSandboxProfile(plan), binary, "--test-detached-command", outputs, outside, listener.Addr().String())
				helper.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pgid: group}
				helper.Env = []string{"PATH=/usr/bin:/bin"}
				helper.Stdout, helper.Stderr = stdout, stderr
				helper.ExtraFiles = []*os.File{controlRead, statusWrite}
				if err := helper.Start(); err != nil {
					return err
				}
				_ = controlRead.Close()
				_ = statusWrite.Close()
				var ready [1]byte
				if _, err := io.ReadFull(statusRead, ready[:]); err != nil || ready[0] != 1 {
					return errors.New("detached fixture failed sandbox checks")
				}
				return nil
			}
			executor := &ExecExecutor{launcher: launcher, renderer: tool.RenderExecOutput}
			candidate, _ := NewExecPlanner().Prepare(processTestCandidate(t).Invocation())
			result := executor.Execute(context.Background(), candidate, nil)
			if outputs == "retained" {
				if result.Status() != tool.ResultOutcomeUncertain || result.Code() != "output_cleanup_incomplete" {
					t.Fatalf("retained writer claimed cleanup: %s %s", result.Status(), result.Code())
				}
			} else if result.Status() != tool.ResultSuccess || result.Code() != "ok" {
				t.Fatalf("closed detached writer changed foreground contract: %s %s", result.Status(), result.Code())
			}
			// 产品返回后夹具仍等待独立控制通道；只有测试能够关闭它并执行唯一Wait。
			if err := controlWrite.Close(); err != nil {
				t.Fatal(err)
			}
			var acknowledged [1]byte
			if _, err := io.ReadFull(statusRead, acknowledged[:]); err != nil || acknowledged[0] != 2 {
				t.Fatalf("fixture did not survive until independent cleanup: %v", err)
			}
			if err := helper.Wait(); err != nil {
				t.Fatal(err)
			}
			content, err := os.ReadFile(outside)
			if err != nil || string(content) != "unchanged" {
				t.Fatal("detached process escaped sandbox")
			}
		})
	}
}

func TestDarwinLargeCommandOutputRetainsHeadTailAndBudgets(t *testing.T) {
	workspace, err := OpenWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer workspace.Close()
	executor, err := NewExecExecutor(workspace, OpenCommandEnvironment(), tool.RenderExecOutput)
	if err != nil {
		t.Fatal(err)
	}
	command := `printf stdout-head; /usr/bin/head -c 600000 /dev/zero | /usr/bin/tr '\000' x; printf stdout-tail; printf stderr-head >&2; /usr/bin/head -c 600000 /dev/zero | /usr/bin/tr '\000' y >&2; printf stderr-tail >&2`
	input, _ := tool.NewExecInput(command, "", 10000)
	call, _ := tool.NewExecReadyCall("call-large", input)
	id, _ := tool.ParseInvocationID("018f1d8a-7b5c-7def-8abc-456789abcdef")
	invocation, _ := tool.NewExecInvocation(id, call)
	candidate, _ := NewExecPlanner().Prepare(invocation)
	result := executor.Execute(context.Background(), candidate, nil)
	if result.Status() != tool.ResultSuccess || !result.ExecMetadata().OutputTruncated() || len(result.Preview().Text()) > tool.ExecModelPreviewBytes {
		t.Fatalf("large output failed: %s", result.Code())
	}
	for _, marker := range []string{"stdout-head", "stdout-tail", "stderr-head", "stderr-tail", "bytes omitted"} {
		if !strings.Contains(result.Preview().Text(), marker) {
			t.Fatalf("large output lost %s", marker)
		}
	}
	for _, stats := range []tool.ExecStreamStats{result.ExecMetadata().Stdout(), result.ExecMetadata().Stderr()} {
		if stats.ObservedBytes() != 600022 || stats.RetainedBytes() != tool.ExecCaptureBytesPerStream {
			t.Fatal("large output counts changed: " + strconv.FormatInt(stats.ObservedBytes(), 10))
		}
	}
}
