#!/usr/bin/env python3
"""保留质量门的全部包与断言，为应用包的 race 测试采集 CPU profile。"""
import datetime
import json
import pathlib
import subprocess
import sys
import time


ROOT = pathlib.Path(__file__).resolve().parent


def run(command):
    started = time.monotonic()
    result = subprocess.run(command)
    record = {"command": command,
              "finished_at": datetime.datetime.now(datetime.timezone.utc).isoformat(),
              "duration_seconds": round(time.monotonic() - started, 3),
              "exit_code": result.returncode}
    with (ROOT / "go-commands.jsonl").open("a") as output:
        output.write(json.dumps(record) + "\n")
    return result.returncode


def main():
    args = sys.argv[1:]
    if not args or args[0] != "test":
        return run(["go", *args])
    if "-race" not in args or "./..." not in args:
        return run(["go", *args, "-count=1", "-json"])
    packages = subprocess.check_output(["go", "list", "./..."], text=True).splitlines()
    application = "easycode/internal/app"
    if application not in packages or len(packages) != len(set(packages)):
        raise RuntimeError("invalid package manifest")
    others = [package for package in packages if package != application]
    (ROOT / "race-package-manifest.json").write_text(json.dumps({
        "all_packages": packages, "profiled": [application], "remaining": others,
        "all_packages_retained": set(packages) == {application, *others},
    }, indent=2) + "\n")
    base = ["go", *[arg for arg in args if arg != "./..."], "-count=1", "-json"]
    first = run([*base, "-cpuprofile=" + str(ROOT / "app-race.pprof"),
                 "-o=/tmp/easycode-rewrite-reverify-race.test", application])
    remaining = run([*base, *others])
    return first or remaining


if __name__ == "__main__":
    sys.exit(main())
