//go:build darwin

package builtin

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"unsafe"

	"easycode/internal/tool"
	"golang.org/x/sys/unix"
)

type darwinProcessLauncher struct {
	workspace      *Workspace
	environment    CommandEnvironment
	registerExit   func(int, int) error
	sandboxProfile func(SandboxPlan) string
	beforeRelease  func(int, *os.File, *os.File) error
}

func newPlatformProcessLauncher(workspace *Workspace, environment CommandEnvironment) processLauncher {
	return &darwinProcessLauncher{workspace: workspace, environment: environment}
}

func commandSandboxProfile(plan SandboxPlan) string {
	var reads, writes strings.Builder
	for _, root := range plan.ReadRoots() {
		reads.WriteString(" (subpath " + strconv.Quote(root) + ")")
	}
	for _, root := range plan.WriteRoots() {
		writes.WriteString(" (subpath " + strconv.Quote(root) + ")")
	}
	return "(version 1)\n(deny default)\n(allow process-exec process-fork)\n(allow signal (target same-sandbox))\n(allow sysctl-read)\n" +
		"(allow file-read* (literal \"/\") (literal \"/private/var/select/sh\"))\n(allow file-read-metadata (path-ancestors " + strconv.Quote(plan.Workspace()) + "))\n" +
		"(allow file-read*" + reads.String() + ")\n" +
		"(allow file-write*" + writes.String() + " (literal \"/dev/null\"))\n"
}

func (launcher *darwinProcessLauncher) start(ctx context.Context, candidate tool.ExecCandidate) (processHandle, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	directory, err := launcher.workspace.handle.openDirectory(candidate.Invocation().Input().Workdir(), launcher.workspace.hooks)
	if err != nil {
		return nil, errors.New("command workdir unavailable")
	}
	defer func() { _ = directory.close() }()
	node, ok := directory.(*unixWorkspaceNode)
	if !ok {
		return nil, errSandboxUnavailable
	}
	rootHandle, ok := launcher.workspace.handle.(*unixWorkspaceHandle)
	if !ok {
		return nil, errSandboxUnavailable
	}
	// FcntlInt通过libSystem调用；固定缓冲区地址，避免整数参数跨调用时失去指针生命周期。
	pathBuffer := new([1024]byte)
	var pinned runtime.Pinner
	pinned.Pin(pathBuffer)
	defer pinned.Unpin()
	_, err = unix.FcntlInt(rootHandle.root.Fd(), unix.F_GETPATH, int(uintptr(unsafe.Pointer(pathBuffer))))
	if err != nil {
		return nil, errSandboxUnavailable
	}
	root := unix.ByteSliceToString(pathBuffer[:])
	plan, err := NewSandboxPlanner().Plan(candidate, root)
	if err != nil {
		return nil, err
	}
	if launcher.environment.executable == "" {
		return nil, errSandboxUnavailable
	}
	profile := commandSandboxProfile
	if launcher.sandboxProfile != nil {
		profile = launcher.sandboxProfile
	}
	command := exec.Command(launcher.environment.executable, "--internal-command-child", profile(plan), candidate.Invocation().Input().Command())
	readyRead, readyWrite, err := os.Pipe()
	if err != nil {
		return nil, errSandboxUnavailable
	}
	readyOwned := true
	defer func() {
		_ = readyWrite.Close()
		if readyOwned {
			_ = readyRead.Close()
		}
	}()
	gateRead, gateWrite, err := os.Pipe()
	if err != nil {
		return nil, errSandboxUnavailable
	}
	defer func() { _ = gateRead.Close(); _ = gateWrite.Close() }()
	queue, err := unix.Kqueue()
	if err != nil {
		return nil, errSandboxUnavailable
	}
	queueOwned := true
	defer func() {
		if queueOwned {
			_ = unix.Close(queue)
		}
	}()
	unix.CloseOnExec(queue)
	command.Env = append(append([]string(nil), launcher.environment.entries...), "HOME="+plan.Workspace(), "TMPDIR="+plan.TemporaryDirectory(), "GOCACHE="+plan.CacheDirectory())
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	outRead, outWrite, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	errRead, errWrite, err := os.Pipe()
	if err != nil {
		_ = outRead.Close()
		_ = outWrite.Close()
		return nil, err
	}
	// sandbox初始化诊断接到空设备，只有已启动的受限shell才把fd6接为用户stderr。
	// 因而启动失败的宿主路径不会进入瞬态progress或最终工具结果。
	command.Stdout = outWrite
	command.ExtraFiles = []*os.File{node.handle, gateRead, readyWrite, errWrite}
	err = command.Start()
	if err == nil && launcher.beforeRelease != nil {
		err = launcher.beforeRelease(command.Process.Pid, outWrite, errWrite)
	}
	_ = readyWrite.Close()
	_ = outWrite.Close()
	_ = errWrite.Close()
	if err != nil {
		if command.Process != nil {
			_ = command.Process.Kill()
			_ = command.Wait()
		}
		_ = outRead.Close()
		_ = errRead.Close()
		return nil, errSandboxUnavailable
	}
	process := &darwinProcess{command: command, out: outRead, errout: errRead, group: command.Process.Pid, queue: queue, sandboxReady: readyRead}
	register := launcher.registerExit
	if register == nil {
		register = registerDarwinExit
	}
	err = register(queue, command.Process.Pid)
	if err == nil {
		_, err = gateWrite.Write([]byte{1})
	}
	if err != nil {
		// 启动屏障尚未放行，helper不能执行用户命令；回收前只操作仍被持有的进程身份。
		_ = command.Process.Kill()
		_ = command.Wait()
		_ = outRead.Close()
		_ = errRead.Close()
		return nil, errSandboxUnavailable
	}
	queueOwned = false
	readyOwned = false
	return process, nil
}

