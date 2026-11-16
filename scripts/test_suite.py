#!/usr/bin/env python3
"""以有界进程运行完整 Go 测试，保留标准测试事件与可核对的执行清单。"""

import argparse
import concurrent.futures
import hashlib
import json
import os
from pathlib import Path
import re
import shlex
import signal
import subprocess
import sys
import tempfile
import threading
import time

try:
    import resource
except ImportError:
    resource = None


def source_digest(root):
    """覆盖执行代码和固定测试数据，运行中修改输入不能得到成功的旧结果。"""
    paths = [root / name for name in ("Makefile", "go.mod", "go.sum", "AGENTS.md")]
    for name in ("cmd", "internal", "scripts", ".github", ".githooks"):
        paths.extend(path for path in (root / name).rglob("*")
                     if path.is_file() and "__pycache__" not in path.parts)
    digest = hashlib.sha256()
    for path in sorted(paths):
        digest.update(str(path.relative_to(root)).encode())
        digest.update(b"\0")
        digest.update(path.read_bytes())
    return digest.hexdigest()


def inventory(events):
    result = {}
    for event in events:
        package = event.get("Package")
        if not package:
            continue
        names = result.setdefault(package, set())
        name = event.get("Output", "").strip()
        if re.fullmatch(r"(?:Test|Example|Fuzz)\w*", name):
            names.add(name)
    return {package: sorted(names) for package, names in sorted(result.items())}


def partition(names, count):
    """按完整名称分配，新增测试自动进入清单，子场景始终随顶层入口执行。"""
    if count < 1:
        raise ValueError("shard count must be positive")
    names = sorted(set(names))
    return [names[index::count] for index in range(min(count, len(names)))]


def selection(names):
    return "^(?:" + "|".join(re.escape(name) for name in names) + ")$"


def process_groups(roots):
    """取消时同时收集测试创建的独立进程组，仅作用于本次启动进程的后代。"""
    groups = set(roots)
    if not roots:
        return groups
    if os.name == "nt":
        return groups
    output = subprocess.check_output(["ps", "-eo", "pid=,ppid=,pgid="], text=True)
    rows = [tuple(map(int, line.split())) for line in output.splitlines()]
    descendants = set(roots)
    while True:
        added = {pid for pid, parent, _ in rows if parent in descendants} - descendants
        if not added:
            break
        descendants.update(added)
    groups.update(group for pid, _, group in rows if pid in descendants)
    # 父进程异常退出后，同一 session 中的独立进程组仍可按启动时的 session 身份回收。
    for pid, _, group in rows:
        try:
            if os.getsid(pid) in roots:
                groups.add(group)
        except (ProcessLookupError, PermissionError):
            pass
    groups.difference_update((0, 1))
    groups.discard(os.getpgrp())
    return groups


class ProcessOwner:
    """工作线程各自等待唯一子进程，主线程统一停止派发和取消进程树。"""

    def __init__(self, directory, environment):
        self.directory = directory
        self.environment = environment
        self.lock = threading.Lock()
        self.active = {}
        self.cancelled = threading.Event()
        self.groups = set()
        self.peak_active = 0

    def execute(self, label, command, cwd):
        start = time.monotonic()
        path = self.directory / (label + ".jsonl")
        if self.cancelled.is_set():
            return {"label": label, "exit": 130, "seconds": 0, "events": []}
        # 启动与磁盘操作在锁外；登记后再次检查取消，关闭启动和取消交错的窗口。
        try:
            process = subprocess.Popen(command, cwd=cwd, env=self.environment,
                                       stdout=subprocess.PIPE, stderr=subprocess.STDOUT,
                                       text=True, errors="replace", start_new_session=os.name != "nt",
                                       creationflags=subprocess.CREATE_NEW_PROCESS_GROUP if os.name == "nt" else 0)
        except OSError:
            self.cancelled.set()
            raise
        with self.lock:
            self.active[process.pid] = process
            self.peak_active = max(self.peak_active, len(self.active))
        if self.cancelled.is_set():
            self.signal_groups({process.pid}, 9)
        events = []
        try:
            with path.open("w", encoding="utf-8") as log:
                for line in process.stdout:
                    log.write(line)
                    try:
                        event = json.loads(line)
                    except ValueError:
                        continue
                    if isinstance(event, dict):
                        events.append(event)
            code = process.wait()
            if code:
                # 在 worker 交还执行槽前停止后续工作，避免调度线程尚未读取失败结果。
                self.cancelled.set()
        except BaseException:
            self.cancelled.set()
            raise
        finally:
            process.stdout.close()
            if process.poll() is None:
                self.signal_groups({process.pid}, 9)
                process.wait()
            with self.lock:
                self.active.pop(process.pid, None)
        return {"label": label, "command": command, "exit": code,
                "seconds": round(time.monotonic() - start, 3), "log": str(path), "pid": process.pid, "events": events}

    @staticmethod
    def signal_groups(groups, sig):
        denied = None
        for group in groups:
            if os.name == "nt":
                command = ["taskkill", "/T", "/PID", str(group)]
                if sig == 9:
                    command.append("/F")
                subprocess.run(command, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, check=False)
                continue
            try:
                os.killpg(group, sig)
            except ProcessLookupError:
                pass
            except PermissionError as error:
                # Darwin 对仅剩僵尸的组也可能返回 EPERM；继续回收其他仍活跃的组。
                current = subprocess.check_output(["ps", "-eo", "pgid=,stat="], text=True)
                live = [line.split() for line in current.splitlines()]
                states = [fields[1] for fields in live if int(fields[0]) == group]
                if any(not state.startswith("Z") for state in states):
                    denied = PermissionError(f"cannot signal process group {group}: states={states}: {error}")
        if denied is not None:
            raise denied

    def failed_pid(self):
        with self.lock:
            active = list(self.active.items())
        # 不能只等待 stdout EOF：失败父进程的后代可能仍持有继承的输出描述符。
        for pid, process in active:
            code = process.poll()
            if code is not None and code != 0:
                return pid
        return None

    def cancel(self, failed_pid=None):
        self.cancelled.set()
        with self.lock:
            roots = set(self.active)
        if failed_pid is not None:
            roots.add(failed_pid)
        self.groups.update(process_groups(roots))
        self.signal_groups(self.groups, signal.SIGTERM)

    def escalate(self):
        with self.lock:
            roots = set(self.active)
        self.groups.update(process_groups(roots))
        self.signal_groups(self.groups, 9)


