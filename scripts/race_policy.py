"""只移出经过逐项审计的串行入口；新测试默认参加 race。"""

import json


def select_race(inventory, policy, goos):
    selected = {package: list(names) for package, names in inventory.items()}
    excluded, seen = [], set()
    retained = set()
    for group in policy:
        package = group["package"]
        if not group["reason"].strip() or not group["source"].strip() or not group["tests"]:
            raise ValueError("race exclusion requires tests, source and reason")
        applicable = goos in group.get("platforms", [goos])
        for name in group["tests"]:
            key = (package, name)
            if key in seen:
                raise ValueError(f"duplicate race exclusion: {package}/{name}")
            seen.add(key)
            if not applicable:
                continue
            if name not in selected.get(package, []):
                raise ValueError(f"stale race exclusion: {package}/{name}")
            selected[package].remove(name)
            excluded.append({"package": package, "test": name, "source": group["source"],
                             "reason": group["reason"], "retained_race": group["retained_race"]})
        if applicable:
            retained.update((item["package"], item["test"]) for item in group["retained_race"])
    for package, name in sorted(retained):
        if name not in selected.get(package, []):
            raise ValueError(f"missing retained race coverage: {package}/{name}")
    return selected, excluded


def load_policy(path):
    return json.loads(path.read_text(encoding="utf-8"))
