#!/usr/bin/env python3
"""用本地假 SSE 和真实 Darwin PTY 验收补全、审批隔离、缩放及终端恢复。"""
import fcntl
import http.server
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
import threading
import time


class Fixture(http.server.BaseHTTPRequestHandler):
    def log_message(self, *_args):
        pass

    def do_POST(self):
        body = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
        self.server.requests.append(body)
        self.send_response(200)
        self.send_header("Content-Type", "text/event-stream")
        self.end_headers()

        def event(value):
            self.wfile.write(("data: " + json.dumps(value) + "\n\n").encode())
            self.wfile.flush()

        if len(self.server.requests) == 1:
            arguments = json.dumps({"file_path": "marker", "content": "must-not-write"})
            item = {"type": "function_call", "id": "write-item", "call_id": "write-call", "name": "Write", "arguments": arguments}
            event({"type": "response.created", "response": {"id": "write-response"}})
            event({"type": "response.output_item.added", "output_index": 0, "item": {k: v for k, v in item.items() if k != "arguments"}})
            event({"type": "response.function_call_arguments.done", "output_index": 0, "item_id": "write-item", "arguments": arguments})
            event({"type": "response.output_item.done", "output_index": 0, "item": item})
            event({"type": "response.completed", "response": {"id": "write-response"}})
        else:
            event({"type": "response.created", "response": {"id": "final"}})
            event({"type": "response.output_text.delta", "delta": "Completion flow complete"})
            event({"type": "response.output_item.done", "item": {"type": "message", "id": "final-message", "role": "assistant", "content": [{"type": "output_text", "text": "Completion flow complete"}]}})
            event({"type": "response.completed", "response": {"id": "final"}})


