# EasyCode Agent 工程约束

本文件适用于整个仓库，只保留每次开发都必须加载的硬约束。详细设计按需阅读：

- 总体架构：`docs/architecture/overall-architecture.md`
- 阶段计划：`docs/roadmap/product-roadmap.md`
- 踩坑记录：`docs/roadmap/pitfall-log.md`
- 架构决策：`docs/architecture/adr/`

## 1. 项目基线

- EasyCode 是 Go 1.24+ 实现的本地 coding agent，产品体验主要对齐 Claude Code，同时保留 OpenAI/Codex 原生能力。
- 用户通过 `base_url`、`api_key` 和模型配置连接服务；不实现登录、OAuth、设备码或订阅鉴权。
- 首版支持 Anthropic Messages 和 OpenAI Responses，优先实现 Responses；首版不隐式兼容 Chat Completions。
- HTTP/SSE 使用 `resty.dev/v3`，JSON 使用 Sonic，TUI 使用 Bubble Tea。
- `../claude-code-sourcemap` 与 `../codex` 仅供只读参考，不得修改、构建依赖或大段照搬。产品行为冲突时优先对齐 Claude Code；内核边界优先选择可测试、可维护且能支持双 Provider 的方案。

## 2. OpenSpec 变更流程

需求、契约和实现变更统一使用：

```text
explore -> propose -> review/confirm -> apply -> verify -> archive
```

- Explore：检查现有 changes/specs、配置、源码和文档，只读澄清问题；写 artifact 前确认范围。
- Propose：使用 `$openspec-propose <change>` 通过 CLI 创建 proposal/spec/design/tasks，禁止手工创建 change 目录；本阶段不实现代码。
- Apply：必须由后续明确请求触发 `$openspec-apply-change <change>`；读取全部 artifacts，按 tasks 实现并更新完成状态。
- 发现设计不成立或范围扩大时暂停实现，先更新 OpenSpec artifacts，不得静默降级、延期或扩张范围。
- 验收完成后同步需要沉淀的 specs，再归档 change。

以下契约变更必须先走 OpenSpec：Provider wire/capability/native item、HistoryProjector、RuntimeCommand/Event、Tool schema/result、Session/JSONL/SQLite schema、缓存 fingerprint/usage 口径、权限与 sandbox、hooks/plugins/skills/MCP manifest、Subagent thread/completion envelope、跨包公共接口或依赖方向。

纯错别字、注释、测试数据或不改变行为的内部重命名可以不创建 change，但仍须通过质量门。ADR 只保存长期架构决策的理由，不替代 OpenSpec 的需求与任务管理。

## 3. 架构硬约束

- 禁止上帝类型、上帝包、循环依赖、全局可变状态、跨层捷径和无意义抽象。
- 禁止用 `core/common/utils/manager` 承载无明确边界的杂项。
- `domain/protocol` 不依赖 HTTP、数据库、文件系统、终端、具体 Provider 或 TUI；副作用集中在 transport/store/executor/terminal 边界。
- 依赖方向保持 `domain/protocol <- provider|tool|context|session|extension|subagent <- runtime <- app/tui/cmd`，下层不得反向依赖。
- 核心协议使用强类型；未知扩展只允许受控的 `json.RawMessage`/opaque envelope，禁止无约束传播 `map[string]any`。
- 构造、getter、序列化和 UI render 中不得执行网络、磁盘、进程或数据库操作。
- 接口保持小而稳定，优先组合与依赖倒置；只有语义和生命周期一致的逻辑才能共享。
- Go 的 Template Method 只能使用“骨架持有策略接口”，禁止用 struct embed + 方法 shadow 模拟动态分派。
- 不得保留无用代码、注释掉的实现、失效 feature flag、永久兼容分支或无删除条件的临时代码。

## 4. Provider 与缓存

