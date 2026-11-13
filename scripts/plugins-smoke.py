#!/usr/bin/env python3
"""用本地 SSE、真实 PTY 和事件握手验收插件浏览、调用、停止及终端恢复。"""
import fcntl
import http.server
import json
import os
from pathlib import Path
import platform
import pty
import select
import shutil
import signal
import struct
import subprocess
import tempfile
import termios
import threading
import time


def response_frames(family, response_id, text):
    """让 CLI 与终端样本共用完整的双 Provider 文本响应帧。"""
    if family == "openai":
        return [
            {"type": "response.created", "response": {"id": response_id}},
            {"type": "response.output_text.delta", "delta": text},
            {"type": "response.output_item.done", "item": {
                "type": "message", "id": response_id + "-answer", "role": "assistant",
                "content": [{"type": "output_text", "text": text}]}},
            {"type": "response.completed", "response": {"id": response_id}},
        ]
    if family == "anthropic":
        return [
            {"type": "message_start", "message": {"id": response_id, "model": "fixture-model"}},
            {"type": "content_block_start", "index": 0, "content_block": {"type": "text", "text": ""}},
            {"type": "content_block_delta", "index": 0, "delta": {"type": "text_delta", "text": text}},
            {"type": "content_block_stop", "index": 0},
            {"type": "message_stop"},
        ]
    raise ValueError("plugin fixture provider is invalid")


def skill_frames(family, response_id, call_id, arguments):
    """通过真实工具续写协议驱动主文档和同来源资源读取。"""
    encoded = json.dumps(arguments)
    started = response_frames(family, response_id, "")[0]
    if family == "openai":
        item_id = call_id + "-item"
        item = {"type": "function_call", "id": item_id, "call_id": call_id, "name": "Skill"}
        return [
            started,
            {"type": "response.output_item.added", "output_index": 0, "item": item},
            {"type": "response.function_call_arguments.delta", "item_id": item_id, "output_index": 0, "delta": encoded},
            {"type": "response.function_call_arguments.done", "item_id": item_id, "output_index": 0, "arguments": encoded},
            {"type": "response.output_item.done", "output_index": 0, "item": dict(item, arguments=encoded)},
            {"type": "response.completed", "response": {"id": response_id}},
        ]
    if family == "anthropic":
        return [
            started,
            {"type": "content_block_start", "index": 0, "content_block": {"type": "tool_use", "id": call_id, "name": "Skill", "input": {}}},
            {"type": "content_block_delta", "index": 0, "delta": {"type": "input_json_delta", "partial_json": encoded}},
            {"type": "content_block_stop", "index": 0},
            {"type": "message_stop"},
        ]
    raise ValueError("plugin fixture provider is invalid")


class Fixture(http.server.BaseHTTPRequestHandler):
    def log_message(self, *_args):
        pass

    def do_POST(self):
        body = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
        self.server.requests.append(body)
        number = len(self.server.requests)
        self.send_response(200)
        self.send_header("Content-Type", "text/event-stream")
        self.end_headers()

        def event(value):
            self.wfile.write(("data: " + json.dumps(value) + "\n\n").encode())
            self.wfile.flush()

        frames = response_frames(self.server.family, f"plugin-{number}", "Plugin flow complete")
        event(frames[0])
        if number == 2:
            self.server.second_entered.set()
            self.server.release_second.wait(15)
            return
        for frame in frames[1:]:
            event(frame)


class CLIFixture(http.server.BaseHTTPRequestHandler):
    def log_message(self, *_args):
        pass

    def do_POST(self):
        self.server.requests.append(json.loads(self.rfile.read(int(self.headers["Content-Length"]))))
        sample = len(self.server.requests) - self.server.case_start
        self.send_response(200)
        self.send_header("Content-Type", "text/event-stream")
        self.end_headers()

        def event(value):
            self.wfile.write(("data: " + json.dumps(value) + "\n\n").encode())

        response_id = f"cli-plugin-{sample}"
        if sample == 1:
            frames = skill_frames(self.server.family, response_id, "cli-main", {"skill": self.server.skill, "args": "model args"})
        elif sample == 2:
            frames = skill_frames(self.server.family, response_id, "cli-resource", {"skill": self.server.skill, "resource": "references/checklist.md"})
        else:
            frames = response_frames(self.server.family, response_id, "Plugin CLI complete")
        for frame in frames:
            event(frame)