def run_case(binary, root, no_color):
    server = http.server.HTTPServer(("127.0.0.1", 0), Fixture)
    server.requests = []
    owner = threading.Thread(target=server.serve_forever)
    owner.start()
    master, slave = pty.openpty()
    ack_read, ack_write = os.pipe()
    fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", 24, 80, 0, 0))
    before = termios.tcgetattr(slave)
    env = os.environ.copy()
    env.update(EASYCODE_COMPLETION_TERMINAL_SMOKE="1", EASYCODE_COMPLETION_FIXTURE_URL=f"http://127.0.0.1:{server.server_port}", EASYCODE_COMPLETION_ACK_FD=str(ack_write), TERM="xterm-256color")
    if no_color:
        env["NO_COLOR"] = "1"
    else:
        env.pop("NO_COLOR", None)
    process = subprocess.Popen([str(binary), "-test.run=^TestCompletionTerminalHelper$", "-test.timeout=30s"], cwd=root / "internal/app", stdin=slave, stdout=slave, stderr=slave, env=env, pass_fds=(ack_write,))
    os.close(ack_write)
    buffer = bytearray()
    output = bytearray()
    messages = []

    def await_ack(predicate):
        deadline = time.monotonic() + 10
        while True:
            while b"\n" in buffer:
                raw, _, remaining = buffer.partition(b"\n")
                buffer[:] = remaining
                message = json.loads(raw)
                messages.append(message)
                if predicate(message):
                    return message
            remaining = deadline - time.monotonic()
            if remaining <= 0:
                raise RuntimeError(f"missing PTY acknowledgement: {messages[-2:]} terminal={output[-600:]!r}")
            ready, _, _ = select.select([master, ack_read], [], [], remaining)
            if not ready:
                continue
            for fd in ready:
                data = os.read(fd, 65536)
                if not data:
                    raise RuntimeError(f"PTY ended early: {output[-1200:]!r}")
                (buffer if fd == ack_read else output).extend(data)

    def key(data, expected, contains=None):
        os.write(master, data)
        return await_ack(lambda m: m["kind"] == "key" and (expected is None or m["key"] == expected) and (contains is None or contains in m["view"]))

    try:
        await_ack(lambda m: m["kind"] == "ready")
        key(b"/con", None, "> /con|")
        dismissed = key(b"\x1b", "esc")
        assert "Up/Down: select" not in dismissed["view"]
        second = key(b"\x1b", "esc")
        assert "> /con|" in second["view"] and not server.requests
        key(b"\t", "tab", "Up/Down: select")
        key(b"\t", "tab", "> /context |")
        key(b"\r", "enter")
        await_ack(lambda m: m["kind"] == "control" and "Context" in m["view"])
        assert not server.requests
        key(b"\x1b", "esc")
        # 清空可能保留的本地命令草稿；每个退格都由确认消息同步。
        for _ in range(9):
            key(b"\x7f", "backspace")
        key(b"$rev", None, "> $rev|")
        # 空粘贴后仅移动光标，候选仍应可见。
        os.write(master, b"\x1b[200~\x1b[201~")
        key(b"\x1b[D", "left", "Up/Down: select")
        key(b"\x1b[C", "right", "> $rev|")
        fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", 24, 8, 0, 0))
        process.send_signal(signal.SIGWINCH)
        narrow = await_ack(lambda m: m["kind"] == "size" and m["width"] == 8)
        assert "Up/Down: select" not in narrow["view"]
        invisible = key(b"\t", "tab")
        assert "$rev|" in invisible["view"] and not server.requests
        fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", 10, 28, 0, 0))
        process.send_signal(signal.SIGWINCH)
        resized = await_ack(lambda m: m["kind"] == "size" and m["width"] == 28 and m["height"] == 10)
        assert "$rev|" in resized["view"]
        fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", 24, 80, 0, 0))
        process.send_signal(signal.SIGWINCH)
        await_ack(lambda m: m["kind"] == "size" and m["width"] == 80)
        # 在既有组合空白前补全，退格必须删掉整个分隔字素。
        os.write(master, "\x1b[200~ \u0301PTY args\x1b[201~".encode())
        await_ack(lambda m: m["kind"] == "key" and "PTY args|" in m["view"])
        key(b"\x01", "ctrl+a")
        for _ in range(4):
            key(b"\x1b[C", "right")
        key(b"\t", "tab", "Up/Down: select")
        key(b"\r", "enter", "$review \u0301|PTY args")
        assert not server.requests
        key(b"\x7f", "backspace", "$review|PTY args")
        key(b" ", " ", "$review |PTY args")
        key(b"\r", "enter")
        await_ack(lambda m: m["kind"] == "control" and "Create file" in m["view"])
        ignored = key(b"x", "x")
        assert "Create file" in ignored["view"]
        key(b"\t", "tab")
        key(b"Do not write", None, "Feedback (what should EasyCode do instead?): Do not write")
        key(b"\x1b", "esc")
        key(b"\r", "enter")
        await_ack(lambda m: m["kind"] == "idle")
        assert len(server.requests) == 2
        request = json.dumps(server.requests[0])
        assert "$review PTY args" in request and "PTY Skill PTY args" in request
        assert "/context" not in request and "Do not write" not in request
        last = key(b"\x03", "ctrl+c")
        deadline = time.monotonic() + 10
        while process.poll() is None:
            ready, _, _ = select.select([master], [], [], min(0.2, max(0, deadline - time.monotonic())))
            if ready:
                output.extend(os.read(master, 65536))
            if time.monotonic() > deadline and process.poll() is None:
                raise RuntimeError(f"PTY did not close: {last} terminal={output[-1800:]!r}")
        assert process.wait() == 0, output[-2000:]
        after = termios.tcgetattr(slave)
        # PENDIN 是内核待重输标记，按既有 PTY 验收规则比较其余完整属性。
        pending = getattr(termios, "PENDIN", 0)
        before[3] &= ~pending
        after[3] &= ~pending
        assert after == before, f"terminal settings were not restored: before={before} after={after}"
        return {"no_color": no_color, "requests": len(server.requests), "resizes": [[8, 24], [28, 10], [80, 24]], "terminal_restored": True, "approval_denied": True}
    finally:
        if process.poll() is None:
            process.kill()
        process.wait()
        server.shutdown()
        owner.join()
        server.server_close()
        for fd in (master, slave, ack_read):
            os.close(fd)


if __name__ == "__main__":
    if platform.system() != "Darwin":
        raise SystemExit("completion PTY acceptance requires Darwin")
    root = Path(__file__).resolve().parents[1]
    with tempfile.TemporaryDirectory(prefix="easycode-completion-smoke-") as directory:
        binary = Path(directory) / "app.test"
        subprocess.run(["go", "test", "-c", "-o", str(binary), "./internal/app"], cwd=root, check=True)
        print(json.dumps({"platform": platform.platform(), "cases": [run_case(binary, root, no_color) for no_color in (False, True)]}, indent=2))