def run_jobs(owner, jobs, workers, deadline, interrupted, clock=time.monotonic):
    results = []
    cancelled_at = None
    escalated = False
    if interrupted() or clock() >= deadline:
        cancelled_at = clock()
        owner.cancel()
    with concurrent.futures.ThreadPoolExecutor(max_workers=workers) as pool:
        pending = {pool.submit(owner.execute, *job): job[0] for job in jobs}
        while pending:
            failed_pid = owner.failed_pid()
            if cancelled_at is None and (interrupted() or clock() >= deadline or failed_pid is not None):
                cancelled_at = clock()
                owner.cancel(failed_pid)
            if cancelled_at is not None and not escalated and clock() - cancelled_at >= 1:
                owner.escalate()
                escalated = True
            done, _ = concurrent.futures.wait(pending, timeout=0.1,
                                             return_when=concurrent.futures.FIRST_COMPLETED)
            for future in done:
                label = pending.pop(future)
                try:
                    result = future.result()
                except Exception as error:
                    result = {"label": label, "exit": 1, "seconds": 0, "events": [], "error": str(error)}
                results.append(result)
                print(f"{result['label']}: exit={result['exit']} {result['seconds']:.3f}s", flush=True)
                if result["exit"] and cancelled_at is None:
                    cancelled_at = clock()
                    owner.cancel(result.get("pid"))
    if cancelled_at is not None and not escalated:
        owner.escalate()
    return results


def verify_execution(result, expected):
    completed = [event["Test"] for event in result["events"]
                 if event.get("Action") in ("pass", "skip") and "Test" in event and "/" not in event["Test"]]
    if sorted(completed) != sorted(expected):
        if result["exit"] == 0:
            result["exit"] = 1
        result["error"] = "test inventory mismatch"
        result["missing"] = sorted(set(expected) - set(completed))
        result["unexpected"] = sorted(set(completed) - set(expected))
    result["completed"] = completed