def run_cli_cases(binary, root):
    results = []
    for family in ("openai", "anthropic"):
        server = http.server.HTTPServer(("127.0.0.1", 0), CLIFixture)
        server.family, server.requests = family, []
        owner = threading.Thread(target=server.serve_forever)
        owner.start()
        try:
            for kind, plugin_id in (("easycode", "easyqa"), ("claude", "claudeqa"),
                                    ("codex", "codexqa"), ("agentplugins", "agentqa")):
                with tempfile.TemporaryDirectory(prefix="easycode-plugin-cli-") as directory:
                    case_root = Path(directory).resolve()
                    home, workspace = case_root / "home", case_root / "workspace"
                    home.mkdir(mode=0o700)
                    workspace.mkdir(mode=0o700)
                    configuration = case_root / "config.json"
                    configuration.write_text(json.dumps({"provider": family, "model": "fixture-model",
                                                         "api_key": "fixture-key",
                                                         "base_url": f"http://127.0.0.1:{server.server_port}"}))
                    configuration.chmod(0o600)
                    env = os.environ.copy()
                    env.update(HOME=str(home), EASYCODE_PROVIDER=family, EASYCODE_API_KEY="fixture-key",
                               EASYCODE_MODEL="fixture-model", EASYCODE_BASE_URL=f"http://127.0.0.1:{server.server_port}")
                    package = case_root / "plugin"
                    shutil.copytree(root / "internal/extension/plugin/testdata" / kind, package)
                    before = len(server.requests)
                    server.case_start, server.skill = before, f"{plugin_id}:review"
                    result = subprocess.run([str(binary), "--config", str(configuration), "--print",
                                             "--plugin-dir", str(package), "--plugin-dir", str(case_root / "missing"),
                                             f"${plugin_id}:review CLI args"], cwd=workspace, env=env,
                                            stdin=subprocess.DEVNULL, capture_output=True, text=True, timeout=15)
                    assert result.returncode == 0 and result.stdout.strip() == "Plugin CLI complete", result.stderr
                    assert not result.stderr and len(server.requests) == before + 3
                    captured = server.requests[before:]
                    assert "Review CLI args" in json.dumps(captured[0])
                    main_result = json.dumps(captured[1])
                    if kind == "codex":
                        assert "skill_model_invocation_disabled" in main_result
                    else:
                        assert "Review model args" in main_result
                    assert "Check correctness" in json.dumps(captured[2])
                    journals = list((home / ".easycode/sessions").rglob("*.jsonl"))
                    assert len(journals) == 1
                    thread = journals[0].stem
                    shutil.rmtree(package)
                    for selector in (["--continue"], ["--resume", thread]):
                        resumed = subprocess.run([str(binary), "--config", str(configuration), "--print",
                                                  *selector, "Continue plugin audit"], cwd=workspace, env=env,
                                                 stdin=subprocess.DEVNULL, capture_output=True, text=True, timeout=15)
                        assert resumed.returncode == 0 and resumed.stdout.strip() == "Plugin CLI complete", resumed.stderr
                        assert not resumed.stderr
                        restored = json.dumps(server.requests[-1])
                        assert "Review CLI args" in restored and "Check correctness" in restored
                    captured = server.requests[before:]
                    assert len(captured) == 5
                    assert all(str(package) not in json.dumps(request) for request in captured)
                    results.append({"family": family, "format": kind, "requests": len(captured),
                                    "explicit_content": True, "invalid_source_isolated": True,
                                    "model_main": "denied_by_policy" if kind == "codex" else "loaded",
                                    "supporting_resource": True, "offline_resume": 2})
        finally:
            server.shutdown()
            owner.join()
            server.server_close()
    return results


