## MODIFIED Requirements

### Requirement: Local hooks and CI share one branch validator

仓库 SHALL 提供单一可测试的分支校验入口，由 pre-commit 和 GitHub Actions 复用；仓库 MUST 移除重复的 pre-push hook，不增加同职责的转发入口。校验器 SHALL 优先使用显式参数或可信 CI source-branch 变量，并在本地从 symbolic ref 获取分支名；本地 detached HEAD 或无法确定 source branch 时 MUST fail closed。分支校验不得替代 `make verify`，pre-commit 仍 SHALL 依次执行完整 `make verify` 与原生 `make smoke`，任一失败均阻止提交。安装入口 MUST 仅引用现存 hook，并保留仓库自己的 hooksPath 设置。

#### Scenario: Validate the pull request source branch in CI

- **WHEN** GitHub Actions 在 pull request merge ref 或 detached checkout 上运行
- **THEN** 校验器使用受信任的 pull request source branch 名称而不是临时 merge ref
- **THEN** 相同名称在本地脚本和 CI 中得到相同结果

#### Scenario: Reject a local detached HEAD

- **WHEN** 开发者在 detached HEAD 上尝试通过 pre-commit 门禁且未提供明确 source branch
- **THEN** 分支校验失败并提示先切换到符合规则的语义分支

#### Scenario: Preserve the full verification gate

- **WHEN** 合法命名分支执行 pre-commit
- **THEN** 分支检查通过后依次执行一次完整 `make verify` 和 `make smoke`；前一步失败不执行后一步，任一失败均阻止提交
- **THEN** 随后的 push 不通过仓库 pre-push 再次运行相同验证

#### Scenario: Keep CI enforcement after removing the push hook

- **WHEN** 提交后分支被改成不合法名称并发起 pull request
- **THEN** CI 分支检查仍拒绝该名称，即使本地 push 不再执行 hook
- **THEN** 仓库不声称已运行被移除的本地 push 检查，也不通过安装脚本清理用户自己的外部 hooks
