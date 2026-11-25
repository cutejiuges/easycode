"""验证测试清单、并发上限和真实子进程取消，不使用延时猜测调度结果。"""

import errno
import copy
import json
import os
from pathlib import Path
import re
import shlex
import socket
import subprocess
import sys
import tempfile
import threading
import time
import unittest
from unittest import mock

import test_suite
import test_binary
import test_ratio
from race_policy import select_race
from test_binary import binary_path


class TestSizeTests(unittest.TestCase):
    def test_complete_snapshot_and_exact_limits(self):
        # 临时仓库必须隔离 hook 的 Git 定位环境，避免 init 修改真实仓库配置。
        environment = {key: value for key, value in os.environ.items() if not key.startswith("GIT_")}
        with mock.patch.dict(os.environ, environment, clear=True), \
                tempfile.TemporaryDirectory(prefix="easycode-size-test-") as directory:
            root = Path(directory)
            subprocess.run(["git", "init", "-q", str(root)], check=True)
            fixtures = {"internal/p/main.go": "package p\n// 业务\nfunc run() {}\n",
                        "internal/p/check_darwin_test.go": "package p\n// Darwin\n",
                        "scripts/run.py": "pass\n", "internal/p/testdata/input.json": "{}\n"}
            for name, content in fixtures.items():
                path = root / name
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_text(content)
            (root / ".gitignore").write_text("ignored_test.go\n")
            (root / "ignored_test.go").write_text("not source\n")
            policy = {"minimum_production_per_test": {"production": 3, "tests": 2},
                      "maximum_test_script_lines": 1}
            report = test_ratio.measure(root, test_ratio.source_paths(root))
            self.assertEqual((report["production"], report["tests"], report["scripts"]), (3, 2, 1))
            self.assertEqual((report["fixture_files"], report["fixture_bytes"]), (1, 3))
            self.assertEqual(test_ratio.violations(report, policy), [])
            target = root / "internal/p/testsupport/helper.go"
            target.parent.mkdir()
            (root / "internal/p/check_darwin_test.go").rename(target)
            moved = test_ratio.measure(root, test_ratio.source_paths(root))
            self.assertEqual((moved["production"], moved["tests"]), (3, 2))
            for change in ({"tests": 3}, {"production": 2}, {"scripts": 2},
                           {"production": 29999, "tests": 20000}):
                with self.subTest(change=change):
                    self.assertEqual(len(test_ratio.violations({**moved, **change}, policy)), 1)
            policy_path = root / "scripts/test_size_policy.json"
            policy_path.write_text(json.dumps(policy))
            (root / "scripts/run.py").write_text("pass\npass\n")
            before = policy_path.read_bytes()
            self.assertTrue(test_ratio.inspect(root)["errors"])
            self.assertEqual(policy_path.read_bytes(), before)
            for name, content in (("internal/p/test_helpers.go", 'package p\nimport "testing"\n'),
                                  ("tests/acceptance_test.py", "pass\n")):
                misplaced = root / name
                misplaced.parent.mkdir(parents=True, exist_ok=True)
                misplaced.write_text(content)
                with self.assertRaises(ValueError):
                    test_ratio.inspect(root)
                misplaced.unlink()


