#!/usr/bin/env python3
"""用真实 EasyCode CLI、Darwin PTY 和本地双 Provider 服务验收 Plan Mode。"""

import argparse
from contextlib import contextmanager
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


class Terminal:
    def __init__(self, args, workspace, env):
        self.master, self.slave = pty.openpty()
        self.buffer = bytearray()
        self.cursor = 0
        self.resize(88, 24)
        self.original = termios.tcgetattr(self.slave)
        self.process = subprocess.Popen(args, stdin=self.slave, stdout=self.slave, stderr=self.slave,
                                        cwd=workspace, env=env)

    def resize(self, width, height):
        fcntl.ioctl(self.slave, termios.TIOCSWINSZ, struct.pack("HHHH", height, width, 0, 0))
        if hasattr(self, "process"):
            os.kill(self.process.pid, signal.SIGWINCH)

    def send(self, keys):
        os.write(self.master, keys)

    def paste(self, text):
        self.send(b"\x1b[200~" + text.encode() + b"\x1b[201~")

    def submit(self, text):
        self.paste(text)
        self.send(b"\r")

    def set_mode(self, command, label):
        self.submit(command)
        for _ in range(3):
            if self.expect(label, "session_busy") == label:
                return
            # 终端答复可能早于 Runtime 的最终清理，原命令仍在编辑器中。
            self.send(b"\r")
        raise AssertionError(f"mode change stayed busy: {self.buffer[-1500:]!r}")

    def expect(self, *needles, timeout=10):
        targets = [value.encode() for value in needles]
        deadline = time.monotonic() + timeout
        while True:
            for target in targets:
                offset = self.buffer.find(target, self.cursor)
                if offset >= 0:
                    self.cursor = offset + len(target)
                    return target.decode()
            remaining = deadline - time.monotonic()
            if remaining <= 0 or not select.select([self.master], [], [], remaining)[0]:
                raise AssertionError(f"missing {needles!r}: {self.buffer[-2500:]!r}")
            data = os.read(self.master, 65536)
            if not data:
                raise AssertionError(f"process closed before {needles!r}: {self.buffer[-2500:]!r}")
            self.buffer.extend(data)

    def quit(self):
        # 可见回复可能先于 owner 完成终态处理，先等待真实空闲快照。
        deadline = time.monotonic() + 5
        while True:
            remaining = deadline - time.monotonic()
            assert remaining > 0, "CLI did not become idle"
            self.submit("/context")
            self.expect("Context · committed snapshot", timeout=remaining)
            activity = self.expect(" · idle", " · turn", timeout=remaining)
            self.send(b"\x1b")
            self.expect("Shift+Tab: switch mode", timeout=remaining)
            if activity == " · idle":
                break
        self.send(b"\x03")
        deadline = time.monotonic() + 5
        while self.process.poll() is None:
            assert time.monotonic() < deadline, self.buffer[-2000:]
            if select.select([self.master], [], [], 0.05)[0]:
                self.buffer.extend(os.read(self.master, 65536))
        assert self.process.returncode == 0, self.buffer[-2000:]
        # PENDIN 是终端内核的暂时重绘标志。
        after = termios.tcgetattr(self.slave)
        pending = getattr(termios, "PENDIN", 0)
        self.original[3] &= ~pending
        after[3] &= ~pending
        assert after == self.original, "CLI did not restore terminal modes"

    def close(self):
        if self.process.poll() is None:
            self.process.kill()
            self.process.wait(timeout=5)
        os.close(self.master)
        os.close(self.slave)


@contextmanager
def capture_terminal(args, workspace, env, log):
    terminal = Terminal(args, workspace, env)
    try:
        yield terminal
    finally:
        terminal.close()
        log.write_bytes(terminal.buffer)


def expected_names(family, plan):
    task, question = ("TodoWrite", "AskUserQuestion") if family == "anthropic" else ("update_plan", "request_user_input")
    names = ["ReadArtifact", "Glob", "Grep", "Read", "Skill", task, question]
    if not plan:
        patch, command = ("Edit", "Bash") if family == "anthropic" else ("apply_patch", "exec_command")
        names[3:3] = [patch]
        names[5:5] = ["Write", command]
    return names