func registerDarwinExit(queue, pid int) error {
	registration := unix.Kevent_t{Ident: uint64(pid), Filter: unix.EVFILT_PROC, Flags: unix.EV_ADD | unix.EV_ONESHOT, Fflags: unix.NOTE_EXIT}
	_, err := unix.Kevent(queue, []unix.Kevent_t{registration}, nil, nil)
	return err
}

// RunCommandChild 只在新启动的辅助进程中绑定目录描述符并替换为sandbox进程。
// 该入口必须在任何应用资源或信号owner启动前调用。
func RunCommandChild(arguments []string) (bool, error) {
	if len(arguments) == 0 || arguments[0] != "--internal-command-child" {
		return false, nil
	}
	if len(arguments) != 3 {
		return true, errSandboxUnavailable
	}
	gate := os.NewFile(4, "command-start-gate")
	var permit [1]byte
	_, err := io.ReadFull(gate, permit[:])
	_ = gate.Close()
	if err != nil || permit[0] != 1 {
		return true, errSandboxUnavailable
	}
	var stat unix.Stat_t
	if err := unix.Fstat(3, &stat); err != nil || stat.Mode&unix.S_IFMT != unix.S_IFDIR {
		return true, errSandboxUnavailable
	}
	if err := unix.Fchdir(3); err != nil {
		return true, errSandboxUnavailable
	}
	_ = unix.Close(3)
	// 只有sandbox中的shell启动成功才确认准入；先关闭私有描述符再执行不可信命令。
	const trampoline = "exec 2>&6; exec 6>&-; printf '\\001' >&5 || exit 125; exec 5>&-; exec /bin/sh -c \"$1\""
	if err := syscall.Exec("/usr/bin/sandbox-exec", []string{"sandbox-exec", "-p", arguments[1], "/bin/sh", "-c", trampoline, "command-sandbox", arguments[2]}, os.Environ()); err != nil {
		return true, errSandboxUnavailable
	}
	return true, nil
}

type darwinProcess struct {
	command      *exec.Cmd
	out, errout  *os.File
	group        int
	queue        int
	reaped       bool
	sandboxReady *os.File
}

func (process *darwinProcess) stdout() io.ReadCloser { return process.out }
func (process *darwinProcess) stderr() io.ReadCloser { return process.errout }
func (process *darwinProcess) observeExit() error {
	var events [1]unix.Kevent_t
	for {
		count, err := unix.Kevent(process.queue, nil, events[:], nil)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			return errors.New("process exit observation failed")
		}
		if count == 1 && events[0].Ident == uint64(process.group) && events[0].Flags&unix.EV_ERROR == 0 && events[0].Fflags&unix.NOTE_EXIT != 0 {
			return nil
		}
		return errors.New("invalid process exit observation")
	}
}
func (process *darwinProcess) wait() (int, error) {
	if process.reaped {
		return 0, errors.New("process already reaped")
	}
	process.reaped = true
	defer func() { _ = unix.Close(process.queue) }()
	err := process.command.Wait()
	var ready [1]byte
	_, readyErr := io.ReadFull(process.sandboxReady, ready[:])
	_ = process.sandboxReady.Close()
	if readyErr != nil || ready[0] != 1 {
		return 0, errSandboxUnavailable
	}
	var exitErr *exec.ExitError
	if err == nil || errors.As(err, &exitErr) {
		// 信号退出使用shell约定的128+signal，保留已确定退出而非误报清理不确定。
		if status, ok := process.command.ProcessState.Sys().(syscall.WaitStatus); ok && status.Signaled() {
			return 128 + int(status.Signal()), nil
		}
		return process.command.ProcessState.ExitCode(), nil
	}
	return 0, errors.New("process wait failed")
}
func (process *darwinProcess) terminate(force bool) error {
	if process.reaped {
		return errors.New("process identity released")
	}
	signal := unix.SIGTERM
	if force {
		signal = unix.SIGKILL
	}
	err := unix.Kill(-process.group, signal)
	if errors.Is(err, unix.ESRCH) {
		return nil
	}
	return err
}
func (process *darwinProcess) groupAlive() (bool, error) {
	if process.reaped {
		return false, errors.New("process identity released")
	}
	members, err := unix.SysctlKinfoProcSlice("kern.proc.pgrp", process.group)
	if err != nil {
		return false, errors.New("process group probe failed")
	}
	// Darwin sys/proc.h中的SZOMB为5；僵尸不再执行，leader仍保留身份直到最终Wait。
	const zombieState = 5
	for _, member := range members {
		if member.Proc.P_stat != zombieState {
			return true, nil
		}
	}
	return false, nil
}
