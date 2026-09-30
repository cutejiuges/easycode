## MODIFIED Requirements

### Requirement: CLI selects one explicit headless output mode

CLI SHALL 提供 `--print` 文本模式和 `--json` JSONL 模式；任一模式都 SHALL 在提交恰好一个新 turn 并收口资源后退出。两个模式 MUST 互斥，并 SHALL 与现有 `--config <path>`、`--resume <thread-id>` 或 `--continue` 组合使用。`--resume` 与 `--continue` MUST 互斥。未指定任一 headless 模式时，现有 TUI 启动语义 MUST 保持不变，且 TUI 同样可以显式使用 `--resume` 或 `--continue`。

CLI 参数结构错误、同时指定两个 headless 模式、同时指定 `--resume` 与 `--continue`，或提供超过一个位置参数 SHALL 作为用法错误在发起配置外部副作用、创建或读取 Session、打开 Catalog 或调用 Provider 前失败，退出码为 `2`。`--version` SHALL 继续不要求 Provider 配置、Catalog 或 prompt。

#### Scenario: Run text headless mode

- **WHEN** 用户执行 `easycode --print "hello"`
- **THEN** 应用提交一次文本 turn、输出其文本结果并退出

#### Scenario: Run JSONL headless mode

- **WHEN** 用户执行 `easycode --json "hello"`
- **THEN** 应用提交一次文本 turn并只向 stdout 输出 JSONL 协议事件

#### Scenario: Continue in headless mode

- **WHEN** 用户执行 `easycode --print --continue "hello"` 或 `easycode --json --continue "hello"`
- **THEN** CLI 允许该组合，并在 Session 自动选择和恢复成功后提交恰好一个新 turn

#### Scenario: Reject conflicting modes

- **WHEN** 用户同时指定 `--print` 和 `--json`
- **THEN** CLI 在加载 Provider、创建 Session 或发起网络请求前以退出码 `2` 失败
- **THEN** 错误写入 stderr，stdout 保持为空

#### Scenario: Reject conflicting Session selectors

- **WHEN** 用户同时指定 `--resume <thread-id>` 和 `--continue`
- **THEN** CLI 在打开 Catalog、读取 journal、加载 Provider 配置或创建 Session 前以退出码 `2` 失败
- **THEN** 错误写入 stderr，stdout 保持为空

#### Scenario: Preserve interactive default

- **WHEN** 用户未指定 `--print` 或 `--json`
- **THEN** CLI 按现有行为启动 TUI，且不把位置参数或 stdin 解释为 headless prompt