class Fixture(http.server.BaseHTTPRequestHandler):
    def log_message(self, *_args):
        pass

    def do_POST(self):
        body = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
        self.server.requests.append(body)
        number = len(self.server.requests)
        family = self.server.family
        plan = 3 <= number <= 7
        try:
            assert [item["name"] for item in body["tools"]] == expected_names(family, plan), body["tools"]
            instruction = body.get("system" if family == "anthropic" else "instructions")
            assert bool(instruction) == plan
            if number > 1:
                # 普通模式中已配对的调用在 Plan 的原生输入中仍然存在。
                assert json.dumps(body).count('"call-1-0"') == 2, "prior call and result must both be present"
            if number == 5:
                assert "Go modules" in json.dumps(body)
        except AssertionError as error:
            self.server.errors.append(f"request {number}: {error}")
            self.send_error(422)
            return
        if number == 6:
            self.server.entered.set()
            self.server.release.wait(10)
        read = ("Read", {"file_path": "README.md"})
        write = ("Write", {"file_path": "prior.txt", "content": "prior ordinary change"})
        blocked = ("Write", {"file_path": "blocked.txt", "content": "must stay absent"})
        final = ("Write", {"file_path": "implemented.txt", "content": "implemented"})
        question = (
            "AskUserQuestion" if family == "anthropic" else "request_user_input",
            {"questions": [
                {"id": "package", "header": "Layout", "question": "Choose a layout", "options": [
                    {"label": "Go modules", "description": "One module"},
                    {"label": "Workspace", "description": "Several modules"},
                ], "multi_select": False},
            ]},
        )
        calls = {1: [write], 3: [read, blocked], 4: [read, question], 8: [final]}.get(number, [])
        replies = {
            2: "## Initial change complete", 3: "Partially streamed plan", 5: "## Plan draft",
            6: "Cancelled request output", 7: "## Revised plan", 9: "## Implementation complete",
        }
        self.send_response(200)
        self.send_header("Content-Type", "text/event-stream")
        self.end_headers()
        try:
            self.stream(family, number, calls, replies.get(number, ""))
        except (BrokenPipeError, ConnectionResetError):
            pass

    def stream(self, family, number, calls, reply):
        def event(kind, **fields):
            wire = json.dumps({"type": kind, **fields}, ensure_ascii=False)
            self.wfile.write(("data: " + wire + "\n\n").encode())
            self.wfile.flush()

        sample = f"sample-{number}"
        if family == "openai":
            event("response.created", response={"id": sample})
            if reply:
                event("response.output_text.delta", delta=reply)
                event("response.output_item.done", item={"id": "message-" + sample, "type": "message",
                      "role": "assistant", "phase": "final", "content": [{"type": "output_text", "text": reply}]})
        else:
            event("message_start", message={"id": sample, "model": "fixture-model"})
            if reply:
                event("content_block_start", index=0, content_block={"type": "text", "text": ""})
                event("content_block_delta", index=0, delta={"type": "text_delta", "text": reply})
                event("content_block_stop", index=0)
        for index, (name, arguments) in enumerate(calls):
            call_id = f"call-{number}-{index}"
            item_id = "item-" + call_id
            position = index + int(bool(reply))
            wire_args = json.dumps(arguments, ensure_ascii=False)
            if family == "openai":
                call = {"type": "function_call", "id": item_id, "call_id": call_id, "name": name}
                event("response.output_item.added", output_index=position, item=call)
                event("response.function_call_arguments.delta", item_id=item_id, output_index=position, delta=wire_args)
                event("response.function_call_arguments.done", item_id=item_id, output_index=position, arguments=wire_args)
                event("response.output_item.done", output_index=position, item=dict(call, arguments=wire_args))
            else:
                event("content_block_start", index=position, content_block={"type": "tool_use", "id": call_id, "name": name, "input": {}})
                event("content_block_delta", index=position, delta={"type": "input_json_delta", "partial_json": wire_args})
                event("content_block_stop", index=position)
        if family == "openai":
            event("response.completed", response={"id": sample})
        else:
            event("message_delta", delta={"stop_reason": "tool_use" if calls else "end_turn"})
            event("message_stop")


