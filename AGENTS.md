# EasyCode Agent 工程约束

本文件适用于仓库根目录及所有子目录。所有参与 EasyCode 设计、实现、测试、评审和文档工作的模型或开发者都必须遵守。

## 1. 项目目标

EasyCode 是一个本地优先的 coding agent：

- 产品能力和交互体验主要对齐 Claude Code。
- 同时完整保留 OpenAI/Codex 模式的重要能力。
- 用户通过 `base_url`、`api_key` 和模型配置连接服务商。
- 不实现账号登录、OAuth、设备码、订阅鉴权等能力。
- 首版完整支持 Anthropic Messages 和 OpenAI Responses。
- 工程使用 Go 1.24+；HTTP/SSE 使用 `resty.dev/v3`，JSON 使用 `github.com/bytedance/sonic`，TUI 使用 `github.com/charmbracelet/bubbletea`。

总体架构和阶段计划分别见：

- `docs/architecture/overall-architecture.md`
- `docs/roadmap/product-roadmap.md`
- `docs/roadmap/pitfall-log.md`

涉及架构、provider、缓存、session 或模块边界的实现必须先阅读对应文档。

## 2. 参考工程

### 2.1 Claude Code

路径：`../claude-code-sourcemap`

- 当前为 `@anthropic-ai/claude-code@2.1.88` 的 sourcemap 还原工程。
- 用于参考 Claude Code 的产品体验、REPL、tools、subagent、hooks、plugins、skills、上下文、session 和 TUI。
- 该工程不是官方完整源码，不得盲目复制其文件组织、重复逻辑或内部实验代码。

### 2.2 Codex

路径：`../codex`

- 用于参考 Rust workspace、OpenAI Responses、事件协议、工具执行、权限/sandbox、session rollout、subagent 和 Ratatui。
- Codex 的 Responses-only 设计不能直接代替 Anthropic Provider Kernel。

### 2.3 使用限制

- 两个参考工程默认只读，不得为了实现 EasyCode 修改它们。
- EasyCode 不得在构建时依赖参考工程源码。
- 可以借鉴设计和行为，不得大段复制不理解的代码。
- 引入参考实现中的机制前，必须说明它适合 EasyCode 的原因、边界和测试方法。
- 产品行为冲突时优先对齐 Claude Code；内核边界优先选择可测试、可维护、能同时支持两个 provider 的方案。

## 3. 不可违反的架构约束

### 3.1 禁止反模式

禁止提交或保留以下设计与代码：

- 上帝类：单一类型同时承担协议、业务编排、存储、权限、UI 或多种无关职责。
- 上帝包：将大部分逻辑堆进 `core`、`common`、`utils`、`manager` 等无明确边界的包。
- 循环依赖：package、模块职责或运行时回调形成依赖环。
- 薄而有损的 provider adapter：把两种 provider 压成通用扁平 Message 后再反向构造请求。
- UI 侵入核心：Provider、ToolExecutor、ContextManager 不得依赖 TUI widget。
- 跨层捷径：通过全局单例、静态可变状态或随意 callback 绕过协议和依赖方向。
- 复制粘贴式 provider 分支：重复逻辑应抽取到真正共享的策略或纯函数，不能散落 `if anthropic` / `if openai`。
- 无约束的 `map[string]any` 传播：核心协议使用强类型；未知扩展仅使用受控的 `json.RawMessage`/opaque envelope。
- 隐式副作用：构造、getter、序列化或 UI render 中不得执行网络、文件、进程或数据库操作。
- 永久兼容分支、无引用代码、无意义抽象和已失效 feature flag。

发现上述问题时，当前任务必须先修正或明确阻止其进入代码库，不能以“后续重构”为理由忽略。

### 3.2 核心代码必须纯净

- `domain`/`protocol` 不依赖 HTTP、数据库、终端、具体 provider SDK 或文件系统实现。
- Context 规划、cache fingerprint、权限判断、stream state transition 尽量实现为纯函数。
- 副作用集中在 transport、store、executor、terminal 等边界模块。
- 核心类型表达领域语义，不使用 UI 字符串或 provider JSON 充当内部状态。
- 错误分类明确，不能用字符串匹配驱动核心控制流。

### 3.3 依赖方向

遵循总体架构文档中的依赖图：

- domain/protocol 位于底层。
- provider、tools、context、session、extensions、subagents 依赖底层领域类型。
- runtime 负责编排这些模块。
- TUI/CLI 通过 SessionService 和 command/event 协议使用 runtime。
- 下层不得反向依赖 runtime、TUI 或 CLI。

新增跨模块依赖前必须确认不会形成环，并优先通过稳定的领域接口或事件协议通信。

## 4. Provider 与缓存约束

### 4.1 Provider 原生语义