- Provider-native history 是请求续写、resume 和 Session 恢复的事实依据；不得先压成统一扁平 Message 再反向构造请求。
- Anthropic thinking/signature/redacted thinking 与 OpenAI reasoning/encrypted content/message phase 必须无损保存。
- Provider Kernel 由 RequestCompiler、StreamReducer、NativeHistory、ToolWireCodec、ReasoningPolicy、CachePlanner、CompactionCodec、UsageParser、HistoryProjector 等小策略组合。
- HistoryProjector 只生成单向、只读、允许有损的 SemanticHistoryView，供 token 估算、历史渲染、Hook 和 Subagent 使用；禁止反向构造 Provider 请求。
- RuntimeEvent 只用于 UI、日志和宿主投影，不能替代 native history。
- 跨 Provider 继续会话必须创建 compacted fork，禁止伪造或转换 opaque reasoning 数据。
- UsageParser 归一化为 `input_uncached/cache_read/cache_write`，并区分 known、unknown、not-applicable；指标不得直接混用两家原始字段。
- 稳定 prompt 内容、Tool Schema 和扩展目录必须显式排序、确定性序列化并产生可回归 fingerprint；时间戳、随机 ID、git/env/cwd 等动态状态不得污染稳定前缀。
- Anthropic cache marker/TTL 与 OpenAI prompt cache key/incremental request 分开实现；缓存失效必须能定位到 source revision、segment 和原因。
- 修改 Provider、上下文、工具描述、skills/plugins/MCP 或缓存逻辑时，必须补充 golden/cache regression test。

## 5. Tool、Session 与安全

- ToolCapability、Facade、Executor、Policy、Scheduler、Presenter、ResultCodec 分离；Executor 不依赖 Provider wire 或 TUI。
- 工具参数完整并进入 ToolCallReady 后才能产生副作用；每次调用具有唯一 call ID 和幂等 ledger。
- 并发执行结果仍按模型调用顺序提交，保持 tool call/output pairing；文件、命令和网络操作必须经过权限、路径和 sandbox capability。
- 大输出必须截断，完整内容按策略保存为 artifact。
- JSONL 是 Session 事实源，SQLite 只是可重建索引；Session schema 和外部协议必须版本化并提供 migration fixture。
- Session 写入保持 append-only、单调 seq、可 flush，并能修复尾部半行；后台任务、进程和 stream 必须支持取消与 shutdown。
- Transcript、diagnostic log 和 telemetry 严格分离；请求/响应正文默认不写诊断日志。
- API key、Authorization、Cookie、敏感 Header 和用户机密不得进入日志、错误、Session、snapshot 或 fixture。
- 文件操作必须防目录穿越并正确处理 symlink 和平台差异。

## 6. Go 与代码整洁

- 所有代码注释、文档注释和 TODO/FIXME 说明使用中文；导出标识符名称可以保留英文，但解释正文必须是中文。
- 对外错误码、错误消息和机器可读错误字段必须使用英文。
- 命名表达业务意图，函数保持单一抽象层级；禁止用字符串匹配驱动核心控制流。
- `context.Context` 是可取消/超时操作的第一个参数，不保存到长期结构体。
- 每个 goroutine 必须有 owner、退出条件和等待/清理路径；channel 由创建者关闭。
- 可恢复错误必须返回，不用 panic 驱动正常流程。
- 锁内禁止执行网络、磁盘、用户回调或其他不可控耗时操作。

## 7. 测试与完成标准

- 每次逻辑变更必须补测试：纯逻辑用单测，跨模块/I/O 用集成测试，Bug 修复用回归测试。
- Provider request/stream 使用 golden/fixture；SSE 覆盖随机 chunk、半包、UTF-8、取消和断线；TUI 使用固定尺寸 snapshot；Session 覆盖迁移和损坏恢复；Tool 覆盖拒绝、取消、重放和幂等。
- 禁止通过删除、跳过、弱化测试或编写无业务断言的测试掩盖失败；单测不得使用真实 API key 或默认访问外网。
- 完成前必须执行 `make verify`，其范围不得弱于 gofmt、go vet、Staticcheck、全量测试和 race test；pre-commit 必须执行同一质量门。
- 无法执行的检查必须在交付说明中列出原因和风险，不能声称全部通过。

交付前必须确认：职责和依赖边界清晰；Provider 原生语义与缓存稳定性未破坏；无重复副作用、secret 泄漏、dead code 或无用依赖；测试充分且 `make verify` 通过；相关 OpenSpec、架构、Roadmap 或踩坑文档已同步。