class InventoryTests(unittest.TestCase):
    def test_timings_include_parallel_children_but_exclude_parent_pause(self):
        events = [
            {"Action": "run", "Time": "2026-11-17T00:00:00.1Z"},
            {"Action": "pause", "Time": "2026-11-17T00:00:01.10000Z"},
            {"Action": "cont", "Time": "2026-11-17T08:00:10.100000000+08:00"},
            {"Action": "run", "Time": "2026-11-17T00:00:11.1Z", "Test": "TestParent/child"},
            {"Action": "pass", "Time": "2026-11-17T00:00:14.1Z", "Test": "TestParent/child", "Elapsed": 3},
            {"Action": "pass", "Time": "2026-11-17T00:00:15.1Z", "Elapsed": 1},
        ]
        events = [{"Package": "fixture/p", "Test": "TestParent", **event} for event in events]
        self.assertEqual(test_suite.test_timings(events), [{"package": "fixture/p", "test": "TestParent", "seconds": 6}])
        for stamp in (None, "invalid timestamp"):
            self.assertEqual(test_suite.test_timings([{"Package": "fixture/p", "Test": "TestMissingTime",
                                                       "Action": "pass", "Time": stamp, "Elapsed": 2}]),
                             [{"package": "fixture/p", "test": "TestMissingTime", "seconds": 2}])

    def test_inventory_includes_examples_fuzz_and_empty_packages(self):
        events = [{"Package": "example/p", "Output": name + "\n"} for name in
                  ["TestOne", "TestTwo", "Example", "ExampleOutput", "FuzzInput", "BenchmarkCost", "ok example/p"]]
        events.append({"Package": "example/empty", "Action": "skip"})
        self.assertEqual(test_suite.inventory(events), {
            "example/empty": [], "example/p": ["Example", "ExampleOutput", "FuzzInput", "TestOne", "TestTwo"]})

    def test_partition_is_deterministic_complete_and_disjoint(self):
        names = ["Test" + str(index) for index in range(37)] + ["FuzzInput", "ExampleOutput"]
        for count in (1, 2, 4, 100):
            shards = test_suite.partition(names, count)
            self.assertEqual(shards, test_suite.partition(list(reversed(names)), count))
            self.assertEqual(sorted(name for shard in shards for name in shard), sorted(names))
            for name in names:
                self.assertEqual(sum(bool(re.fullmatch(test_suite.selection(shard), name)) for shard in shards), 1)
            self.assertTrue(all(not re.fullmatch(test_suite.selection(shard), "TestUnlisted") for shard in shards))
        with self.assertRaises(ValueError):
            test_suite.partition(names, 0)

    def test_missing_duplicate_and_unexpected_results_fail(self):
        for completed in (["TestA"], ["TestA", "TestB", "TestB"], ["TestA", "TestOther"]):
            result = {"exit": 0, "events": [{"Action": "pass", "Test": name} for name in completed]}
            test_suite.verify_execution(result, ["TestA", "TestB"])
            self.assertEqual(result["exit"], 1)
        result = {"exit": 0, "events": [{"Action": "pass", "Test": "TestA"},
                                          {"Action": "pass", "Test": "TestA/child"},
                                          {"Action": "skip", "Test": "TestB"}]}
        test_suite.verify_execution(result, ["TestA", "TestB"])
        self.assertEqual(result["exit"], 0)


