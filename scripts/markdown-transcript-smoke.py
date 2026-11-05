#!/usr/bin/env python3
"""通过真实 PTY 与独立受理回执测量 Markdown 冷扫描期间的交互响应。"""
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


def run_case(binary, root, no_color):
    master, slave = pty.openpty()
    read_fd, write_fd = os.pipe()
    fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", 24, 80, 0, 0))
    before = termios.tcgetattr(slave)
    env = os.environ.copy()
    env.update(EASYCODE_MARKDOWN_TERMINAL_SMOKE="1", EASYCODE_MARKDOWN_ACK_FD=str(write_fd), TERM="xterm-256color")
    if no_color:
        env["NO_COLOR"] = "1"
    else:
        env.pop("NO_COLOR", None)
    process = subprocess.Popen([str(binary), "-test.run=^TestMarkdownTerminalHelper$", "-test.timeout=30s"], stdin=slave, stdout=slave, stderr=slave, pass_fds=(write_fd,), cwd=root, env=env)
    os.close(write_fd)
    pending = bytearray()
    output = bytearray()
    samples = []
    def receive(prefix):
        deadline = time.monotonic() + 10
        while time.monotonic() < deadline:
            while b"\n" in pending:
                line, _, rest = pending.partition(b"\n")
                pending[:] = rest
                if line.startswith(prefix):
                    return line.decode()
            ready, _, _ = select.select([read_fd, master], [], [], max(0, deadline-time.monotonic()))
            for fd in ready:
                data = os.read(fd, 65536)
                if not data:
                    raise RuntimeError(f"terminal ended before {prefix!r}: {output[-1000:]!r}")
                (pending if fd == read_fd else output).extend(data)
        raise RuntimeError(f"missing acknowledgement {prefix!r}")
    def send(data, kind):
        start = time.perf_counter_ns()
        os.write(master, data)
        ack = receive(b"ACK ")
        samples.append({"kind": kind, "ms": (time.perf_counter_ns()-start)/1e6, "ack": ack})
        return ack
    def queued(first, second, kind):
        start = time.perf_counter_ns()
        os.write(master, first + second)
        receive(b"ACK ")
        ack = receive(b"ACK ")
        samples.append({"kind": kind, "ms": (time.perf_counter_ns()-start)/1e6, "ack": ack})
        return ack
    try:
        receive(b"READY")
        # 以第一条受理回执建立启动屏障，随后测量不依赖 sleep。
        send(b"a", "startup_input")
        for _ in range(20):
            send(b"a", "input")
            send(b"\x7f", "backspace")
        send(b"\x1b[5~", "page_up")
        send(b"\x1b[6~", "page_down")
        send(b"\x1b[15~", "open_code")
        if "cancelling=true" not in send(b"\x03", "stop_acceptance"):
            raise RuntimeError("stop was not accepted")
        send(b"\x1b[19~", "reset")
        for _ in range(5):
            send(b"\x1b[15~", "open_code")
            send(b"\x1b[17~", "close_code_cold_scan")
        send(b"\x1b[15~", "open_code")
        queued(b"\x1b[17~", b"a", "input_queued_at_cold_close")
        if "approval=true" not in queued(b"\x1bOR", b"\x1b[18~", "approval_queued_at_budget_fallback"):
            raise RuntimeError("queued approval was not accepted")
        send(b"\x1b[19~", "approval_close")
        if "cancelling=true" not in queued(b"\x1bOR", b"\x03", "stop_queued_at_budget_fallback"):
            raise RuntimeError("queued stop was not accepted")
        send(b"\x1b[19~", "reset")
        send(b"\x1b[18~", "approval_open")
        ack = send(b"r", "approval_focus")
        if "approval=true" not in ack:
            raise RuntimeError("approval lost focus")
        send(b"\x1b[19~", "approval_close")
        for width, height in [(34, 12), (12, 6), (80, 24)]:
            fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", height, width, 0, 0))
            os.kill(process.pid, signal.SIGWINCH)
            receive(f"SIZE {width} {height}".encode())
            send(b"a", "input_after_resize")
        send(b"\x0f", "history_open")
        receive(b"HISTORY_READY")
        send(b"\r", "detail_open")
        receive(b"DETAIL_READY")
        if "raw=true" not in send(b"r", "raw_toggle"):
            raise RuntimeError("raw toggle failed")
        if "raw=false" not in send(b"r", "markdown_toggle"):
            raise RuntimeError("Markdown toggle failed")
        send(b"\x0f", "history_close")
        send(b"\x1b[20~", "commit_blocked_projection")
        if "handoff=true" not in send(b"a", "input_during_handoff"):
            raise RuntimeError("projection did not retain handoff")
        send(b"\x1bOS", "release_projection")
        receive(b"PROJECTION_READY")
        os.write(master, b"\x1b[21~")
        receive(b"CLOSED")
        process.wait(timeout=5)
        if process.returncode:
            raise RuntimeError(f"helper failed: {output[-1000:]!r}")
        after = termios.tcgetattr(slave)
        pending_flag = getattr(termios, "PENDIN", 0)
        before[3] &= ~pending_flag
        after[3] &= ~pending_flag
        if before != after:
            raise RuntimeError("terminal attributes were not restored")
        times = sorted(item["ms"] for item in samples if item["kind"] != "startup_input")
        p95 = times[max(0, (len(times)*95+99)//100-1)]
        return {"no_color": no_color, "p95_ms": p95, "max_ms": max(times), "passed": p95 <= 100 and max(times) <= 250, "terminal_restored": True, "samples": samples}
    finally:
        if process.poll() is None:
            process.kill()
            process.wait()
        os.close(read_fd)
        os.close(master)
        os.close(slave)


def main():
    root = Path(__file__).resolve().parent.parent
    with tempfile.TemporaryDirectory(prefix="easycode-markdown-smoke-") as temporary:
        binary = Path(temporary)/"tui.test"
        subprocess.run(["go", "test", "-c", "-o", str(binary), "./internal/tui"], cwd=root, check=True)
        results = [run_case(binary, root, False), run_case(binary, root, True)]
        print(json.dumps({"platform": platform.platform(), "cpu": platform.processor(), "results": results}, ensure_ascii=False, indent=2))
        if not all(case["passed"] for case in results):
            raise SystemExit(1)


if __name__ == "__main__":
    main()
