## Why

当前九项工具、本地 Skills 和 PreToolUse 已可在修改前检查并批准操作，但成功修改后的检查结果还不能以明确授权、可恢复的方式交给模型继续处理。PostToolUse 可以复用已经验收的 Hook 来源、信任审查、只读沙箱和进程 owner，补齐“修改 → 检查 → 根据反馈继续”的纵向闭环；Skills 的权限声明、fork 和热加载则各自需要更大的生命周期设计。

## What Changes

- 在现有 user/project `hooks.json` 增加同步 command `PostToolUse`，只覆盖具有真实执行 started 和成功结果的 `fs.patch`、`fs.write`、`process.exec`；保持现有字面 matcher、冻结来源和 Darwin 只读/禁网执行边界。
- 将执行事件与反馈策略纳入声明、计划、运行身份、fingerprint 和独立信任；匹配的 PostToolUse 在原工具执行前完成信任审查，headless 不因后置事件而放行修改。
- 先冻结并 Sync 真实工具结果，再运行后置检查。Hook 失败不能撤销、改写或重报已经发生的成功；前序调用的检查收口前，后序工具、steering 和下一 sample 不得越过 exclusive 屏障。
- 增加默认 `feedback: "none"`、显式 `feedback: "model"` 的披露选择。仅明确开启并信任的结构化 `additionalContext` 可以成为有来源、有预算的模型反馈；stderr、普通 stdout、自由文本错误及命令正文不自动进入模型历史、日志或 artifact。使用调用级分阶段预算，真实结果及反馈分别计费、调用只收口一次；仅无匹配或全部 none 承诺既有输出字节不变，model 模式即使返回空对象也可能因预留缩短原预览。
- **BREAKING**：更新唯一当前 Hook/Session 契约，分开保存真实结果、后置检查事实和最终冻结反馈；恢复只使用 durable bytes 补齐原生输出，不重跑旧工具或 Hook，不新增版本字段、迁移器或兼容 reader。
- 扩展 `/hooks` 审查及工具详情，区分 Pre/Post、工具成功与后置检查状态，明确反馈会发送给模型并随会话保存；补齐双 Provider native/cache regression、固定 fixtures、PTY 和确定性故障验证。

- Post stdin 使用固定字段的结果元数据投影，不透传模型 preview 或 Exec stdout/stderr，避免夹带 artifact 引用；未启动检查以明确跳过后缀收口。前后置历史按事件/计划分开关联，durable 后置更新驱动有界详情缓存失效，旧回复不得覆盖新状态。

### Non-Goals

不包含 PostToolUseFailure、只读工具 Hook、自动格式化或工作区写权限、工具输出替换、decision/block/continue 控制、自动重试、async、HTTP/prompt/agent Hook、Skill frontmatter hooks、Plugins/MCP、热加载、持久信任或平台扩张。不修改其他隔离进程正在推进的 reasoning 展示，也不把此切片写成 P6 整体完成。

## Capabilities

### New Capabilities

- `extension/post-tool-use`：成功后置事件、执行前信任、稳定输入、显式反馈披露、错误与取消、只读执行及独立结果语义。

### Modified Capabilities

- `extension/command-hooks`：多事件冻结配置、事件隔离身份与信任、共享有界命令生命周期；保留 PreToolUse 严格拒绝子集。
- `tool/approval`：PostToolUse 的命令及披露信任先于原工具 admission，且不能替代普通批准。
- `tool/invocation-loop`：真实结果 durable 后在同一 exclusive 屏障运行后置检查，检查收口后才能继续工具组。
- `tool/result-budget`：独立反馈预留、完整块接收、累计计费及恢复不重算。
- `provider/native-history-persistence`：原结果与已冻结反馈共同形成唯一配对 output，保持原生 kind、顺序和不含反馈时的 bytes。
- `session/jsonl-store`：后置计划、开始/结束/收口事实与反馈的严格当前状态机及固定 fixtures。
- `session/resume`：保留真实成功，离线收口未完成后置检查，复用已保存反馈且零重跑。
- `tui/hooks`：事件/披露策略浏览、执行前审查和成功后检查状态。
- `tui/tool-transcript`：明确关联原工具的后置状态与已披露反馈详情。

## Impact

实施范围为 `internal/domain`、`internal/extension/hook`、`internal/tool`、`internal/tool/builtin`、`internal/runtime`、`internal/provider/{anthropic,openai}`、`internal/session`、`internal/protocol`、`internal/app` 与 `internal/tui`。沿用 `internal/executor/process` 和 `internal/extension/localfile`，不新增依赖、第二套调度器或万能 Hook manager；不增加第十项模型工具或 headless 外部事件。

方案基于 EasyCode `d7af3c9`、Claude Code 2.1.88 本地 sourcemap `a8a678c` 与 Codex 本地 `6f8bf683c` 的只读核对，不修改、依赖或复制参考工程。本 change 已按明确 apply 请求实施，验收命令与业务证据记录在 `verification.md`；相关架构、Roadmap、能力清单与踩坑记录同步更新，本次按明确归档请求同步主规格，归档核对与最终位置见 `archive.md`。