class SuiteTests(unittest.TestCase):
    def test_invalid_diagnostic_budgets_are_rejected(self):
        runner = Path(test_suite.__file__).resolve()
        for budget in ("-1", "nan", "inf"):
            result = subprocess.run([sys.executable, "-B", str(runner), "--budget=" + budget],
                                    capture_output=True, text=True, timeout=5)
            self.assertEqual(result.returncode, 2)
            self.assertIn("budget must be finite and nonnegative", result.stderr)

    def test_build_tag_inventory_and_nested_build_keep_selected_toolchain(self):
        with tempfile.TemporaryDirectory(prefix="easycode-inventory-test-") as directory:
            root = Path(directory)
            toolchain_bin = root / "selected toolchain" / "bin"
            toolchain_bin.mkdir(parents=True)
            if os.name == "posix":
                nested_go = toolchain_bin / "go"
                nested_go.write_text("#!/bin/sh\nprintf 'fixture-selected-toolchain\\n'\n")
                nested_go.chmod(0o700)
                static = toolchain_bin / "make"
                static.write_text("#!" + sys.executable + "\nimport os,sys\n"
                                  "assert os.environ['GOMAXPROCS'] == str(min(2, os.cpu_count() or 1))\n"
                                  "assert all(x in sys.argv for x in ['branch-policy-test', 'fmt-check', 'vet', 'lint', 'test-runner-test'])\n")
                static.chmod(0o700)
            fake = root / "fake_go.py"
            fake.write_text('''import hashlib,json,os,re,shlex,subprocess,sys
from pathlib import Path
args = sys.argv[1:]
goroot = str(Path(__file__).parent.resolve()/'selected toolchain')
assert os.environ['GOMAXPROCS'] == ('1' if args[:2] == ['tool', 'test2json'] else str(min(2, os.cpu_count() or 1)))
if args[0] == 'env':
 print(json.dumps({'GOOS': 'fixture', 'GOARCH': 'fixture', 'GOVERSION': 'fixture', 'GOROOT': goroot, 'GOCACHE': 'fixture'}))
 sys.exit(0)
if args[0] == 'list':
 print('fixture/package '+str(Path(__file__).parent.resolve()))
 sys.exit(0)
if '-list=.' in args:
 directory = shlex.split(next(arg[6:] for arg in args if arg.startswith('-exec=')))[-1]
 name = hashlib.sha256(os.fsencode(Path(__file__).parent.resolve())).hexdigest()
 binary = Path(directory)/(name+('.exe' if os.name == 'nt' else '.test'))
 binary.write_text('race' if '-race' in args else 'normal')
 race = '-race' in args
else:
 assert args[:2] == ['tool', 'test2json']
 binary = Path(args[args.index('-p')+2])
 race = binary.read_text() == 'race'
 assert str(Path.cwd()) == str(Path(__file__).parent.resolve())
 assert os.environ['PWD'] == str(Path.cwd())
 assert os.environ['GOROOT'] == goroot
 if os.name == 'posix':
  assert subprocess.check_output(['go'], text=True).strip() == 'fixture-selected-toolchain'
 assert '-test.paniconexit0' in args and '-test.count=1' in args
 assert '-test.parallel=1' in args
names = ['Example', 'FuzzInput', 'TestRaceOnly' if race else 'TestNormalOnly']
for name in names:
 if '-list=.' in args:
  print(json.dumps({'Package': 'fixture/package', 'Output': name+'\\n'}))
 else:
  pattern = next(arg[10:] for arg in args if arg.startswith('-test.run='))
  if re.fullmatch(pattern, name):
   print(json.dumps({'Package': 'fixture/package', 'Action': 'pass', 'Test': name}))
print(json.dumps({'Package': 'fixture/package', 'Action': 'pass'}))
''')
            report = root / "report"
            runner = Path(test_suite.__file__).resolve()
            mode = "verify" if os.name == "posix" else "all"
            command = [sys.executable, "-B", str(runner), "--mode=" + mode, "--jobs=2", "--parallel=1",
                       "--race-scope=full",
                       "--go=" + shlex.join([sys.executable, str(fake)]), "--output=" + str(report)]
            result = subprocess.run(command, capture_output=True, text=True, timeout=10)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
            summary = json.loads((report / "summary.json").read_text())
            self.assertEqual(summary["budget_seconds"], 0)
            self.assertEqual(summary["race_scope"], "full")
            self.assertEqual(summary["race_exclusions"], [])
            self.assertEqual(summary["selected_inventory"], summary["inventory"])
            for mode, name in (("normal", "TestNormalOnly"), ("race", "TestRaceOnly")):
                expected = ["Example", "FuzzInput", name]
                self.assertEqual(summary["inventory"][mode]["fixture/package"], expected)
                completed = next(run["completed"] for run in summary["runs"]
                                 if run["label"] == mode + "-fixture-package-1")
                self.assertEqual(completed, expected)
            binaries = [Path(run['command'][run['command'].index('-p') + 2]) for run in summary['runs']
                        if 'test2json' in run.get('command', [])]
            self.assertEqual(len(binaries), 2)
            self.assertNotEqual(binaries[0], binaries[1])
            self.assertTrue(all(not binary.parent.exists() for binary in binaries))
            if os.name == "posix":
                self.assertEqual(next(run["exit"] for run in summary["runs"] if run["label"] == "static"), 0)
                self.assertLessEqual(summary["peak_active_commands"], 2)
                static.write_text("#!" + sys.executable + "\nimport sys\nsys.exit(7)\n")
                failed = root / "failed-report"
                result = subprocess.run([*command[:-1], "--output=" + str(failed)], capture_output=True, text=True, timeout=10)
                self.assertNotEqual(result.returncode, 0, result.stdout + result.stderr)
                summary = json.loads((failed / "summary.json").read_text())
                self.assertEqual(next(run["exit"] for run in summary["runs"] if run["label"] == "static"), 7)
                self.assertNotIn("race", summary["inventory"])


