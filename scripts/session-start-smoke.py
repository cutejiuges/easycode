#!/usr/bin/env python3
"""用真实 EasyCode CLI、Darwin PTY 和本地双 Provider 服务验收 SessionStart。"""

from smoke_support import smoke_binary, complete_cases
import argparse
import http.server
import json
import os
from pathlib import Path
import platform
import runpy
import subprocess
import tempfile
import threading


SUPPORT = runpy.run_path(str(Path(__file__).with_name("plan-mode-smoke.py")))
capture_terminal = SUPPORT["capture_terminal"]

STARTUP = "startup question " + "setup " * 1400 + "done"
RESUMED = "resume question " + "detail " * 2700 + "done"
NEXT = "automatic compaction question " + "more " * 2000 + "done"
DENIED = "question after denying resume context"
RAW_STDERR = "SESSION-START-PRIVATE-STDERR"


class Fixture(SUPPORT["Fixture"]):
    def do_POST(self):
        body = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
        self.server.requests.append(body)
        number = len(self.server.requests)
        family = self.server.family
        summary = body.get("tool_choice") in ("none", {"type": "none"})
        reply = f"checkpoint-summary-{number}" if summary else f"session-start-answer-{number}"
        self.send_response(200)
        self.send_header("Content-Type", "text/event-stream")
        self.end_headers()
        self.stream(family, number, [], reply)


def hook_context(source):
    return f"frozen {source} workspace context"


def write_hooks(workspace):
    hook_script = workspace / "session-start-fixture.sh"
    hook_script.write_text(r"""
case "$(cat)" in
  *'"source":"startup"'*) source=startup ;;
  *'"source":"resume"'*) source=resume ;;
  *'"trigger":"manual"'*) source=manual ;;
  *'"trigger":"auto"'*) source=auto ;;
  *) exit 2 ;;
esac
printf 'SESSION-START-PRIVATE-STDERR\n' >&2
printf '{"hookSpecificOutput":{"hookEventName":"SessionStart","additionalContext":"frozen %s workspace context"}}\n' "$source"
""")
    hook_dir = workspace / ".easycode"
    hook_dir.mkdir()
    config = hook_dir / "hooks.json"
    config.write_text(json.dumps({"hooks": {"SessionStart": [
        {"matcher": source, "hooks": [{"id": source, "type": "command",
                                      "command": "/bin/sh ./session-start-fixture.sh", "feedback": "model"}]}
        for source in ("startup", "resume", "compact")
    ]}}))
    config.chmod(0o600)


def verify_review(terminal, trigger):
    title = "trust this SessionStart hook?" if trigger.startswith("compact") else "Trust this SessionStart hook?"
    terminal.expect(title)
    terminal.expect(f"Triggered by: {trigger}")
    terminal.expect("> Deny")


def trust(terminal):
    terminal.send(b"\t")
    terminal.expect("> Trust for this session")
    terminal.send(b"\r")


def verify_detail(terminal):
    assert hook_context("startup").encode() not in terminal.buffer
    terminal.send(b"\x0f")
    terminal.expect("  SessionStart (startup) · Saved model context")
    terminal.send(b"\x1b[B\x1b[B")
    terminal.expect("> SessionStart (startup) · Saved model context")
    assert hook_context("startup").encode() not in terminal.buffer
    terminal.send(b"\r")
    terminal.expect(hook_context("startup"))
    terminal.send(b"\x1b")
    terminal.expect("Ctrl+O/Esc: close")
    terminal.send(b"\x1b")
    terminal.expect("Shift+Tab: switch mode")


def read_journal(path):
    data = path.read_bytes()
    assert RAW_STDERR.encode() not in data
    return list(map(json.loads, data.splitlines()))


def wire_text(request, family):
    items = request["input" if family == "openai" else "messages"]
    return "\n".join(part["text"] for item in items for part in item.get("content", [])
                     if isinstance(part, dict) and "text" in part)


def verify_order(text, *parts):
    start = 0
    for part in parts:
        marker = text.find(part, start)
        assert marker >= 0 and text.count(part) == 1, f"expected once and in order: {part[:100]!r}: {text[:250]!r}"
        start = marker + len(part)


def verify_journal(records):
    closures = [item for item in records if item["event_kind"] == "session_start_hook_closed"]
    assert [item["payload"]["target"]["source"] for item in closures] == [
        "startup", "compact", "resume", "compact", "resume", "resume"]
    assert [item["payload"]["state"] for item in closures] == ["applied"] * 4 + ["skipped", "cancelled"]
    for item in closures[:4]:
        target = item["payload"]["target"]
        native = item["payload"]["native"]["payload"]
        assert native["kind"] == "session_start_context"
        if target["source"] != "compact":
            batch = [entry for entry in records if entry["batch_id"] == item["batch_id"]]
            assert [entry["event_kind"] for entry in batch] == [
                "session_start_hook_closed", "turn_inputs_committed", "provider_native_commit"]
            assert item["turn_id"] == target["turn_id"]
            continue
        assert item["batch_size"] == 1 and "turn_id" not in item
        checkpoint, = (record for record in records if record["seq"] == target["checkpoint_sequence"])
        assert checkpoint["event_kind"] == "compaction_completed" and checkpoint["seq"] < item["seq"]
    assert all("native" not in item["payload"] for item in closures[4:])
    assert closures[4]["payload"]["code"] == "hook_trust_denied"
    skipped = {item["payload"]["plan_digest"] for item in closures[4:]}
    assert not any(item["event_kind"] == "session_start_hook_started" and item["payload"]["plan_digest"] in skipped
                   for item in records)