- Anthropic thinking/signature/redacted thinking 必须原样保存和恢复。
- OpenAI reasoning summary/raw/encrypted content、message phase 必须原样保存和恢复。
- Provider-native item 是下一次请求和 resume 的依据。
- RuntimeEvent 用于 UI 和投影，不能反向替代 native history。
- Anthropic 和 OpenAI 分别拥有 RequestCompiler、StreamReducer、ToolWireCodec、ReasoningPolicy、CachePlanner 和 CompactionCodec。
- 跨 provider 继续会话必须创建 compacted fork，禁止伪造或转换 opaque reasoning 数据。

### 4.2 缓存是一级目标

任何会改变模型请求的逻辑都必须评估缓存影响：

- system/developer instructions
- tool schema、名称、描述和顺序
- plugin/skill/MCP catalog
- context ordering 和 world-state 注入
- compaction 和 rehydrate
- provider headers/beta/cache marker
- model/provider/capability 切换

硬性规则：

- 稳定集合必须显式排序，不得依赖 HashMap 或文件系统枚举顺序。
- 最终 wire 使用 canonical serialization 和可回归的 segment fingerprint。
- 时间戳、随机 ID、实时 git/env/cwd 等动态内容不得污染稳定前缀。
- Anthropic cache marker/TTL 和 OpenAI prompt cache key/incremental request 分开实现。
- 缓存失效必须能定位到 source revision、segment 和原因。
- 第三方服务未返回 cache usage 时使用 unknown，不能当作零。

修改缓存敏感代码必须补充或更新 golden/cache regression test。

## 5. 封装、抽象与多态

- 遵循单一职责、依赖倒置、接口隔离和组合优于继承。
- 重复逻辑必须抽取复用，但只有语义和生命周期都一致的逻辑才能共享。
- Anthropic/OpenAI 恰好长得相似的 JSON 不代表属于同一抽象。
- 接口应小而稳定；禁止为一个实现创建多层空壳接口。
- 使用强类型 ID、状态和结果，避免裸字符串在跨模块传播。
- 公开 API 必须表达错误、取消、未知/缺失信息，不能用默认值掩盖状态。

推荐按需使用以下模式：

- Template Method：共享 turn 生命周期。
- Strategy：provider request/stream/cache/compaction。
- State：stream reducer 和 turn 状态机。
- Command：runtime command 和 tool invocation。
- Observer/Event Bus：RuntimeEvent 投影。
- Repository：session store/index。
- Decorator/Pipeline：hooks、policy、telemetry。
- Adapter：Claude/Codex plugin manifest 导入。

设计模式用于隔离变化，不得为了模式数量制造复杂度。

## 6. Tool 约束

- ToolCapability、ToolFacade、ToolExecutor、ToolPolicy、ToolScheduler、ToolPresenter 和 ToolResultCodec 分离。
- ToolExecutor 不知道 provider wire 和 TUI。
- 同一能力允许在 Anthropic/OpenAI 下使用不同 tool schema、名称和提示词。
- 流式参数 decoder 可以 provider-specific；共享的是最终能力和执行器。
- 工具只有在参数完整并进入 ToolCallReady 后才能产生副作用。
- 每次调用具有唯一 call ID 和 execution ledger，重试/resume 不得重复副作用。
- 并发工具可以并行执行，但结果必须保持模型调用顺序和 call/output pairing。
- 文件、命令和网络工具必须经过权限、路径和 sandbox 策略。
- 大输出必须截断，完整内容按策略保存为 artifact。

## 7. Session、日志与安全

- JSONL 是 session 事实源，SQLite 是可重建索引。
- Session schema 和外部协议必须版本化，并有 migration/fixture。
- Transcript、diagnostic log 和 telemetry 严格分离。
- API key、Authorization、cookie、敏感 header 不得进入日志、错误、session、snapshot 或 fixture。
- 请求/响应正文默认不写调试日志；显式诊断也必须脱敏。
- Session 写入保持 append-only、单调 seq、可 flush、可修复半行。
- 文件路径必须防目录穿越并正确处理 symlink 和平台差异。
- 所有后台任务、进程和 stream 都必须具有取消和 shutdown 路径。

## 8. 代码整洁要求

