#!/usr/bin/env python3
"""以真实 Darwin PTY、双 Provider 假 SSE 和事件握手验收文件引用。"""
import http.server
import json
import os
from pathlib import Path
import platform
import select
import signal
import struct
import subprocess
import tempfile
import threading
import time


class AckInbox:
    """按到达顺序保留尚未消费的回执，按键等待不能丢掉先到的异步事实。"""

    def __init__(self):
        self.messages = []
        self.pending = []

    def append(self, message):
        self.messages.append(message)
        self.pending.append((len(self.messages), message))

    def take(self, predicate, after=0):
        for index, (sequence, message) in enumerate(self.pending):
            if sequence > after and predicate(message):
                self.pending.pop(index)
                return message
        return None


class Fixture(http.server.BaseHTTPRequestHandler):
    def log_message(self, *_args):
        pass

    def do_POST(self):
        body = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
        with self.server.condition:
            self.server.requests.append(body)
            number = len(self.server.requests)
            self.server.condition.notify_all()
        if number == 1:
            self.server.release.wait(20)
        elif number == 3:
            self.server.stop_release.wait(10)
        self.send_response(200)
        self.send_header("Content-Type", "text/event-stream")
        self.end_headers()

        def event(value):
            self.wfile.write(("data: " + json.dumps(value) + "\n\n").encode())
            self.wfile.flush()

        text = f"File turn complete {number}"
        try:
            if self.server.family == "openai":
                event({"type": "response.created", "response": {"id": f"file-{number}"}})
                event({"type": "response.output_item.done", "item": {"type": "message", "id": f"message-{number}", "role": "assistant", "phase": "final", "content": [{"type": "output_text", "text": text}]}})
                event({"type": "response.completed", "response": {"id": f"file-{number}"}})
            else:
                event({"type": "message_start", "message": {"id": f"file-{number}", "model": "fixture-model"}})
                event({"type": "content_block_start", "index": 0, "content_block": {"type": "text", "text": ""}})
                event({"type": "content_block_delta", "index": 0, "delta": {"type": "text_delta", "text": text}})
                event({"type": "content_block_stop", "index": 0})
                event({"type": "message_delta", "delta": {"stop_reason": "end_turn"}})
                event({"type": "message_stop"})
        except (BrokenPipeError, ConnectionResetError):
            pass


