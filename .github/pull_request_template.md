## Description // 说明

Describe the purpose, main implementation, and verification results. // 请说明变更目的、主要实现和验证结果。

## Checklist // 检查清单

- [ ] The branch name follows the semantic policy, such as `feat/add-session-picker` or `fix/session-symlink-race`. // 分支名称符合语义规则，例如 `feat/add-session-picker` 或 `fix/session-symlink-race`。
- [ ] The PR title, description, and every list item use semantically equivalent `English // 中文翻译` text. // PR 标题、描述和每个列表条目均使用语义一致的 `English // 中文翻译` 文本。
- [ ] `make verify` passed, with any unavailable checks and risks documented. // 已运行 `make verify`，并记录任何无法执行的检查及风险。
- [ ] GitHub checks `branch-governance`, `verify`, and native macOS `smoke` passed on the final commit. // 最终提交的 `branch-governance`、`verify` 与原生 macOS `smoke` 三个 GitHub checks 均已通过。

> Administrators must configure all three jobs as required checks in the GitHub `main` ruleset and block direct pushes; report any protection not yet enabled. This template and workflow do not activate external repository protection automatically. // 管理员须在 GitHub `main` ruleset 中把上述三个 jobs 设为 required checks，并禁止 direct push；尚未启用的保护须明确报告。本模板与 workflow 不会自动启用仓库外部保护。