class RacePolicyTests(unittest.TestCase):
    def test_critical_plan_keeps_only_named_paths_and_preserves_normal_inventory(self):
        inventory = {"fixture/p": ["TestConcurrent", "TestSerial", "TestNew", "FuzzNew"]}
        original = copy.deepcopy(inventory)
        policy = [{"package": "fixture/p", "tests": ["TestConcurrent"], "reason": "并发提交关键路径"}]
        selected, unselected = select_race(inventory, policy, "linux")
        self.assertEqual(selected, {"fixture/p": ["TestConcurrent"]})
        self.assertEqual(inventory, original)
        self.assertEqual({x["test"] for x in unselected}, {"TestSerial", "TestNew", "FuzzNew"})
        for invalid, reason in [(policy * 2, "duplicate"),
                                ([{**policy[0], "tests": ["TestRemoved"]}], "stale"),
                                ([], "empty")]:
            with self.subTest(reason=reason), self.assertRaisesRegex(ValueError, reason):
                select_race(inventory, invalid, "linux")

    def test_critical_plan_respects_platform_availability(self):
        policy = [{"package": "fixture/p", "tests": ["TestUnix"], "reason": "描述符关键路径",
                   "platforms": ["darwin", "linux"]},
                  {"package": "fixture/p", "tests": ["TestOwner"], "reason": "通用关闭路径"}]
        selected, _ = select_race({"fixture/p": ["TestOwner"]}, policy, "windows")
        self.assertEqual(selected, {"fixture/p": ["TestOwner"]})
        with self.assertRaisesRegex(ValueError, "stale"):
            select_race({"fixture/p": ["TestOwner"]}, policy, "linux")


@unittest.skipUnless(os.name == "posix", "Executable script fixture requires a native POSIX host")
class BinaryCaptureTests(unittest.TestCase):
    def test_capture_preserves_cwd_arguments_exit_and_isolates_duplicate_package_names(self):
        with tempfile.TemporaryDirectory(prefix="easycode-binary-test-") as directory:
            root = Path(directory)
            cwd = root / "with spaces" / "package"
            cwd.mkdir(parents=True)
            original = root / "original.test"
            original.write_text("#!" + sys.executable + "\nimport json,os,sys\n"
                                "print(json.dumps([os.getcwd(), sys.argv[1:]]))\nsys.exit(7)\n")
            original.chmod(0o700)
            cache = root / "binaries"
            wrapper = Path(test_suite.__file__).with_name("test_binary.py").resolve()
            result = subprocess.run([sys.executable, str(wrapper), str(cache), str(original), "-test.list=.", "arg with spaces"],
                                    cwd=cwd, capture_output=True, text=True, timeout=5)
            self.assertEqual(result.returncode, 7, result.stderr)
            self.assertEqual(json.loads(result.stdout), [str(cwd.resolve()), ["-test.list=.", "arg with spaces"]])
            binary = binary_path(cache, cwd)
            self.assertTrue(binary.is_file())
            self.assertEqual(original.stat().st_ino, binary.stat().st_ino)
            self.assertNotEqual(binary, binary_path(cache, root / "another" / "package"))
            original.unlink()
            replay = subprocess.run([str(binary), "retained"], cwd=cwd, capture_output=True, text=True, timeout=5)
            self.assertEqual(replay.returncode, 7, replay.stderr)
            self.assertEqual(json.loads(replay.stdout), [str(cwd.resolve()), ["retained"]])

    def test_copy_fallback_retains_bytes_and_permissions_and_propagates_other_failures(self):
        with tempfile.TemporaryDirectory(prefix="easycode-link-test-") as directory:
            root = Path(directory)
            original = root / "original"
            original.write_bytes(b"compiled binary fixture")
            original.chmod(0o700)
            for code in (errno.EXDEV, errno.EPERM, errno.ENOTSUP):
                with self.subTest(code=code):
                    retained = root / str(code)
                    with mock.patch.object(test_binary.os, "link", side_effect=OSError(code, "fixture")):
                        test_binary.retain_binary(original, retained)
                    self.assertEqual(retained.read_bytes(), original.read_bytes())
                    self.assertEqual(retained.stat().st_mode, original.stat().st_mode)
            with mock.patch.object(test_binary.os, "link", side_effect=OSError(errno.ENOSPC, "fixture")):
                with self.assertRaises(OSError), mock.patch.object(test_binary.shutil, "copy2") as copy:
                    test_binary.retain_binary(original, root / "failed")
                copy.assert_not_called()