def run_case(binary, root, directory, family, no_color):
    import fcntl
    import pty
    import termios

    workspace = directory / f"{family}-{no_color}"
    (workspace / "src").mkdir(parents=True)
    (workspace / "src/a b.go").write_bytes(b"first\r\nFILE_FROZEN_2\nthird\n")
    (workspace / "src/z.go").write_text("STOP_FILE\n")
    (workspace / "manual.txt").write_text("MANUAL_FROZEN\n")
    (workspace / ".gitignore").write_text("manual.txt\n")
    server = http.server.HTTPServer(("127.0.0.1", 0), Fixture)
    server.family, server.requests = family, []
    server.condition, server.release, server.stop_release = threading.Condition(), threading.Event(), threading.Event()
    owner = threading.Thread(target=server.serve_forever)
    owner.start()
    master, slave = pty.openpty()
    ack_read, ack_write = os.pipe()
    fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", 24, 80, 0, 0))
    before = termios.tcgetattr(slave)
    env = os.environ.copy()
    env.update(EASYCODE_FILE_TERMINAL_SMOKE="1", EASYCODE_FILE_TERMINAL_FAMILY=family,
               EASYCODE_FILE_TERMINAL_URL=f"http://127.0.0.1:{server.server_port}",
               EASYCODE_FILE_TERMINAL_WORKSPACE=str(workspace), EASYCODE_FILE_TERMINAL_ACK_FD=str(ack_write), TERM="xterm-256color")
    if no_color:
        env["NO_COLOR"] = "1"
    else:
        env.pop("NO_COLOR", None)
    process = subprocess.Popen([str(binary), "-test.run=^TestFileMentionsTerminalHelper$", "-test.timeout=60s"], cwd=root / "internal/app", stdin=slave, stdout=slave, stderr=slave, env=env, pass_fds=(ack_write,))
    os.close(ack_write)
    buffer, output, inbox = bytearray(), bytearray(), AckInbox()
    messages = inbox.messages

    def await_ack(predicate, after=0):
        deadline = time.monotonic() + 12
        while True:
            matched = inbox.take(predicate, after)
            if matched is not None:
                return matched
            while b"\n" in buffer:
                raw, _, remaining = buffer.partition(b"\n")
                buffer[:] = remaining
                message = json.loads(raw)
                inbox.append(message)
                if message["kind"] == "action_failed":
                    raise RuntimeError(f"host action failed: {message}")
                matched = inbox.take(predicate, after)
                if matched is not None:
                    return matched
            remaining = deadline - time.monotonic()
            if remaining <= 0:
                raise RuntimeError(f"missing acknowledgement: {messages[-2:]} terminal={output[-1200:]!r}")
            ready, _, _ = select.select([master, ack_read], [], [], remaining)
            for fd in ready:
                data = os.read(fd, 65536)
                if not data:
                    raise RuntimeError(f"PTY ended early: {output[-1200:]!r}")
                (buffer if fd == ack_read else output).extend(data)

    def await_view(fragment, second=None):
        predicate = lambda m: fragment in m["view"] and (second is None or second in m["view"])
        if messages and predicate(messages[-1]):
            return messages[-1]
        return await_ack(predicate, after=len(messages))

    def key(data, expected=None, contains=None):
        after = len(messages)
        os.write(master, data)
        return await_ack(lambda m: m["kind"] == "key" and (expected is None or m.get("key") == expected) and (contains is None or contains in m["view"]), after=after)

    def await_requests(count):
        with server.condition:
            assert server.condition.wait_for(lambda: len(server.requests) >= count, timeout=10), "provider request did not arrive"
            return server.requests[count - 1]

    def resize(width, height):
        after = len(messages)
        fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", height, width, 0, 0))
        process.send_signal(signal.SIGWINCH)
        return await_ack(lambda m: m["kind"] == "size" and m["Width"] == width and m["Height"] == height, after=after)

    try:
        ready = await_ack(lambda m: m["kind"] == "ready")
        journal = Path(ready["journal"])
        initial_bytes = journal.read_bytes()
        key("\x1b[200~请看 @sr\x1b[201~".encode(), contains="@sr|")
        key(b"\x1b[200~#L2-3 tail\x1b[201~", contains="tail|")
        key(b"\x01", "ctrl+a")
        for _ in range(6):
            key(b"\x1b[C", "right")
        assert messages[-1]["Scans"] == 0, "pasted target started discovery before explicit reopening"
        key(b"\t", "tab")
        await_ack(lambda m: m["kind"] == "scan_blocked")
        await_view("@src/a b.go [file]")
        pending = key(b"x", "x", "@srx|#L2-3")
        assert "@src/a b.go [file]" not in pending["view"]
        stale = key(b"\t", "tab")
        assert "@srx|#L2-3" in stale["view"] and stale["Reads"] == 0 and not server.requests
        key(b"\x7f", "backspace")
        key(b"\x1b[15~", "f5")
        await_view("Tab/Enter: fill")
        filled = key(b"\r", "enter", '@"src/a b.go"#L2-3 |tail')
        assert filled["Reads"] == 0 and journal.read_bytes() == initial_bytes and not server.requests
        key(b"\r", "enter")
        blocked = await_ack(lambda m: m["kind"] == "read_blocked")
        assert blocked["Reads"] == 1 and journal.read_bytes() == initial_bytes and not server.requests
        (workspace / "src/a b.go").write_text("CHANGED_AFTER_PREPARATION\n")
        key(b"\x1b[17~", "f6")
        await_ack(lambda m: m["kind"] == "effort" and m.get("selected") == "low")
        assert journal.read_bytes() == initial_bytes
        key(b"\x1b[18~", "f7")
        await_ack(lambda m: m["kind"] == "saved")
        first = await_requests(1)
        serialized = json.dumps(first)
        assert "FILE_FROZEN_2" in serialized and "CHANGED_AFTER_PREPARATION" not in serialized
        effort = first.get("reasoning", {}).get("effort") if family == "openai" else first.get("output_config", {}).get("effort")
        assert effort == "low"
        key(b"\x0f", "ctrl+o")
        await_view("> user:")
        key(b"\r", "enter")
        await_view("Source lines: 3; included: 2-3", "FILE_FROZEN_2")
        key(b"\x0f", "ctrl+o")
        key(b"manual @manual.txt", contains="> manual @manual.txt|")
        await_view("No matching files")
        queued_after = len(messages)
        key(b"\r", "enter")
        queued = await_ack(lambda m: m["kind"] == "input_accepted", after=queued_after)
        assert queued["Reads"] == 2 and len(server.requests) == 1, (queued, len(server.requests))
        retracted_after = len(messages)
        key(b"\x1b", "esc")
        restored = await_ack(lambda m: m["kind"] == "retracted", after=retracted_after)
        assert "> manual @manual.txt|" in restored["view"] and "MANUAL_FROZEN" not in restored["view"]
        (workspace / "manual.txt").write_text("MANUAL_RESUBMIT\n")
        resubmitted_after = len(messages)
        key(b"\r", "enter")
        await_ack(lambda m: m["kind"] == "input_accepted" and m["Reads"] == 3, after=resubmitted_after)
        (workspace / "manual.txt").write_text("MANUAL_CHANGED_AFTER_QUEUE\n")
        key(b"\x1b[19~", "f8")
        await_ack(lambda m: m["kind"] == "effort" and m.get("selected") == "high" and m.get("captured") == "low" and m.get("pending"))
        for width, height in ((28, 10), (20, 5), (1, 1), (80, 24)):
            resized = resize(width, height)
            assert resized["Reads"] == 3
        server.release.set()
        await_ack(lambda m: m["kind"] == "terminal" and m["event"] == "turn_completed")
        await_ack(lambda m: m["kind"] == "terminal" and m["event"] == "turn_completed")
        second = await_requests(2)
        serialized = json.dumps(second)
        assert "MANUAL_RESUBMIT" in serialized and "MANUAL_CHANGED_AFTER_QUEUE" not in serialized and "MANUAL_FROZEN" not in serialized
        effort = second.get("reasoning", {}).get("effort") if family == "openai" else second.get("output_config", {}).get("effort")
        assert effort == "high"
        (workspace / "src/new.go").write_text("new file\n")
        key(b"check @new", contains="> check @new|")
        await_view("@src/new.go [file]")
        key(b"\x1b", "esc")
        (workspace / ".gitignore").write_text("manual.txt\nsrc/new.go\n")
        key(b"\t", "tab")
        fresh = await_view("No matching files")
        assert "@src/new.go [file]" not in fresh["view"]
        key(b"\x05", "ctrl+e")
        for _ in range(10):
            key(b"\x7f", "backspace")
        key(b"stop @src/z.go", contains="> stop @src/z.go|")
        await_view("Tab/Enter: fill")
        key(b"\r", "enter", "> stop @src/z.go |")
        key(b"\r", "enter")
        await_requests(3)
        key(b"\x03", "ctrl+c")
        server.stop_release.set()
        terminal = await_ack(lambda m: m["kind"] == "terminal")
        assert terminal["event"] == "turn_failed" and terminal["Reads"] == 4 and len(server.requests) == 3
        if no_color:
            assert all("\x1b[38;" not in m["view"] for m in messages)
        key(b"\x1b[21~", "f10")
        process.wait(timeout=10)
        while select.select([master], [], [], 0)[0]:
            try:
                chunk = os.read(master, 65536)
            except OSError:
                break
            if not chunk:
                break
            output.extend(chunk)
        assert process.returncode == 0 and b"FILE_MENTIONS_EXIT=0" in output, output[-1600:]
        after = termios.tcgetattr(slave)
        pending_flag = getattr(termios, "PENDIN", 0)
        before[3] &= ~pending_flag
        after[3] &= ~pending_flag
        assert after == before, "terminal settings were not restored"
        return {"provider": family, "no_color": no_color, "requests": 3, "content_reads": 4, "scans": terminal["Scans"], "terminal_restored": True}
    finally:
        server.release.set()
        server.stop_release.set()
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
        raise SystemExit("file mention PTY acceptance requires Darwin")
    root = Path(__file__).resolve().parents[1]
    with tempfile.TemporaryDirectory(prefix="easycode-file-mentions-smoke-") as temporary:
        directory = Path(temporary)
        binary = directory / "app.test"
        subprocess.run(["go", "test", "-c", "-o", str(binary), "./internal/app"], cwd=root, check=True)
        results = [run_case(binary, root, directory, family, no_color) for family in ("openai", "anthropic") for no_color in (False, True)]
        print(json.dumps({"platform": platform.platform(), "cases": results}, indent=2))
