## ADDED Requirements

### Requirement: Picker selection enters the existing explicit recovery boundary

用户从启动选择器确认目标后，系统 SHALL 固定该 canonical thread ID，并重新获得连续 exclusive lease，执行与显式 `--resume` 一致的读取、回放、配置/native 校验、合法修复、补偿和 writer transfer。Catalog 元数据及预览结果 MUST NOT 替代任何检查。目标 busy、删除、损坏或不兼容 MUST 明确失败，禁止选择其他 thread、创建新 Session 或转换 Provider/model。

当前配置 family/wire/model 不匹配时选择器 SHALL 显示差异并阻止提交；实际恢复仍须重新验证。恢复成功 SHALL 不自动执行 Provider sample 或旧工具，等待下一次真实用户输入。旧 `--resume`、`--continue` 和 headless 的选择及输出语义 MUST 保持不变。

#### Scenario: Resume an explicitly selected root thread
- **WHEN** 用户选中存在、兼容且空闲的 root thread
- **THEN** 恢复同一 Session/thread identity、原生历史与可见历史，保持 lease 到活动 writer 清理完成，不自动调用模型

#### Scenario: Race selection with another owner
- **WHEN** 列表/预览后另一进程取得目标 lease
- **THEN** 恢复返回 session_busy，原 journal bytes 不变，不尝试更旧会话

### Requirement: Cross-directory picker recovery requires explicit workspace acknowledgment

选择器中目标 creation cwd 与冻结的启动 cwd 不同时，系统 SHALL 在提交恢复前展示二者，并说明当前项目指令和工具 workspace 使用启动 cwd。确认默认 SHALL 为返回列表，只有明确选择继续才恢复。系统 MUST NOT chdir、自动查找其他 worktree、生成可执行 cd 命令或写剪贴板。该确认不改变既有显式 `--resume` 的跨目录契约。

#### Scenario: Decline a different-directory target
- **WHEN** 用户选中另一目录会话后按 Esc 或接受默认返回选项
- **THEN** 回到列表，未执行恢复、读取项目指令或修改 journal

#### Scenario: Confirm recovery in the current workspace
- **WHEN** 用户明确确认在启动 cwd 继续另一创建目录的会话
- **THEN** cwd 不变，项目指令和工具权限来自当前 workspace，原 metadata/native history 不被重写

### Requirement: Startup recovery exposes cancellation and failure stages honestly

恢复请求 SHALL 具有唯一接收边界。取消早于接收时 MUST 不启动恢复；接收后取消 SHALL 请求终止并等待确定结果和资源清理，不能宣称已撤销持久化变化。恢复成功与取消竞争时，未交付的活动资源 MUST 由原 owner 关闭，禁止泄漏 writer 或启动 Chat。恢复已被接收后 Esc/Ctrl+C SHALL 取消整个启动，确定结果且清理成功返回 130；结果不确定或清理失败返回 1。

发生在可能修改 journal 之前且清理成功的拒绝 SHALL 可返回原列表，保留选择和搜索，等待用户显式重试。已开始合法 repair/compensation 后的失败、Sync 不确定、poisoned 或清理失败 SHALL 结束本次启动，不发布可交互 Chat、不自动重试、不承诺 bytes 未变化。已成功提交的修复/补偿 MUST 保留，不为失败或取消回滚 append-only facts。超时 MUST NOT 放弃资源所有权，必须保持最终完成信号及清理责任。

#### Scenario: Cancel before recovery admission
- **WHEN** 请求尚未接收便被取消
- **THEN** 不读取目标、不获取 writer、不产生恢复副作用

#### Scenario: Cancel after a successful recovery before handoff
- **WHEN** 恢复已产生有效活动资源，但取消先于 Chat 接收完成
- **THEN** 原 owner 关闭 writer/Provider/工具资源并等待完成，UI 不进入 Chat

#### Scenario: Fail after a legitimate durable repair
- **WHEN** 合法修复或中断补偿已发生，后续 artifact/工具/Runtime 装配失败
- **THEN** 保留合法事实并释放资源，以失败结束启动，不将结果描述为只读拒绝或自动重复恢复

#### Scenario: Reject invalid history before repair
- **WHEN** 配置或 native 校验发现不兼容、损坏或旧开发 shape
- **THEN** 修复前拒绝，journal bytes 不变，清理成功后允许用户在列表中另行选择