- 全局代码注释、文档注释、TODO/FIXME 说明统一使用中文；Go 导出标识符的文档注释可以保留标识符英文名称，但解释正文必须使用中文。
- 对外暴露的错误码、错误消息和可机器解析的错误字段必须使用英文，保持跨语言客户端稳定；内部中文注释不得混入 error code。
- 命名表达意图，避免 `data`、`manager`、`helper`、`misc` 等模糊名称。
- 函数保持单一抽象层级；复杂条件提取为有业务含义的函数或类型。
- 注释解释“为什么”和约束，不重复代码表面行为。
- 公共 API、协议、状态机和不直观不变量需要文档。
- 禁止留下无用代码、注释掉的实现、未使用依赖和无法触达分支。
- 逻辑被替代后立即删除旧实现；迁移期兼容代码必须有明确删除条件。
- 避免无边界的布尔参数；使用枚举或配置值对象表达模式。
- 避免超长参数列表；相关数据使用职责明确的输入对象。
- 避免一个函数同时做解析、校验、执行、持久化和渲染。
- 对重复代码进行语义审查后复用，不进行机械 DRY。
- `context.Context` 作为需要取消/超时操作的第一个参数，不保存进长期结构体。
- 每个 goroutine 必须有明确 owner、退出条件和等待/清理路径，禁止 goroutine 或 channel 泄漏。
- channel 的创建者负责关闭；接收方不得关闭不属于自己的 channel。
- 可恢复错误必须返回，不使用 panic 驱动正常控制流。
- 锁内不执行网络、磁盘、用户回调或其他不可控耗时操作。

## 9. 测试硬约束

每次逻辑变更必须补充充分测试：

- 纯逻辑和状态转换：单元测试。
- 跨模块、I/O、session、tool、hook 行为：集成测试。
- Provider request/stream：golden 和 fixture 测试。
- SSE：随机 chunk boundary、半包、UTF-8、断线和取消测试。
- Cache：canonical request、segment fingerprint 和失效矩阵测试。
- Bug 修复：能先失败、修复后通过的回归测试。
- TUI：固定终端尺寸 snapshot 和按键路由测试。
- Session schema：旧版本 fixture、迁移和损坏恢复测试。
- Tool 副作用：权限拒绝、取消、重放和幂等测试。

禁止：

- 仅为覆盖率编写没有断言业务结果的测试。
- 通过删除、跳过或弱化测试掩盖失败。
- 使用真实 API key 或默认访问外部网络的单元测试。
- 让测试依赖执行顺序、真实时钟、随机文件枚举顺序或不可控环境。

实现完成后必须运行：

```text
make verify
```

`make verify` 必须覆盖 `gofmt -l .`、`go vet ./...`、`go test ./...` 和 `go test -race ./...`。不得削弱 `.githooks/pre-commit` 的检查范围或绕过失败结果；新增必要检查时，应同步更新 Makefile 和提交钩子的测试约束。

如果命令因阶段尚未初始化、平台限制或外部依赖无法运行，必须在交付说明中明确列出未运行项、原因和风险，不能声称测试全部通过。

## 10. 每次变更的强制流程

### 10.1 开始前

1. 阅读根目录 `AGENTS.md` 和相关架构/roadmap 文档。
2. 检查工作区现状，保留用户已有改动。
3. 明确本次变更所属模块、依赖方向和阶段验收标准。
4. 识别 provider、缓存、session、权限和兼容性影响。

### 10.2 实现中

1. 先建立或更新测试，再完成最小正确实现。
2. 保持职责集中，发现上帝类/包倾向立即拆分。
3. 发现重复逻辑时判断其语义是否真正一致，再决定抽象。
4. 不通过临时全局状态、无类型 JSON 或跨层调用绕过设计。
5. 删除被替代的旧代码和无用 import/dependency。
6. 新发现的系统性问题记录到 `docs/roadmap/pitfall-log.md`。

### 10.3 完成后自检

每次实现完成后必须逐项检查并修正：

- 是否出现上帝类、上帝包或职责混杂？
- 是否新增循环依赖、反向依赖或跨层捷径？
- 是否有可复用但仍重复的逻辑？是否存在错误抽象？
- Provider 原生数据是否无损？是否泄漏到 UI？
- Tool、Context、Session、TUI 边界是否保持清晰？
- 稳定前缀、排序和 cache fingerprint 是否受到影响？
- 是否可能重复执行副作用？
- 是否有无用代码、旧实现、未使用依赖或永久 TODO？
- 是否可能泄漏 secret 或用户代码内容？
- 单元、集成、golden、缓存和 TUI 测试是否充分？
- 全量格式化、lint 和测试是否通过？
- 架构、roadmap、ADR 或踩坑文档是否需要同步？

未完成上述自检不得宣称任务完成。

## 11. Definition of Done

一个逻辑变更只有同时满足以下条件才算完成：

1. 功能行为符合需求和阶段验收标准。
2. 架构边界清晰，没有本文禁止的反模式。
3. Provider 原生语义和缓存稳定性未被破坏。
4. 相关单元、集成、golden、回归或 TUI 测试已补充。
5. 全量适用测试、格式化和 lint 通过。
6. 无 dead code、无无用依赖、无未说明的兼容分支。
7. 安全和 secret 脱敏检查通过。
8. 相关文档、ADR 和踩坑记录已同步。
