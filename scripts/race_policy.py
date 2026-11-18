"""默认只运行关键并发路径，普通测试仍执行当前完整清单。"""

import json


def select_race(inventory, policy, goos):
    selected = {package: [] for package in inventory}
    seen = set()
    for group in policy:
        package = group["package"]
        if not group["reason"].strip() or not group["tests"]:
            raise ValueError("critical race entry requires tests and reason")
        applicable = goos in group.get("platforms", [goos])
        for name in group["tests"]:
            key = (package, name)
            if key in seen:
                raise ValueError(f"duplicate critical race entry: {package}/{name}")
            seen.add(key)
            if not applicable:
                continue
            if name not in inventory.get(package, []):
                raise ValueError(f"stale critical race entry: {package}/{name}")
            selected[package].append(name)
    if not any(selected.values()):
        raise ValueError("critical race plan is empty for current platform")
    excluded = [{"package": package, "test": name, "reason": "outside critical race paths"}
                for package, names in inventory.items() for name in names if name not in selected[package]]
    selected = {package: sorted(names) for package, names in selected.items()}
    return selected, excluded


def load_policy(path):
    return json.loads(path.read_text(encoding="utf-8"))