def run_case(binary, root, family, no_color):
    server = http.server.HTTPServer(("127.0.0.1", 0), Fixture)
    server.family = family
    server.requests = []
    server.second_entered = threading.Event()
    server.release_second = threading.Event()
    owner = threading.Thread(target=server.serve_forever)
    owner.start()
    master, slave = pty.openpty()
    ack_read, ack_write = os.pipe()
    fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", 24, 80, 0, 0))
    before = termios.tcgetattr(slave)
    env = os.environ.copy()
    env.update(EASYCODE_PLUGINS_TERMINAL_SMOKE="1",
               EASYCODE_PLUGINS_ACK_FD=str(ack_write),
               EASYCODE_PLUGINS_FIXTURE_URL=f"http://127.0.0.1:{server.server_port}",
               EASYCODE_PLUGINS_FIXTURE_FAMILY=family,
               TERM="xterm-256color")
    if no_color:
        env["NO_COLOR"] = "1"
    else:
        env.pop("NO_COLOR", None)
    process = subprocess.Popen([str(binary), "-test.run=^TestPluginsTerminalHelper$", "-test.timeout=45s"],
                               cwd=root / "internal/app", stdin=slave, stdout=slave,
                               stderr=slave, env=env, pass_fds=(ack_write,))
    os.close(ack_write)
    pending, output, messages = bytearray(), bytearray(), []

    def await_ack(predicate):
        deadline = time.monotonic() + 10
        while True:
            while b"\n" in pending:
                raw, _, rest = pending.partition(b"\n")
                pending[:] = rest
                message = json.loads(raw)
                messages.append(message)
                if predicate(message):
                    return message
            remaining = deadline - time.monotonic()
            if remaining <= 0:
                raise RuntimeError(f"missing plugin acknowledgement: {messages[-2:]} terminal={output[-1200:]!r}")
            readable, _, _ = select.select([master, ack_read], [], [], remaining)
            for fd in readable:
                data = os.read(fd, 65536)
                if not data:
                    raise RuntimeError(f"PTY ended early: {output[-1600:]!r}")
                (pending if fd == ack_read else output).extend(data)

    def key(data, expected=None, contains=None):
        os.write(master, data)
        return await_ack(lambda m: m["kind"] == "key"
                         and (expected is None or m["key"] == expected)
                         and (contains is None or contains in m["view"]))

    def erase(count):
        for _ in range(count):
            key(b"\x7f", "backspace")

    try:
        await_ack(lambda m: m["kind"] == "ready")
        key(b"/plug", contains="plugins [command]")
        key(b"\r", "enter", "Plugins · Skills only")
        key(b"\r", "enter", "Plugin details")
        key(b"\x1b[200~ignored\t\r\x1b\x03\x1b[201~", contains="Plugin details")
        assert not server.requests
        key(b"\x1b", "esc", "Plugins · Skills only")
        key(b"\x1b", "esc", "/plug|")
        erase(5)
        key(b"/plugins", contains="/plugins|")
        key(b"\r", "enter", "Plugins · Skills only")
        key(b"\r", "enter", "Plugin details")
        for width, height in ((28, 10), (8, 2), (80, 24)):
            fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", height, width, 0, 0))
            process.send_signal(signal.SIGWINCH)
            resized = await_ack(lambda m: m["kind"] == "size" and m["width"] == width and m["height"] == height)
            assert "Esc:" in resized["view"]
        key(b"\r", "enter", "/claudeqa:review |")
        key(b"\x1b[200~PTY args\x1b[201~", contains="PTY args|")
        key(b"\r", "enter")
        completed = await_ack(lambda m: m["kind"] == "idle")
        assert completed["event"] == "turn_completed"
        assert len(server.requests) == 1
        assert {"openai": "input", "anthropic": "messages"}[family] in server.requests[0]
        assert "Review PTY args" in json.dumps(server.requests[0])
        key(b"/skills", contains="/skills|")
        key(b"\r", "enter", "Skills")
        key(b"\r", "enter", "$plugins |")
        assert len(server.requests) == 1
        erase(len("$plugins "))
        key(b"/claudeqa:review second", contains="second|")
        key(b"\r", "enter")
        assert server.second_entered.wait(10), "second request never started"
        key(b"/plugins", contains="/plugins|")
        key(b"\r", "enter", "Plugins · Skills only")
        key(b"\x03", "ctrl+c")
        await_ack(lambda m: m.get("event") == "turn_failed")
        server.release_second.set()
        erase(len("/plugins"))
        key(b"\x03", "ctrl+c")
        deadline = time.monotonic() + 10
        while process.poll() is None:
            remaining = deadline - time.monotonic()
            if remaining <= 0:
                raise RuntimeError(f"plugin PTY did not exit: {output[-1200:]!r}")
            readable, _, _ = select.select([master], [], [], min(0.2, remaining))
            if readable:
                try:
                    output.extend(os.read(master, 65536))
                except OSError:
                    break
        assert process.wait(timeout=5) == 0, output[-2000:]
        after = termios.tcgetattr(slave)
        pending_flag = getattr(termios, "PENDIN", 0)
        before[3] &= ~pending_flag
        after[3] &= ~pending_flag
        assert before == after, "terminal settings were not restored"
        assert len(server.requests) == 2
        return {"family": family, "no_color": no_color, "requests": len(server.requests),
                "provider_wire": True, "turn_completed": True,
                "draft_restored": True, "reserved_skill": True,
                "paste_isolated": True, "active_stop": True,
                "sizes": [[80, 24], [28, 10], [8, 2]], "terminal_restored": True}
    finally:
        server.release_second.set()
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
        raise SystemExit("plugins PTY acceptance requires Darwin")
    root = Path(__file__).resolve().parents[1]
    with tempfile.TemporaryDirectory(prefix="easycode-plugins-smoke-") as directory:
        binary = Path(directory) / "app.test"
        cli = Path(directory) / "easycode"
        subprocess.run(["go", "test", "-c", "-o", str(binary), "./internal/app"], cwd=root, check=True)
        subprocess.run(["go", "build", "-o", str(cli), "./cmd/easycode"], cwd=root, check=True)
        print(json.dumps({"platform": platform.platform(),
                          "binary_cli_cases": run_cli_cases(cli, root),
                          "cases": [run_case(binary, root, family, no_color)
                                    for family in ("openai", "anthropic")
                                    for no_color in (False, True)]}, indent=2))
