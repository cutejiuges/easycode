#!/usr/bin/env python3
"""本地假 SSE 服务与真实 PTY 的问卷、取消、窗口和终端恢复验收。"""
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
            self.wfile.write(("data: " + json.dumps(value, ensure_ascii=False) + "\n\n").encode())
            self.wfile.flush()
        if len(self.server.requests) == 1:
            arguments = json.dumps({"questions": [{"id": "storage", "header": "Storage", "question": "Choose storage?", "options": [{"label": "SQLite", "description": "Local file"}, {"label": "Remote", "description": "Shared service"}], "multi_select": False}]})
            item = {"type": "function_call", "id": "q-item", "call_id": "q-call", "name": "request_user_input", "arguments": arguments}
            event({"type": "response.created", "response": {"id": "q-response"}})
            event({"type": "response.output_item.added", "output_index": 0, "item": {k: v for k, v in item.items() if k != "arguments"}})
            event({"type": "response.function_call_arguments.done", "output_index": 0, "item_id": "q-item", "arguments": arguments})
            event({"type": "response.output_item.done", "output_index": 0, "item": item})
            event({"type": "response.completed", "response": {"id": "q-response"}})
        else:
            event({"type": "response.created", "response": {"id": "final"}})
            event({"type": "response.output_text.delta", "delta": "Question flow complete"})
            event({"type": "response.output_item.done", "item": {"type": "message", "id": "final-message", "role": "assistant", "content": [{"type": "output_text", "text": "Question flow complete"}]}})
            event({"type": "response.completed", "response": {"id": "final"}})


def run_case(binary, root, kind, no_color=False):
    server = http.server.HTTPServer(("127.0.0.1", 0), Fixture)
    server.requests = []
    owner = threading.Thread(target=server.serve_forever)
    owner.start()
    master, slave = pty.openpty()
    ack_read, ack_write = os.pipe()
    fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", 24, 80, 0, 0))
    before = termios.tcgetattr(slave)
    env = os.environ.copy()
    env.update(EASYCODE_QUESTION_TERMINAL_SMOKE="1", EASYCODE_QUESTION_FIXTURE_URL=f"http://127.0.0.1:{server.server_port}", TERM="xterm-256color", EASYCODE_QUESTION_ACK_FD=str(ack_write))
    env["EASYCODE_QUESTION_PICKER"] = "1" if kind == "picker_cancel" else "0"
    if no_color:
        env["NO_COLOR"] = "1"
    process = subprocess.Popen([str(binary), "-test.run=^TestQuestionTerminalHelper$", "-test.timeout=30s"], stdin=slave, stdout=slave, stderr=slave, cwd=root / "internal/app", env=env, pass_fds=(ack_write,))
    os.close(ack_write)
    output = bytearray()
    cursor = 0
    ack_buffer = bytearray()
    def wait_ack(text):
        deadline = time.monotonic() + 10
        while text.encode() not in ack_buffer:
            ready, _, _ = select.select([master, ack_read], [], [], max(0, deadline-time.monotonic()))
            if not ready:
                raise RuntimeError(f"missing acknowledgement {text}: {output[-1000:]!r}")
            for fd in ready:
                data = os.read(fd, 65536)
                if not data:
                    raise RuntimeError("terminal ended before acknowledgement")
                (ack_buffer if fd == ack_read else output).extend(data)
        end = ack_buffer.index(text.encode()) + len(text)
        del ack_buffer[:end]
    def wait_for(text):
        nonlocal cursor
        needle = text.encode()
        deadline = time.monotonic() + 10
        while needle not in output[cursor:]:
            if time.monotonic() > deadline:
                raise RuntimeError(f"missing {text!r}: {output[-1800:]!r}")
            ready, _, _ = select.select([master], [], [], 0.2)
            if ready:
                try:
                    output.extend(os.read(master, 65536))
                except OSError:
                    break
            if process.poll() is not None:
                break
        if needle not in output[cursor:]:
            raise RuntimeError(f"missing {text!r}: {output[-1800:]!r}")
        cursor = len(output)
    try:
        if kind == "picker_cancel":
            wait_for("fixture")
            os.write(master, b"\x03")
            expected = 130
        else:
            wait_for("EasyCode")
            os.write(master, b"ask\r")
            wait_for("Question 1/1")
            if kind == "stop":
                os.write(master, b"\x03")
                wait_ack("TURN_IDLE")
            else:
                if kind == "resize_other":
                    fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", 6, 12, 0, 0))
                    os.kill(process.pid, signal.SIGWINCH)
                    wait_ack("SIZE 12 6")
                    os.write(master, b"\x1b[B\x1b[B\r")
                    wait_for("Notes")
                    os.write(master, "\x1b[200~自定义\x1b[201~\r".encode())
                else:
                    os.write(master, b"\r")
                wait_for("Review")
                if kind == "resize_other":
                    fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", 24, 80, 0, 0))
                    os.kill(process.pid, signal.SIGWINCH)
                    wait_ack("SIZE 80 24")
                os.write(master, b"\r")
                wait_ack("TURN_IDLE")
                wait_for("Question flow complete")
                if len(server.requests) != 2 or "q-call" not in json.dumps(server.requests[1]):
                    raise RuntimeError("missing paired question continuation")
                text = json.dumps(server.requests[1], ensure_ascii=False)
                if ("自定义" if kind == "resize_other" else "SQLite") not in text:
                    raise RuntimeError("answer missing from continuation")
            os.write(master, b"\x03")
            expected = 0
        wait_for(f"QUESTION_EXIT={expected}")
        process.wait(timeout=5)
        if process.returncode != 0:
            raise RuntimeError(f"helper failed: {output[-1000:]!r}")
        after = termios.tcgetattr(slave)
        pending = getattr(termios, "PENDIN", 0)
        before[3] &= ~pending
        after[3] &= ~pending
        if before != after:
            raise RuntimeError("terminal modes not restored")
        return {"case": kind, "no_color": no_color, "exit_code": expected, "terminal_restored": True, "requests": len(server.requests)}
    finally:
        if process.poll() is None:
            process.kill()
            process.wait()
        os.close(ack_read)
        os.close(master)
        os.close(slave)
        server.shutdown()
        owner.join()
        server.server_close()


def main():
    root = Path(__file__).resolve().parent.parent
    with tempfile.TemporaryDirectory(prefix="easycode-question-smoke-") as directory:
        binary = Path(directory) / "app.test"
        subprocess.run(["go", "test", "-c", "-o", str(binary), "./internal/app"], cwd=root, check=True)
        results = [run_case(binary, root, kind, no_color) for kind, no_color in [("selection", False), ("resize_other", True), ("stop", True), ("picker_cancel", True)]]
        print(json.dumps({"platform": platform.platform(), "cases": results}, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
