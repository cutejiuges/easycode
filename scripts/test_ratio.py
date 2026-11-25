#!/usr/bin/env python3
"""统计完整源码快照的测试维护成本，不根据宿主平台或 Git 增量筛选。"""

import json
from pathlib import Path
import re
import subprocess
import sys


def source_paths(root):
    output = subprocess.check_output(
        ["git", "ls-files", "--cached", "--others", "--exclude-standard", "-z"], cwd=root)
    return sorted({Path(name.decode()) for name in output.split(b"\0") if name})


def measure(root, paths):
    report = {"production": 0, "tests": 0, "scripts": 0,
              "production_nonblank": 0, "tests_nonblank": 0,
              "fixture_files": 0, "fixture_bytes": 0, "packages": {}}
    for relative in paths:
        path = root / relative
        if not path.is_file() or "vendor" in relative.parts:
            continue
        fixture = "testdata" in relative.parts
        support = bool({"testsupport", "testutil"}.intersection(relative.parts))
        go = relative.suffix == ".go"
        script = relative.suffix in (".py", ".sh")
        if not (fixture or go or script):
            continue
        if path.is_symlink():
            raise ValueError(f"source accounting requires a regular file: {relative}")
        content = path.read_bytes()
        if fixture:
            report["fixture_files"] += 1
            report["fixture_bytes"] += len(content)
            continue
        lines = content.count(b"\n") + int(bool(content) and not content.endswith(b"\n"))
        if script:
            if relative.parts[0] != "scripts" and not support:
                raise ValueError(f"unclassified script source: {relative}")
            report["scripts"] += lines
            continue
        test = relative.name.endswith("_test.go") or support
        if not test and (re.search(r"(^|_)test(s|ing)?(_|$)", relative.stem) or
                         any(part in ("test", "tests") for part in relative.parts) or
                         re.search(rb'(?m)^\s*(?:import\s+)?(?:[.\w]+\s+)?"testing(?:/[^"]*)?"', content)):
            raise ValueError(f"test-only Go source must use _test.go or testsupport/testutil: {relative}")
        if not test and re.search(rb"(?m)^// Code generated .* DO NOT EDIT\.$", content):
            continue
        if not test and relative.parts[0] not in ("cmd", "internal"):
            raise ValueError(f"unclassified Go source: {relative}")
        kind = "tests" if test else "production"
        report[kind] += lines
        report[kind + "_nonblank"] += sum(bool(line.strip()) for line in content.splitlines())
        package = relative.parent.as_posix()
        report["packages"].setdefault(package, {"production": 0, "tests": 0})[kind] += lines
    return report


def violations(report, policy):
    if set(policy) != {"minimum_production_per_test", "maximum_test_script_lines"}:
        raise ValueError("test size policy requires a ratio and script budget")
    ratio = policy["minimum_production_per_test"]
    budget = policy["maximum_test_script_lines"]
    if not isinstance(ratio, dict) or set(ratio) != {"production", "tests"} or any(
            type(value) is not int or value <= 0 for value in (*ratio.values(), budget)):
        raise ValueError("test size policy requires positive integer ratio terms and script budget")
    errors = []
    if report["production"] == 0:
        errors.append("production source inventory is empty")
    maximum = report["production"] * ratio["tests"] // ratio["production"]
    if report["tests"] > maximum:
        errors.append(f"test ratio below {ratio['production']}:{ratio['tests']}: "
                      f"maximum tests={maximum}, excess={report['tests'] - maximum}")
    if report["scripts"] > budget:
        errors.append(f"test script budget exceeded: maximum={budget}, excess={report['scripts'] - budget}")
    return errors


def inspect(root):
    policy = json.loads((root / "scripts/test_size_policy.json").read_text(encoding="utf-8"))
    report = measure(root, source_paths(root))
    report["policy"] = policy
    report["errors"] = violations(report, policy)
    return report


def main():
    root = Path(__file__).resolve().parent.parent
    try:
        report = inspect(root)
        ratio = report["production"] / report["tests"] if report["tests"] else float("inf")
        required = report["policy"]["minimum_production_per_test"]
        print(f"Go production={report['production']} tests={report['tests']} ratio={ratio:.4f}:1 "
              f"required={required['production']}:{required['tests']}")
        print(f"Test scripts={report['scripts']} budget={report['policy']['maximum_test_script_lines']}; "
              f"fixtures={report['fixture_files']} files/{report['fixture_bytes']} bytes")
        for package, values in sorted(report["packages"].items()):
            print(f"  {package}: production={values['production']} tests={values['tests']}")
        for error in report["errors"]:
            print(error, file=sys.stderr)
        return int(bool(report["errors"]))
    except (OSError, ValueError, subprocess.SubprocessError) as error:
        print(f"test size check failed: {error}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