def run_case(binary, temporary, family, output):
    directory = temporary / family
    workspace, home = directory / "workspace", directory / "home"
    workspace.mkdir(parents=True)
    home.mkdir(mode=0o700)
    write_hooks(workspace)
    server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Fixture)
    server.family, server.requests = family, []
    config = directory / "config.json"
    config.write_text(json.dumps({
        "provider": family, "base_url": f"http://127.0.0.1:{server.server_port}",
        "model": "fixture-model", "api_key": "local-session-start-fixture",
        "context_window_tokens": 11000, "reserved_output_tokens": 1024,
        "max_output_tokens": 1024, "context_safety_margin_tokens": 128,
        "auto_compact": True, "compaction_output_tokens": 512,
    }))
    config.chmod(0o600)
    env = {key: value for key, value in os.environ.items() if not key.startswith("EASYCODE_")}
    env.update(HOME=str(home), TERM="xterm-256color", NO_COLOR="1")
    flags = [str(binary), "--config", str(config)]
    owner = threading.Thread(target=server.serve_forever)
    owner.start()
    try:
        with capture_terminal(flags, workspace, env, output / f"{family}-startup.pty") as terminal:
            terminal.resize(110, 32)
            terminal.expect("Default")
            assert len(server.requests) == 0
            terminal.submit(STARTUP)
            verify_review(terminal, "startup")
            assert len(server.requests) == 0
            trust(terminal)
            terminal.expect("session-start-answer-1")
            terminal.wait_idle()
            verify_detail(terminal)
            terminal.submit("/compact")
            verify_review(terminal, "compact (manual)")
            assert len(server.requests) == 2
            trust(terminal)
            terminal.expect("SessionStart (compact) · Context saved (1)")
            assert hook_context("manual").encode() not in terminal.buffer
            terminal.quit()

        path, = (home / ".easycode" / "sessions").rglob("*.jsonl")
        before = path.read_bytes()
        flags += ["--resume", read_journal(path)[0]["thread_id"]]
        with capture_terminal(flags, workspace, env, output / f"{family}-resume.pty") as terminal:
            terminal.resize(110, 32)
            terminal.expect("Default")
            assert len(server.requests) == 2 and path.read_bytes() == before
            terminal.submit(RESUMED)
            verify_review(terminal, "resume")
            assert len(server.requests) == 2
            trust(terminal)
            terminal.expect("session-start-answer-3")
            terminal.wait_idle()
            terminal.submit(NEXT)
            verify_review(terminal, "compact (auto)")
            assert len(server.requests) == 4
            trust(terminal)
            terminal.expect("session-start-answer-5")
            terminal.quit()

        with capture_terminal(flags, workspace, env, output / f"{family}-denied.pty") as terminal:
            terminal.resize(110, 32)
            terminal.expect("Default")
            terminal.submit(DENIED)
            verify_review(terminal, "resume")
            assert len(server.requests) == 5
            terminal.send(b"\r")
            terminal.expect("session-start-answer-6")
            terminal.quit()

        with capture_terminal(flags, workspace, env, output / f"{family}-cancelled.pty") as terminal:
            terminal.resize(110, 32)
            terminal.expect("Default")
            terminal.submit("question cancelled during resume review")
            verify_review(terminal, "resume")
            terminal.send(b"\x03")
            terminal.expect("SessionStart (resume) · Cancelled")
            terminal.expect("Error: user_cancelled: turn was cancelled")
            terminal.quit()

        assert len(server.requests) == 6
        startup, manual, resumed, auto, continued, denied = (wire_text(item, family) for item in server.requests)
        verify_order(startup, hook_context("startup"), STARTUP)
        verify_order(manual, hook_context("startup"), STARTUP)
        assert hook_context("manual") not in manual
        verify_order(resumed, "checkpoint-summary-2", hook_context("manual"), hook_context("resume"), RESUMED)
        assert hook_context("auto") not in auto and NEXT not in auto
        verify_order(continued, "checkpoint-summary-4", hook_context("auto"), NEXT)
        verify_order(denied, hook_context("auto"), DENIED)
        assert hook_context("resume") not in denied
        assert all(RAW_STDERR not in item for item in (startup, manual, resumed, auto, continued, denied))
        verify_journal(read_journal(path))
        print(f"{family}: verified startup, Ctrl+O details, CLI resume, manual and automatic compaction, denial and cancellation", flush=True)
    finally:
        server.shutdown()
        server.server_close()
        owner.join()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, default=Path(".test-results/session-start"))
    args = parser.parse_args()
    if platform.system() != "Darwin":
        parser.error("this smoke test requires Darwin")
    args.output.mkdir(exist_ok=True, parents=True)
    with tempfile.TemporaryDirectory(prefix="easycode-session-start-pty-") as name:
        temporary = Path(name).resolve()
        repository = Path(__file__).resolve().parent.parent
        binary = smoke_binary("easycode", temporary, repository)
        cases = []
        for family in ("openai", "anthropic"):
            run_case(binary, temporary, family, args.output)
            cases.append({"provider": family, "terminal_restored": True})
        complete_cases(cases)


if __name__ == "__main__":
    main()
