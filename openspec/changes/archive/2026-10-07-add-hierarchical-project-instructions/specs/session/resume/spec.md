## ADDED Requirements

### Requirement: Resume uses current startup project instructions as external context

新进程通过 `--resume` 或 `--continue` 恢复 thread 时，应用 SHALL 使用当前进程启动工作目录发现的单一项目指令快照编译后续请求。该快照 MUST 独立于 Session `creation_cwd`、Catalog row、JSONL records、恢复出的 Provider native history和 TUI transcript；应用不得自动切换到创建目录读取旧项目指令，也不得把当前绝对 cwd 或项目根直接注入请求或 cache fingerprint。

当前快照与原进程快照相同时，恢复后请求 MUST 满足 native history 恢复等价契约。当前快照不同或不存在时，应用 SHALL 保持既有 records 和 restored native history 不变，只更新请求时的临时项目上下文及其内容派生 cache 输入；本次变化不得成为 Provider 配置不兼容、Session migration 或 journal 重写的理由。

#### Scenario: Resume from a different cwd with the same instruction snapshot

- **WHEN** 用户从不同绝对 cwd 显式恢复 thread，但当前发现得到相同的规范化项目指令快照
- **THEN** 应用不 `chdir`、不修改 Session metadata，并继续使用恢复出的 native history
- **THEN** 绝对 cwd 差异不改变请求 canonical bytes 或 cache fingerprint

#### Scenario: Resume with changed project instructions

- **WHEN** 用户恢复兼容 thread，而当前启动上下文发现的项目指令与原进程不同
- **THEN** 应用不改写既有 JSONL records、不伪造 native history，也不把变化判为 Provider 不兼容
- **THEN** 下一请求只使用当前快照生成一个临时项目指令上下文

#### Scenario: Fail discovery before resumed network activity

- **WHEN** 当前启动上下文的项目指令发现因不安全文件、读取失败或非法 UTF-8 失败
- **THEN** resume 或 continue 在进入可提交状态和发起 Provider 请求前失败
- **THEN** 目标 journal bytes 与恢复出的 native history 保持不变