def run_suite(args):
    started = time.monotonic()
    root = Path(__file__).resolve().parent.parent
    directory = Path(args.output).resolve() if args.output else Path(tempfile.mkdtemp(prefix="easycode-tests-"))
    directory.mkdir(parents=True, exist_ok=True)
    print(f"Test report: {directory}", flush=True)
    environment = dict(os.environ, GOMAXPROCS=str(args.parallel))
    owner = ProcessOwner(directory, environment)
    go = shlex.split(args.go)
    deadline = started + args.budget
    stopped = []
    previous = {sig: signal.signal(sig, lambda received, _: stopped.append(received))
                for sig in (signal.SIGINT, signal.SIGTERM)}
    report = {"mode": args.mode, "jobs": args.jobs, "parallel": args.parallel, "shards": args.shards,
              "exit": 1, "platform": sys.platform, "cpu_count": os.cpu_count(),
              "budget_seconds": args.budget, "test_result_cache": False, "phases": [], "runs": []}
    if hasattr(os, "sysconf"):
        report["memory_bytes"] = os.sysconf("SC_PAGE_SIZE") * os.sysconf("SC_PHYS_PAGES")

    def execute(jobs, workers):
        results = run_jobs(owner, jobs, workers, deadline, lambda: bool(stopped))
        report["runs"].extend(results)
        if owner.cancelled.is_set() or any(result["exit"] for result in results):
            raise RuntimeError("verification command failed or was cancelled")
        return results

    try:
        report["source_digest"] = source_digest(root)
        metadata = execute([("environment", [*go, "env", "-json", "GOOS", "GOARCH", "GOVERSION", "GOCACHE"], root)], 1)[0]
        report["go_environment"] = json.loads(Path(metadata["log"]).read_text(encoding="utf-8"))
        if args.mode == "verify":
            execute([("static", ["make", "--no-print-directory", "-j1", "branch-policy-test", "fmt-check",
                                  "vet", "lint", "test-runner-test", "GO=" + args.go], root)], 1)
        report["inventory"] = {}
        modes = [args.mode] if args.mode in ("normal", "race") else ["normal", "race"]
        for mode in modes:
            phase_start = time.monotonic()
            discovery = [*go, "test", "-json", "-list=.", "-p", str(args.jobs)]
            if mode == "race":
                discovery.append("-race")
            discovery.append("./...")
            discovered = execute([(mode + "-inventory", discovery, root)], 1)[0]
            names = inventory(discovered["events"])
            if not names:
                raise RuntimeError("empty package inventory")
            report["inventory"][mode] = names
            jobs, expected = [], {}
            for package, tests in names.items():
                count = args.shards if package == "easycode/internal/app" else 1
                shards = partition(tests, count) or [[]]
                for index, shard in enumerate(shards):
                    label = mode + "-" + package.replace("/", "-") + f"-{index + 1}"
                    command = [*go, "test", "-json", "-count=1", "-p=1", "-parallel=" + str(args.parallel),
                               "-timeout=" + args.timeout]
                    if mode == "race":
                        command.append("-race")
                    command.extend(["-run=" + selection(shard), package])
                    jobs.append((label, command, root))
                    expected[label] = shard
            # 先派发关键路径；其他包按名称确定排序，所有进程共享同一并发上限。
            jobs.sort(key=lambda job: ("-internal-app-" not in job[0], job[0]))
            results = run_jobs(owner, jobs, args.jobs, deadline, lambda: bool(stopped))
            report["runs"].extend(results)
            for result in results:
                verify_execution(result, expected[result["label"]])
            failed = owner.cancelled.is_set() or any(result["exit"] for result in results)
            report["phases"].append({"mode": mode, "seconds": round(time.monotonic() - phase_start, 3),
                                     "exit": int(failed)})
            if failed:
                if time.monotonic() >= deadline:
                    raise RuntimeError("verification exceeded time budget")
                raise RuntimeError("test execution failed, was cancelled, or did not match inventory")
        if time.monotonic() >= deadline:
            raise RuntimeError("verification exceeded time budget")
        if source_digest(root) != report["source_digest"]:
            raise RuntimeError("verification inputs changed during execution")
        report["exit"] = 0
    except (RuntimeError, OSError, subprocess.SubprocessError) as error:
        report["exit"] = 1
        report["error"] = str(error)
        print(str(error), file=sys.stderr)
        failures = [result for result in report["runs"] if result["exit"] and result.get("log")]
        for result in failures[:3]:
            print(f"{result['label']}: {result['log']}", file=sys.stderr)
            print(Path(result["log"]).read_text(encoding="utf-8")[-4000:], file=sys.stderr)
    finally:
        for sig, handler in previous.items():
            signal.signal(sig, handler)
        report["seconds"] = round(time.monotonic() - started, 3)
        report["peak_active_commands"] = owner.peak_active
        if resource is not None:
            usage = resource.getrusage(resource.RUSAGE_CHILDREN)
            report["max_child_rss_bytes"] = usage.ru_maxrss * (1 if sys.platform == "darwin" else 1024)
            report["child_cpu_seconds"] = round(usage.ru_utime + usage.ru_stime, 3)
        slow = []
        for result in report["runs"]:
            for event in result["events"]:
                if event.get("Action") == "pass" and event.get("Test") and "/" not in event["Test"]:
                    slow.append({"phase": result["label"].split("-", 1)[0], "package": event["Package"],
                                 "test": event["Test"], "seconds": event.get("Elapsed", 0)})
            result.pop("events", None)
        report["slow_tests"] = sorted(slow, key=lambda item: item["seconds"], reverse=True)[:20]
        (directory / "summary.json").write_text(json.dumps(report, indent=2) + "\n", encoding="utf-8")
        print(f"Total: {report['seconds']:.3f}s; exit={report['exit']}; report={directory / 'summary.json'}", flush=True)
    return report["exit"]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--mode", choices=("normal", "race", "all", "verify"), default="all")
    parser.add_argument("--go", default="go")
    # 子进程清理及真实 I/O 会等待；有界超配允许其他包利用空闲执行时间。
    parser.add_argument("--jobs", type=int, default=min(8, 2 * (os.cpu_count() or 1)))
    parser.add_argument("--parallel", type=int, default=2)
    # Darwin 的重矩阵需要细分；其他平台减少独立进程中的重复初始化。
    parser.add_argument("--shards", type=int, default=16 if sys.platform == "darwin" else 4)
    parser.add_argument("--timeout", default="20m")
    parser.add_argument("--budget", type=float, default=300)
    parser.add_argument("--output")
    args = parser.parse_args()
    if min(args.jobs, args.parallel, args.shards, args.budget) <= 0:
        parser.error("resource limits and budget must be positive")
    return run_suite(args)


if __name__ == "__main__":
    sys.exit(main())