def run_case(binary, temporary, family, output):
    directory = temporary / family
    workspace, home = directory / "workspace", directory / "home"
    workspace.mkdir(parents=True)
    home.mkdir(mode=0o700)
    (workspace / "README.md").write_text("Fixture source\n")
    server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Fixture)
    server.family, server.requests, server.errors = family, [], []
    server.release = threading.Event()
    server.entered = threading.Event()
    config = directory / "config.json"
    config.write_text(json.dumps({"provider": family, "base_url": f"http://127.0.0.1:{server.server_port}",
                                  "model": "fixture-model", "api_key": "local-plan-fixture"}))
    config.chmod(0o600)
    env = {key: value for key, value in os.environ.items() if not key.startswith("EASYCODE_")}
    env.update(HOME=str(home), TERM="xterm-256color", NO_COLOR="1")
    flags = [str(binary), "--config", str(config)]
    owner = threading.Thread(target=server.serve_forever)
    owner.start()
    try:
        with capture_terminal(flags, workspace, env, output / f"{family}-live.pty") as terminal:
            terminal.expect("Default")
            terminal.submit("Normal setup")
            terminal.expect("Create file")
            terminal.send(b"\x1b[A\r")
            terminal.expect("● Initial change complete")
            assert (workspace / "prior.txt").read_text() == "prior ordinary change"
            terminal.set_mode("/plan", "Plan mode")
            assert len(server.requests) == 2
            failure_start = len(terminal.buffer)
            terminal.submit("Check the restricted catalog")
            terminal.expect("stream_protocol_error")
            failure = terminal.buffer[failure_start:terminal.cursor].lower()
            assert b"partially streamed plan" in failure and b"unfinished" in failure, failure
            assert not (workspace / "blocked.txt").exists()
            terminal.submit("Read, ask, then draft")
            terminal.expect("Question 1/1")
            terminal.send(b"\r")
            terminal.expect("Review")
            terminal.send(b"\x1b[Z")
            terminal.expect("Question 1/1")
            terminal.send(b"\r")
            terminal.expect("Review")
            terminal.send(b"\r")
            terminal.expect("● Plan draft")
            terminal.quit()

        path, = (home / ".easycode" / "sessions").rglob("*.jsonl")
        before = path.read_bytes()
        thread = json.loads(before.splitlines()[0])["thread_id"]
        resumed = flags + ["--resume", thread]
        with capture_terminal(resumed, workspace, env, output / f"{family}-resumed.pty") as terminal:
            terminal.expect("Default")
            assert len(server.requests) == 5 and before == path.read_bytes()
            terminal.paste("Reopened draft")
            terminal.send(b"\x1b[D" * 5)
            terminal.send(b"\x1b[Z")
            terminal.expect("Plan mode")
            terminal.resize(32, 8)
            terminal.expect("Plan")
            terminal.resize(88, 24)
            terminal.send(b"\r")
            assert server.entered.wait(5), server.errors
            terminal.submit("/plan off")
            terminal.expect("session_busy")
            terminal.send(b"\x7f" * len("/plan off"))
            terminal.send(b"\x03")
            terminal.expect("user_cancelled")
            server.release.set()
            terminal.submit("Revise the plan after cancellation")
            terminal.expect("● Revised plan")
            terminal.set_mode("/plan off", "Default")
            assert len(server.requests) == 7
            terminal.submit("Implement with an explicit approval")
            terminal.expect("Create file")
            terminal.send(b"\x1b[A\x1b[A\r")
            terminal.expect("● Implementation complete")
            terminal.quit()
        assert (workspace / "implemented.txt").read_text() == "implemented"
        assert not server.errors, server.errors
        assert len(server.requests) == 9
        assert "Reopened draft" in json.dumps(server.requests[5])
        assert b"work_mode_snapshot" not in path.read_bytes()
        print(f"{family}: verified 9 requests, refused Plan writes, restored Default and approved implementation", flush=True)
    finally:
        server.release.set()
        server.shutdown()
        server.server_close()
        owner.join()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, default=Path(".test-results/plan-mode"))
    args = parser.parse_args()
    if platform.system() != "Darwin":
        parser.error("this smoke test requires Darwin")
    args.output.mkdir(exist_ok=True, parents=True)
    with tempfile.TemporaryDirectory(prefix="easycode-plan-pty-") as name:
        temporary = Path(name).resolve()
        binary = temporary / "easycode"
        repository = Path(__file__).resolve().parent.parent
        subprocess.run(["go", "build", "-o", binary, "./cmd/easycode"], check=True, cwd=repository)
        for family in ("openai", "anthropic"):
            run_case(binary, temporary, family, args.output)


if __name__ == "__main__":
    main()
