## 中文说明 / Chinese Description

请说明变更目的、主要实现和验证结果。

## English Description / 英文说明

Describe the purpose, main implementation, and verification results.

## Checklist / 检查清单

- [ ] 分支名称符合语义规则，例如 `feat/add-session-picker` 或 `fix/session-symlink-race`。
- [ ] Branch name follows the semantic policy, such as `feat/add-session-picker` or `fix/session-symlink-race`.
- [ ] PR 标题与描述同时提供语义一致的中文和英文版本。
- [ ] The PR title and description provide semantically equivalent Chinese and English versions.
- [ ] 已运行 `make verify`，并记录任何无法执行的检查及风险。
- [ ] `make verify` passed, with any unavailable checks and risks documented.
- [ ] `branch-governance` 与 `verify` 两个 GitHub checks 均已通过。
- [ ] Both GitHub checks, `branch-governance` and `verify`, passed.

> 管理员须在 GitHub `main` ruleset 中把上述两个 jobs 设为 required checks，并禁止 direct push；本模板与 workflow 不会自动启用仓库外部保护。
> Administrators must configure both jobs as required checks in the GitHub `main` ruleset and block direct pushes; this template and workflow do not activate external repository protection automatically.
