## Why

双 Provider 已经能够事务化提交原生历史并通过 `HistoryProjector` 生成只读语义视图，但历史仍只存在于进程内；退出 EasyCode 后，用户无法恢复会话，P2 的 Session、resume 与后续 headless/tool 幂等能力也没有可靠事实源。现在需要先完成一个范围受控的 JSONL Session 纵向切片，在引入 SQLite、工具和完整 headless 模式前固定原生历史持久化、恢复及 durable turn 顺序。

## What Changes

- 新增版本化、append-only 的 JSONL Session 事实源，由单 writer 分配单调序号并执行 batch append/Sync；固定包含 replay requirement 的可扩展 record envelope、per-kind payload revision、用户私有文件权限、大小边界、严格 JSON/checksum 校验和仅限尾部的修复机制。后续工具/MCP/Hook 等能力只扩展记录词汇，不改写日志内核；旧程序遇到未知且恢复必需的记录时安全拒绝，遇到声明为可选的未知记录时可有界保留并跳过。
- 新增 Provider 原生历史增量提交的持久化信封与 Provider-owned codec；当前文本切片把一次成功 sample 的用户输入与模型输出作为一个原子增量，未来工具结果可在下一次模型请求前形成 input-only 原生增量。OpenAI Responses 和 Anthropic Messages 各自编码、校验并恢复自己的原生历史，不把当前“一次文本请求+回复”固化成不可扩展的整 turn payload，也不经由 `SemanticHistoryView` 或统一扁平 Message 重建。
- **BREAKING（内部接口）**：扩展 Provider Conversation/Factory 与 Runtime 的 sample 提交接缝，使完整原生 sample commit 在 Provider 内存历史提交和对应成功终态发布前先写入并 Sync 到 Session；测试替身和应用装配需同步更新。
- 为 RuntimeEvent 绑定稳定的 session/thread/turn ID，并分离 Provider native commit、sample boundary 与 turn lifecycle boundary；当前文本 turn 只有一个“输入+输出”原生增量，未来 tool loop 可在同一 turn 下追加模型输出、input-only tool result、工具与权限记录。失败、取消、timeout、提前 EOF 和持久化失败不得把未提交 staging 变成可续写历史。
- 在字节级 Loader 之上增加强类型 replay validator/planner，校验 metadata cardinality、turn/commit/terminal 顺序和当前 payload revision 的合法状态转换。完整但未闭合的尾部 turn 作为可恢复的进程中断处理；resume 必须先 durable 追加明确失败边界，不能将其补造为成功历史或在其后直接启动新 turn。
- 新增显式 `--resume <thread-id>`：从 JSONL 校验并恢复同一 Provider/wire/model 的原生历史，通过 `HistoryProjector` 回放可见 transcript，然后继续写入同一 root thread。Session metadata 额外保存仅用于索引和诊断的规范化创建 cwd，不自动切换工作目录，也不进入 Provider request/cache fingerprint；当前配置继续提供 base URL 与 API key，Session 不保存连接 secret。
- 增加双 Provider 原生 round-trip/request golden、严格 envelope 解码、required/optional 未知记录、语义状态机、悬空 turn 收口、尾部修复、序号与权限、损坏拒绝、取消/持久化失败、恢复回放和 race 回归测试。
- 本切片不实现 SQLite 索引、`--continue`、`--print`/`--json`、session list/fork、跨 Provider/model 恢复、UsageParser、CachePlanner、ContextPlanner、工具或完整失败 transcript 持久化。

## Capabilities

### New Capabilities

- `session/jsonl-store`: 定义版本化且可扩展的 JSONL Session 事实源 envelope、replay requirement、记录种类与语义 registry、batch 顺序、Sync、权限、严格校验和尾部修复契约。
- `provider/native-history-persistence`: 定义双 Provider 每次 sample 原生 commit 的 opaque 持久化信封、无损编解码、兼容校验和恢复契约。
- `session/resume`: 定义新 root Session 的创建、创建 cwd 元数据、显式 thread resume、配置兼容性检查、悬空 turn 收口、语义 transcript 回放和后续续写行为。

### Modified Capabilities

- `runtime/chat-turn`: 成功终态增加 durable Provider sample 提交顺序与 session/thread/turn 标识，持久化失败成为唯一失败终态。
- `tui/basic-chat`: 原有仅内存对话扩展为可从显式 thread 恢复并回放文本历史，同时保持 TUI 不解析 Provider wire。

## Impact

- 主要影响 `internal/session`、`internal/provider`、`internal/provider/openai`、`internal/provider/anthropic`、`internal/runtime`、`internal/protocol`、`internal/app`、`internal/tui` 和 `cmd/easycode`，并新增对应 fixture、golden 与集成测试。
- Provider 原生数据仍封装在各自包内；Session 只保存带 family/wire/schema revision 的受控 opaque payload，Runtime/TUI 不获得具体 wire 类型。
- 默认 Session 数据位于架构约定的 `~/.easycode/sessions/YYYY/MM/DD/<thread-id>.jsonl`；测试和应用装配使用显式注入的数据根目录，不依赖全局可变状态。
- metadata 中的创建 cwd 只作为 Session/未来 SQLite 投影的事实；本 change 不据此自动 `chdir`，也不提前定义 P3 workspace root、symlink containment 或工具路径权限语义。
- 恢复仅支持记录时相同的 Provider family、wire 与 model；不匹配在网络请求前以安全英文错误拒绝。API key、Authorization、Cookie、敏感 Header、base URL 和配置文件路径不得写入 Session 或测试 fixture。
- 不新增 SQLite 或其他第三方依赖；SQLite 可重建索引与 headless 输出留给后续独立 change。
