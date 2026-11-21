## Why

用户已经能在摘要请求前通过 PreCompact 检查或补充重点，成功压缩后缺少用本地脚本核验实际摘要的入口。PostCompact 补齐路线图 P4/P6 的压缩后检查，手动和自动压缩共用一个检查点提交边界，可以沿用现有信任、沙箱与取消机制。

## What Changes

- 新增同步命令事件 `PostCompact`。沿用 `manual`、`auto` 和 `*` 字面触发匹配，向已审查的脚本传递本次成功安装的摘要和压缩身份。
- 在成功检查点 Sync 并安装后执行检查。TUI 区分「压缩已保存，等待审查／运行检查」与独立结果；拒绝、已确认的脚本失败、超限和取消均保留压缩成功及其计量。后置事实写入或清理不确定时关闭后续接纳，同时保留已知成功的压缩。
- 复用只读禁网 runner、逐声明会话信任和既有取消入口。命令只以退出状态报告本地检查结果；固定状态和计数用于界面及恢复。
- 非交互匹配以安全状态跳过本地检查，保留原有 `stream-json` v1 压缩和 turn 输出。
- **BREAKING** 内部 Session 成功压缩记录增加必需的后置计划，按唯一当前结构替换相关固定 fixture。恢复按成功记录的原序号安装 checkpoint，再根据已保存的执行证据本地收口后置检查。已准入而无结束事实的运行保持未知结果。

## Capabilities

### New Capabilities

- `extension/post-compact-hooks` 定义压缩后观察事件、信任披露、命令输入和运行语义。

### Modified Capabilities

- `extension/command-hooks` 扩展冻结来源的事件清单以及共享匹配、顺序、信任和计数规则。
- `context/compaction` 区分新窗口安装、本地检查与面向宿主的压缩终态。
- `context/auto-compaction` 规定检查结束前的采样屏障及取消行为。
- `session/jsonl-store` 定义计划和后置运行事实的当前编码及回放位置。
- `session/resume` 在成功 checkpoint 后恢复中断检查并验证本地收口。
- `tui/hooks` 增加后置检查的审查、活动及结果投影。
- `headless/stream-control` 规定非交互检查跳过时的压缩成功与输出契约。

## Impact

- 变更涉及 `domain`、`extension/hook`、`runtime`、`session`、`protocol`、`app` 和 `tui` 的现有 Hook 或压缩边界。双 Provider checkpoint、原生 item、请求编译和稳定缓存前缀格式继续沿用现有契约。
- 首个切片限于本地同步检查。SessionStart 的上下文重注入、脚本输出控制、异步执行和插件声明仍属独立能力。
