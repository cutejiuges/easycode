#!/usr/bin/env python3
"""使用本地双 Provider 和真实 PTY 验证推理阅读、提交交接及终端恢复。"""
import fcntl
import json
import os
from pathlib import Path
import platform
import pty
import select
import signal
import struct
import subprocess
import tempfile
import termios
import time


def run_case(binary, root, family, no_color, cancel):
    master, slave = pty.openpty()
    read_fd, write_fd = os.pipe()
    fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", 24, 80, 0, 0))
    before = termios.tcgetattr(slave)
    env = os.environ.copy()
    env.update(EASYCODE_REASONING_TERMINAL_SMOKE="1", EASYCODE_REASONING_FAMILY=family,
               EASYCODE_REASONING_ACK_FD=str(write_fd), TERM="xterm-256color")
    if no_color:
        env["NO_COLOR"] = "1"
    else:
        env.pop("NO_COLOR", None)
    process = subprocess.Popen([str(binary), "-test.run=^TestReasoningTerminalHelper$", "-test.timeout=35s"],
                               stdin=slave, stdout=slave, stderr=slave, pass_fds=(write_fd,), cwd=root, env=env)
    os.close(write_fd)
    pending = bytearray()
    output = bytearray()
    messages = []

    def receive(prefix):
        deadline = time.monotonic() + 10
        while time.monotonic() < deadline:
            for i, line in enumerate(messages):
                if line.startswith(prefix):
                    return messages.pop(i).decode()
            ready, _, _ = select.select([read_fd, master], [], [], max(0, deadline-time.monotonic()))
            for fd in ready:
                data = os.read(fd, 65536)
                if not data:
                    raise RuntimeError(f"ended before {prefix!r}: {output[-2000:]!r}")
                if fd == master:
                    output.extend(data)
                else:
                    pending.extend(data)
                    while b"\n" in pending:
                        line, _, rest = pending.partition(b"\n")
                        pending[:] = rest
                        messages.append(bytes(line))
        raise RuntimeError(f"missing {prefix!r}: {messages!r}: {output[-1000:]!r}")

    def send(data):
        os.write(master, data)
        return receive(b"ACK ")

    try:
        receive(b"READY")
        send(b"work")
        send(b"\r")
        receive(b"LIVE")
        ack = send(b"\x0f")
        receive(b"LIST detail")
        ack = send(b"\r")
        if "body=true" not in ack:
            raise RuntimeError(f"live body missing: {ack}")
        if "raw=true" not in send(b"r"):
            raise RuntimeError("raw toggle failed")
        send(b"r")
        for width, height in [(34, 12), (80, 24)]:
            fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", height, width, 0, 0))
            os.kill(process.pid, signal.SIGWINCH)
            receive(f"SIZE {width} {height}".encode())
        send(b"\x1b[6~")
        send(b"\x1b[5~")
        if cancel:
            if "cancelling=true" not in send(b"\x03"):
                raise RuntimeError("active reader did not prioritize cancellation")
            receive(b"FAILED")
            send(b"\x0f")
        else:
            send(b"\x1b[15~")
            commit = receive(b"COMMIT ").split()
            read = receive(b"READ ").split()
            if commit[1:] != read[1:3]:
                raise RuntimeError(f"cursor handoff drifted: {commit} / {read}")
            receive(b"COMPLETE")
            ack = send(b"\x0f")
            if "thinking=true" in ack:
                raise RuntimeError("committed reasoning left active status")
            if "uncertain=true" not in send(b"\x1b[19~"):
                raise RuntimeError("uncertain save state missing")
        os.write(master, b"\x1b[21~")
        receive(b"CLOSED")
        process.wait(timeout=5)
        if process.returncode:
            raise RuntimeError(f"helper failed: {output[-2000:]!r}")
        after = termios.tcgetattr(slave)
        flag = getattr(termios, "PENDIN", 0)
        before[3] &= ~flag
        after[3] &= ~flag
        if before != after:
            raise RuntimeError("terminal attributes not restored")
        for private in [b"opaque-reasoning-sentinel", b"raw-reasoning-sentinel", b"ignored-start-sentinel"]:
            if private in output:
                raise RuntimeError("opaque data reached terminal")
        return {"provider": family, "no_color": no_color, "cancel": cancel,
                "cursor_handoff": not cancel, "terminal_restored": True, "passed": True}
    finally:
        if process.poll() is None:
            process.kill()
            process.wait()
        os.close(read_fd)
        os.close(master)
        os.close(slave)


def main():
    if platform.system() != "Darwin":
        raise SystemExit("reasoning PTY helper requires Darwin")
    root = Path(__file__).resolve().parent.parent
    with tempfile.TemporaryDirectory(prefix="easycode-reasoning-smoke-") as temporary:
        binary = Path(temporary) / "app.test"
        subprocess.run(["go", "test", "-c", "-o", str(binary), "./internal/app"], cwd=root, check=True)
        cases = [run_case(binary, root, family, no_color, cancel)
                 for family in ["openai", "anthropic"] for no_color in [False, True] for cancel in [False, True]]
        print(json.dumps({"platform": platform.platform(), "cases": cases}, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
