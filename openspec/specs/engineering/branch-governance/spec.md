# engineering/branch-governance Specification

## Purpose

定义仓库分支名称的语义化强约束和一致执行入口，使新增功能、Bug 修复及其他工程变更能够从分支名称识别意图，并在本地与 GitHub 合并门禁中得到相同判定。

## Requirements

### Requirement: Branch names use an approved semantic prefix

普通开发分支名称 MUST 匹配 `^(feat|fix|refactor|docs|test|ci|build|perf|chore|revert)/[a-z0-9]+(-[a-z0-9]+)*$`。新增用户或系统能力 MUST 使用 `feat/`；Bug、安全缺陷或契约违规修复 MUST 使用 `fix/`；其余变更必须使用与实际意图一致的已登记前缀。后缀 MUST 是小写 kebab-case，禁止下划线、空后缀、连续或首尾连字符、用户名/工具前缀和仅 ticket 编号而无语义的名称。

#### Scenario: Accept a feature branch

- **WHEN** 新功能分支名为 `feat/add-session-picker`
- **THEN** 分支检查通过并识别其语义为新增功能

#### Scenario: Accept a bug-fix branch

- **WHEN** Bug 或安全修复分支名为 `fix/session-symlink-race`
- **THEN** 分支检查通过并识别其语义为修复

#### Scenario: Reject a non-semantic branch

- **WHEN** 分支名使用 `codex/`、用户名、`feature/`、下划线、裸 ticket 编号、`main` 或其他未登记格式
- **THEN** 分支检查失败并输出不含仓库 secret 的英文修复提示

### Requirement: Local hooks and CI share one branch validator

仓库 SHALL 提供单一可测试的分支校验入口，由 pre-commit、pre-push 和 GitHub Actions 复用。校验器 SHALL 优先使用显式参数或可信 CI source-branch 变量，并在本地从 symbolic ref 获取分支名；本地 detached HEAD 或无法确定 source branch 时 MUST fail closed。分支校验不得替代 `make verify`，pre-commit 仍 SHALL 执行完整质量门。

#### Scenario: Validate the pull request source branch in CI

- **WHEN** GitHub Actions 在 pull request merge ref 或 detached checkout 上运行
- **THEN** 校验器使用受信任的 pull request source branch 名称而不是临时 merge ref
- **THEN** 相同名称在本地脚本和 CI 中得到相同结果

#### Scenario: Reject a local detached HEAD

- **WHEN** 开发者在 detached HEAD 上尝试通过 pre-commit 或 pre-push 门禁且未提供明确 source branch
- **THEN** 分支校验失败并提示先切换到符合规则的语义分支

#### Scenario: Preserve the full verification gate

- **WHEN** 合法命名分支执行 pre-commit
- **THEN** 分支检查通过后继续执行不弱于 `make verify` 的完整质量门

### Requirement: Main branch requires the governance checks

主分支 SHALL 通过 GitHub ruleset 或等价保护设置禁止直接合并未通过分支命名检查和 `make verify` 的变更。仓库内文档 MUST 记录需要管理员启用的 required checks；仓库文件无法自行设置的托管平台配置不得被错误声明为已经自动生效。

#### Scenario: Block a pull request with an invalid branch name

- **WHEN** pull request source branch 不符合语义命名规则
- **THEN** required branch-governance check 失败且主分支不能合并该变更

#### Scenario: Document external ruleset activation

- **WHEN** 维护者安装或审计仓库门禁
- **THEN** 文档明确列出 required checks 和主分支保护步骤
- **THEN** 自动化测试只声称验证仓库内可执行部分