@unittest.skipUnless(os.name == "posix", "POSIX process-group lifecycle requires a native POSIX host")
class ProcessTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="easycode-runner-test-")
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.owner = test_suite.ProcessOwner(self.root, os.environ.copy())
        self.listener = socket.socket()
        self.listener.bind(("127.0.0.1", 0))
        self.listener.listen()
        self.listener.settimeout(5)
        self.addCleanup(self.listener.close)
        self.helper = self.root / "helper.py"
        self.helper.write_text('''import json,os,signal,socket,subprocess,sys
child = None
if len(sys.argv) > 2 and sys.argv[2] == 'ignore':
 signal.signal(signal.SIGTERM, signal.SIG_IGN)
mode = sys.argv[2] if len(sys.argv) > 2 else ''
if mode in ('child', 'orphan'):
 child = subprocess.Popen([sys.executable, '-c', 'import signal; signal.pause()'],
                          start_new_session=mode == 'child', preexec_fn=os.setpgrp if mode == 'orphan' else None)
s = socket.create_connection(('127.0.0.1', int(sys.argv[1])))
s.sendall((json.dumps({'pid': os.getpid(), 'child': child.pid if child else None, 'label': sys.argv[3]})+'\\n').encode())
command = s.recv(1)
if child and mode != 'orphan':
 child.terminate()
 child.wait()
sys.exit(7 if command == b'f' else 0)
''')
        self.connections = []
        self.addCleanup(lambda: [connection.close() for connection in self.connections])
        self.addCleanup(self.owner.escalate)

    def job(self, name, child=False, ignore=False):
        command = [sys.executable, str(self.helper), str(self.listener.getsockname()[1])]
        if child:
            command.append(child if isinstance(child, str) else "child")
        elif ignore:
            command.append("ignore")
        else:
            command.append("")
        command.append(name)
        return name, command, self.root

    def accept(self):
        connection, _ = self.listener.accept()
        connection.settimeout(5)
        self.connections.append(connection)
        data = b""
        while not data.endswith(b"\n"):
            data += connection.recv(1024)
        return connection, json.loads(data)

    def start(self, jobs, interrupted=lambda: False, clock=time.monotonic, **limits):
        results = []
        errors = []

        def run():
            try:
                results.extend(test_suite.run_jobs(self.owner, jobs, 2, clock() + 10, interrupted, clock, **limits))
            except Exception as error:
                errors.append(error)

        thread = threading.Thread(target=run)
        thread.start()
        self.addCleanup(lambda: thread.join(5))
        return thread, results, errors

    def assert_finished(self, state):
        thread, results, errors = state
        thread.join(5)
        self.assertFalse(thread.is_alive(), "runner retained its worker")
        self.assertFalse(errors)
        self.assertFalse(self.owner.active)
        return results

    def test_parallel_limit_and_all_results(self):
        state = self.start([self.job(str(index)) for index in range(4)])
        for _ in range(2):
            first, _ = self.accept()
            second, _ = self.accept()
            self.assertEqual(self.owner.peak_active, 2)
            first.sendall(b"x")
            second.sendall(b"x")
        results = self.assert_finished(state)
        self.assertEqual(len(results), 4)
        self.assertTrue(all(result["exit"] == 0 for result in results))

    def test_heavy_limit_keeps_light_work_running_without_omissions(self):
        jobs = [self.job("heavy-first"), self.job("heavy-second"), self.job("light")]
        state = self.start(jobs, heavy_labels=("heavy-first", "heavy-second"), heavy_limit=1)
        first, first_info = self.accept()
        second, second_info = self.accept()
        with self.owner.lock:
            active = set(self.owner.active)
        self.assertEqual(len(active), 2)
        # 两个连接建立后主进程仍被握手阻塞；只有轻包允许越过排队的第二个重包。
        self.assertEqual({first_info["label"], second_info["label"]}, {"heavy-first", "light"})
        first.sendall(b"x")
        second.sendall(b"x")
        third, _ = self.accept()
        third.sendall(b"x")
        results = self.assert_finished(state)
        self.assertEqual({result["label"] for result in results}, {job[0] for job in jobs})
        self.assertTrue(all(result["exit"] == 0 for result in results))

    def test_failure_cancels_sibling_and_reaps_independent_child_group(self):
        state = self.start([self.job("first", True), self.job("second", True), self.job("queued")])
        first, first_info = self.accept()
        _, second_info = self.accept()
        first.sendall(b"f")
        results = self.assert_finished(state)
        self.assertTrue(any(result["exit"] == 7 for result in results))
        self.assertEqual(next(result["exit"] for result in results if result["label"] == "queued"), 130)
        self.assertTrue(self.owner.cancelled.is_set())
        for info in (first_info, second_info):
            # killpg 后僵尸只等待系统收尸，不能再执行或持有文件/CPU。
            completed = subprocess.run(["ps", "-p", str(info["child"]), "-o", "stat="], capture_output=True, text=True)
            self.assertTrue(not completed.stdout.strip() or completed.stdout.strip().startswith("Z"))

    def test_interrupt_cancels_running_work(self):
        stopped = threading.Event()
        state = self.start([self.job("first"), self.job("second")], stopped.is_set)
        self.accept()
        self.accept()
        stopped.set()
        results = self.assert_finished(state)
        self.assertTrue(all(result["exit"] != 0 for result in results))

    def test_expired_budget_starts_no_process(self):
        results = test_suite.run_jobs(self.owner, [self.job("expired")], 1, time.monotonic() - 1, lambda: False)
        self.assertEqual(self.owner.peak_active, 0)
        self.assertEqual(results[0]["exit"], 130)

    def test_deadline_escalates_ignored_termination_with_controlled_clock(self):
        now = [0]
        state = self.start([self.job("first", ignore=True), self.job("second", ignore=True)], clock=lambda: now[0])
        self.accept()
        self.accept()
        now[0] = 11
        self.assertTrue(self.owner.cancelled.wait(5))
        # 接受取消后推进同一 owner 的宽限期，不通过真实延时猜测是否发生升级。
        now[0] = 13
        results = self.assert_finished(state)
        self.assertTrue(all(result["exit"] == -9 for result in results))

    def test_start_failure_stops_queued_work(self):
        results = test_suite.run_jobs(self.owner, [("missing", [str(self.root / "missing")], self.root),
                                                  self.job("queued")], 1, time.monotonic() + 5, lambda: False)
        self.assertEqual(self.owner.peak_active, 0)
        self.assertEqual(next(result["exit"] for result in results if result["label"] == "queued"), 130)

    def test_failed_parent_cannot_leave_an_orphan_in_its_session(self):
        state = self.start([self.job("orphan", child="orphan")])
        connection, info = self.accept()
        connection.sendall(b"f")
        results = self.assert_finished(state)
        self.assertEqual(results[0]["exit"], 7)
        child = subprocess.run(["ps", "-p", str(info["child"]), "-o", "stat="], capture_output=True, text=True)
        self.assertTrue(not child.stdout.strip() or child.stdout.strip().startswith("Z"))

    def test_disappeared_group_permission_error_does_not_hide_failure(self):
        with mock.patch("test_suite.os.killpg", side_effect=PermissionError), \
                mock.patch("test_suite.subprocess.check_output", return_value=""):
            self.owner.signal_groups({123456}, 9)
        with mock.patch("test_suite.os.killpg", side_effect=PermissionError), \
                mock.patch("test_suite.subprocess.check_output", return_value="123456 Z\n"):
            self.owner.signal_groups({123456}, 9)
        with mock.patch("test_suite.os.killpg", side_effect=PermissionError), \
                mock.patch("test_suite.subprocess.check_output", return_value="123456 S\n"):
            with self.assertRaises(PermissionError):
                self.owner.signal_groups({123456}, 9)


if __name__ == "__main__":
    unittest.main()
