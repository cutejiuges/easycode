## Context

动机见 [proposal.md](proposal.md)。本设计基于 2026-11-05 的实际 checkout：没有活动 change，现有八能力 catalog、双 Provider 多 sample 工具循环、当前无版本 Session、自动压缩、同轮 steering、本地 Skills、会话选择器和流式 Markdown 均已有消费者与测试。Roadmap 顶部的“P2 进行中”和 README 的部分“尚未实现”描述滞后，因此选型以源码和现行 specs 为准，不把阶段标题当成能力盘点。

关键接点：

| 接点 | 当前行为 | 本次调整 |
|---|---|---|
| `internal/tool/catalog.go`、`builtin/specs.go` | 校验八能力真实组件并生成稳定两家 facade | 增加真实提问执行器与两个 structured facade |
| `internal/tool/tool.go` 的 `CapabilityID.Exclusive()` | 同时被调度、approval validator 和恢复 grammar 当作副作用判据 | 分离独占调度与需要批准，审计全部生产调用点 |
| `internal/runtime/tool_scheduler.go`、`exclusive.go` | 连续只读批次、独占屏障、骨架持有策略、按序 durable | 复用 started/result 骨架，独立提问策略，不进入 executeApproved 的批准分支 |
| `internal/runtime/agent_loop.go`、`approval.go` | 同一 owner 接收控制；turn worker 等待一个请求 | 同一 owner 路由回答，独立实例状态，不复用 grant 或 approval response |
| `internal/session/replay.go` | exclusive 调用通常要求 allow，且恢复不得重跑 | 显式区分 Question 与需批准的三能力，保留既有副作用约束 |
| `internal/runtime/result_budget.go` | owner 按 index 冻结 preview，通用 finalization 可缩短部分工具结果 | 回答在接收前预检，不能通用截断为部分成功决定 |
| `internal/tui/model.go`、composer 与 transcript 查询 | modal 焦点隔离、字素编辑、安全显示、有界分页 | 独立问卷状态机，复用编辑原语与历史查询 |

参考根按主 checkout `/Users/cutejiuge/IdeaProjects/easycode` 的同级目录解析；本 worktree 的直接 `../` 下没有两参考仓库。参考目录只读，未修改、构建或成为 EasyCode 依赖。

### 参考证据与取舍

Claude Code 参考为 sourcemap 还原的 **2.1.88**，不是完整官方源码。Codex 实际本地 HEAD 为 **6f8bf683c**，与总体架构文档记录的旧 `c248f6d48b` 不同；本方案明确使用所读快照，不声称两者相同。

| 参考源码（相对各自参考根） | 已观察行为 | 采用与调整 |
|---|---|---|
| Claude `restored-src/src/tools/AskUserQuestionTool/AskUserQuestionTool.tsx`、`prompt.ts` | 1–4 题、2–4 选项、multiSelect、自动 Other、推荐项；输入还允许宿主 answers/annotations，工具复用权限 UI，标记 concurrency-safe | 采用多题/多选/其他/说明的体验；模型输入不允许 answers；独立回答控制，不把交互等同授权 |
| Claude `restored-src/src/components/permissions/AskUserQuestionPermissionRequest/` 下的 `QuestionView.tsx`、`use-multiple-choice-state.ts`、`SubmitQuestionsView.tsx` | 每题保留选择和文字，支持前后导航与 Review your answers；未答题警告；单题单选可省去复核页 | 采用逐题草稿和复核，所有提交都经过明确复核；未答必须明确跳过，不靠焦点代答 |
| Codex `codex-rs/protocol/src/request_user_input.rs`、`codex-rs/core/src/tools/handlers/request_user_input_spec.rs` | 题目有稳定 id，答案按 id 关联，工具 schema 与宿主 response 分离 | 采用题目 ID；回答采用原题顺序的 typed slice，避免正文作 key、逗号拼接和无约束 map |
| Codex `codex-rs/core/src/tools/handlers/request_user_input.rs`、`codex-rs/core/src/session/mod.rs` 的 request/notify 路径及 handler tests | 原生 call_id/turn 关联；当前 Plan 与普通模式有 blocking 差异；等待响应后返回工具结果 | 采用请求/回答关联与原生 result；本切片固定阻塞当前工具位置，不引入 mode、后台问题或自动代答 |
| Codex `codex-rs/tui/src/bottom_pane/request_user_input/mod.rs` 及 render tests/snapshots | 选项和 notes 分离、题间导航、未答确认、窄屏；当前还含自动 resolution 分支 | 采用说明编辑和窄屏验证；不采用计时代答或实验开关 |

Claude 的 concurrency-safe 和 Codex 的非阻塞扩展均不能直接套入 EasyCode：当前 sample 中后序 Write/Exec 的候选已由模型生成，用户作答不能改写这些参数。提问屏障只保证时序；新决定必须由下一 sample 产生新的调用。工具说明明确提示模型在需要答案决定参数时单独提问，不把问卷当作批准或运行计划的替代物。

## Goals / Non-Goals

**Goals:**

- 一条清晰的 `问题 ready -> started -> 等待 -> 回答 result -> native outputs -> 继续采样` 路径，完整复用单 writer、预算、有序提交和清理 owner。
- 共享问题语义，保留两家原生 function/tool_use 形状；UI、native history、工具授权各有独立事实边界。
- 回答 admission 与 durable resolution 分离，重复、过期、取消和崩溃都能给出确定结果或明确不确定状态。
- 新逻辑进入小文件与封闭 typed union；只为当前真实消费者增加小接口，不引入通用问卷平台。

**Non-Goals:**

- 不增加 Plan Mode、异步/后台问卷、自动超时代答、图片/HTML preview、secret 输入模式、Subagent 转发或 MCP elicitation。
- 不改变现有外部 headless JSONL 命令格式，不从 stdin 另建交互通道；不创建全局历史或持久化未提交草稿。
- 不升级内部格式版本，不保留第二 catalog/reader，不借此完成全部 P5/P6/P7。

## Decisions

### 1. 选型与范围

选择 AskUser，因为它直接补齐“模型发现不确定性 -> 用户决定 -> 同一任务继续”的交互链。后台进程需要受控 handle、PTY/stdin、超时、脱离进程约束与唯一 shutdown owner；Hook/MCP 需要外部命令/连接、trust 和新的 manifest 生命周期；跨 Provider fork 需要新 thread 来源与压缩分叉。它们均有价值，但当前提问可以利用已验证的工具与宿主机制取得更短的用户反馈链。

高级 reasoning 展示可改善透明度，但不能替代缺失的用户决策闭环；因此本切片先补提问，不同时扩展 reasoning 配置或消息 phase UI。

### 2. 强类型输入与回答

共享 QuestionID、QuestionInput、QuestionOption、QuestionAnswer、QuestionAnswerSet 位于 `internal/protocol/question_values.go`，以私有不可变编码持有；`internal/tool/question.go` 使用类型别名复用并组合 QuestionInvocation，`question_result.go` 持有结果关联与完整 preview。该放置避免 protocol 反向依赖 tool，语义契约不变。题目 fingerprint 由 `QuestionInput.Fingerprint()` 生成并严格校验；构造、decoder、validator 全覆盖，不接受 any。slice/bytes getter 深拷贝。完整字段、数量与大小约束见 tool/user-question spec。

两家 facade 分别命名 AskUserQuestion / request_user_input，使用同一有界语义 schema：questions 数组，每题 id/header/question/options/multi_select；id 是题目关联，不是 Session InputID。两家均不暴露 answers、annotations、isBlocking、autoResolutionMs、权限或 mode 参数。兼容参考工具的完整外部 schema 不在范围内。

回答以原题序的数组表达，每项包含 question_id、selected_options、text、skipped；选择使用选项索引，不把 label 或“推荐”文案作为控制信号。单选/多选和补充文字共用一种当前答案形状，validator 决定合法组合；不按逗号拆分用户答案。空白判断不改写有效正文。Other 是 UI 入口，不是模型生成的一条选项。

最终 preview 为确定性编码的完整回答，保留选中标签、原索引、说明及跳过信息；历史详情从原 ready 输入加冻结结果还原展示。typed result 的关联/计数 metadata 必须与 preview 校验一致。回答正文不额外复制到 diagnostic、telemetry 或 Catalog；若内存 result 需要 typed answers，由当前 strict decoder 从唯一冻结 preview 重建，不在磁盘另存第二份答案正文。

### 3. 真正复用工具执行，分开调度与授权

保留 `Exclusive()` 表示调度屏障，增加当前 capability 的 `RequiresApproval()` 判定，只有 Patch/Write/Exec 为真；两者使用常量/switch，不建可变 registry。Question Exclusive=true、RequiresApproval=false。逐项审计 replay.go、runtime recovery、approval validator、readonly material 和 scheduler 中全部 Exclusive 调用，不机械全局替换；本次审批相关校验也有回归。

新增实际 `tool.QuestionExecutor` 与最小 `tool.QuestionResponder` 接口。builtin 的纯内存构造器返回真实执行器：Execute 接收完整 QuestionInvocation 和显式 responder，验证回答、构造完整结果；不依赖 Provider/TUI、不自行开 goroutine。app 将同一执行器装配给 catalog 与 Runtime。Runtime 为当前 invocation 组合带 turn、emit、allowance 的 responder 策略，拥有等待和控制关联；没有 Runtime/工具构造循环、空 executor 或虚构 sandbox 候选。

有序 scheduler 对到达的 Question 使用独立控制策略，复用 `executeExclusive` 的 started admission、exactly-once 执行接收、结果身份验证与持久化骨架；只有需要审批的能力进入现有 executeApproved。能力无文件、进程、网络操作，policy 的真实结论是交互可用或明确拒绝；不产生 approval_decided。多个提问按模型 index 串行，前后都不可越过。

### 4. 单 owner 接收与取消线性化

新增 `protocol/user_question.go`、`runtime/user_question.go`、`agent_loop.go` 和 `tui/user_questions.go` 等小文件。命令拟名 resolve_user_question，事件拟名 user_question_requested / user_question_resolved；每个新增当前 kind 同时具备 typed constructor、strict decoder、validator、真实宿主消费者与测试。

每次 live 请求有独立 UUIDv7 question request ID；响应携带 expected TurnID、invocation ID、request ID、question fingerprint 和完整 AnswerSet。fingerprint 来源为固定 codec 编码的 capability/完整题目，不使用时钟、cwd、UI 或随机 ID；不进入稳定 catalog。UUID 生成使用显式执行点，New 构造器不访问外部资源。

pending 请求是 Runtime 实例状态，最多一个，没有后台服务、第二 FIFO 或新的长期 owner。已有 turn worker 等待完成信号，已有 AgentLoop owner 接收 response/stop/shutdown。锁只保护有界内存状态转换和 close(done)；编码预检在锁外运行，再以同一请求/generation 复核并接受；不在锁内调用 Provider、emit、Sync、Close、用户回调或阻塞 channel 操作。

| 先发生的线性化事件 | 后续结果 |
|---|---|
| response admission 前观察到 context/stop 取消 | 不接受答案，保持无答案写入，按调用位置取消配对 |
| response 已接收 | turn owner 持有不可撤回的回答义务，先等 result 的确定提交结果；随后 stop 可阻止后序工具/采样 |
| result Sync 成功 | 才发布 resolved/已提交；即使随后 output commit 或网络失败，回答仍是已保存事实 |
| result Sync 不确定 | poison，禁止继续工具/网络，不发布成功、不重新提交 |
| host detach/shutdown | 停止投递并排空，关闭等待走已有取消路径，join 后才释放 writer/lease |

回答 accepted/resolving 不等于 durable。迟到或重复响应不影响新请求；context 取消检查必须进入最终 admission 决定，不能只检查一个尚未更新的关闭标记。成功结果持久化后才清空活动问卷并允许后序调用，保证 resolved 不与下一请求交叉覆盖。

### 5. 持久化和恢复

继续使用当前四类工具事实，不新增问卷 Session event：

```text
sample/native + usage + ready ... --Sync/finalize-->
prior results --Sync-->
question started --Sync--> requested --> answer admission
question result --Sync--> resolved
later results --Sync--> ordered native outputs --Sync/finalize-->
steering / auto compaction / next sample
```

Question 正常 grammar 为 ready -> started -> result；approval fact 对它非法。无交互/预算拒绝可以 ready -> error/denied result，执行接收前取消可以 ready -> cancelled。success/outcome_uncertain 必须有 started。Patch/Write/Exec 的 allow 前置条件保持。

| 崩溃位置 | 恢复动作 |
|---|---|
| ready 后、started 前 | session_interrupted_before_execution，零重新提问 |
| started 后、result 前（包括只在内存接收了答案） | outcome_uncertain，不能声称答案已保存，也不能重弹问卷 |
| result 后、outputs 前 | 使用冻结 preview 按原 call index 补齐 outputs |
| outputs 已提交 | 不重复追加；按既有旧 turn 本地收口规则执行 |

恢复同时核对 native sample 与 ready 的完整题目。`provider.RestoreRecord` 在纯内存中携带该 sample 的 typed QuestionInvocation 和已提交 InvocationResult，app 按 NativeSequence 从已经验证的 ready/result 事实线性分组绑定；双 Provider 的 restore/offline reader 比对原生调用数、call ID、不可变 QuestionInput 及原生输出的完整冻结 bytes 一致后才安装历史。缺失或伪造关联整体拒绝。该绑定不进入 JSONL/native/checkpoint，不额外保存正文，不通过 transcript 展示值反向构造请求。

不把 Question exclusive 自动解释为“未批准”；修改恢复与 grammar 的所有相应判定。新 current fixture 固定为仓库 bytes，覆盖两家、混合组、各尾部及 checksum 合法的语义损坏；测试运行时不生成输入 fixture。旧开发 shape 直接失败，不迁移、不在 resume 重写已提交前缀。

### 6. Provider、预算与缓存

RequestCompiler/reducer/native codec/result codec 中只增加当前 facade 分支，公共层不解析 wire。OpenAI 输出保持 function_call_output，Anthropic 保持 tool_result；不将宿主回答保存成新 user_inputs，不修改 HistoryProjector 的单向文本契约。两家各自保持 opaque reasoning、phase、signature 和 tool pairing。

单次回答 preview 上限 128 KiB；原始文字合计 16 KiB。到达独占 slot 后从现有 resultBudget 取得 allowance，仍保留后续调用 1 KiB 安全空间，计入 sample 512 KiB/turn 2 MiB。问卷显示前至少保证最小合法结果可表示，提交前用真实固定 codec 编码完整结果预检；原始文字满足 16 KiB 并不代表 JSON 转义后能装下。

Provider 的单项 output 编码复用各自现有 result codec 私有逻辑参与纯内存预检。当前 PrepareToolOutputs 要求完整调用组，不能拿它编码单个待回答 slot；为现有 ToolResultPreparer 增加 PrepareToolOutputPreflight(invocation) 方法，返回小型 ToolOutputPreflight 策略，其唯一方法 Validate(result) error 只验证/编码本项。两家策略持有不可变 pending-call 快照，Runtime 在发布问题前准备它，回答 admission 在锁外使用它；不临时 finalize、读取活动可变 Conversation 或伪造 sibling results。预检同时检查 Session result 编码，失败返回给问卷保留草稿。不能等 accepted 后才用通用 FinalizeResult 截断答案；接收后额度由当前独占 slot 保有。最后全组 PrepareToolOutputs 的配对和总大小检查仍执行。

catalog 从八能力变九能力，定义变化应当改变 tool_catalog source revision、相关 golden 和稳定前缀 fingerprint；这是一项可定位的预期失效。以后改变题目、回答、请求 ID、窗口尺寸、说明输入、剩余额度不会再改变静态 catalog。原生调用/结果只影响历史尾部与完整请求；原生 footprint 覆盖其占用，不另外作为 current_input 或 Skill 注入。HTTP/SSE 与 WebSocket full/incremental 分别回归，不声称不同传输帧 bytes 相同。

### 7. TUI 状态与历史回看

状态为 choosing / editing / reviewing / submitting，原题序保存有界草稿。默认只聚焦首项，不标为已答。单选 Enter、复选 Space/Enter、Tab 说明、Shift+Tab 上题；所有完成都到复核页，可返回修改。未答题须明确标 skipped；复核中可以显式跳过整次提问并提交全 skipped，模型得到准确语义继续任务，不冒充用户选择。

文字输入复用 composer 的字素插入/删除/换行/paste 原语，使用独立实例与较小额度，不接入聊天历史。Esc 在文字编辑态先回选择；其余 Esc 和 Ctrl+C 使用现有定向 stop，返还 auto/保留 strict-next 的语义不变。Submitting 禁用重复决定，等待 resolved 或 terminal；accepted 返回不关闭问卷。无自动倒计时。

提问/审批均高于 transcript，scheduler 保证正常情况下不同时活动；矛盾事件失败关闭，不能猜默认选项。问卷关闭恢复聊天草稿、光标、历史浏览和 transcript 锚点。宽窄屏均可访问问题、全部选项、输入位置、复核动作；显示只处理副本，中文/emoji/双向和终端控制字符沿用既有规则。

主线 Question 独立显示并打断 Read/Search 分组。Ctrl+O 的原 typed transcript/detail 查询增加该能力，原题加冻结回答按 64 KiB 页展示、共享最多四页缓存；较长安全显示可分页，不能再建整会话副本。恢复后显示同一问题/答案，只有只读回看入口，没有旧回答提交按钮。

### 8. 无交互宿主

沿用当前 approval 的宿主失败关闭原则，但使用提问独立结果 `denied/user_input_unavailable`。catalog 不因终端或宿主变化而切换，真实执行路径立即返回配对结果，无 requested/resolved 问卷事件，无 stdin 消费和隐藏 goroutine。模型可以继续总结或自然语言询问，但仍受 16 samples/64 calls 上限。一次性和 stream-json 格式不变，外部不新增 answer command；将来需要远程回答必须另行确认协议。

## Risks / Trade-offs

- 独占与审批的历史耦合可能被漏改：按全部 Exclusive 使用点审计，并同时验证旧 Patch/Write/Exec 拒绝、allow、grant、恢复及混合调用；新增 Question approval 伪造 fixture 必须拒绝。
- 模型在同一 sample 预先产生了依赖答案的命令：不允许回答改写 frozen invocation；提示模型先单独提问，后续调用仍必须审批，不自动丢弃或重排 call。
- 用户误以为 accepted 已保存：UI 保持 submitting，只有 result Sync 后 resolved；Sync 失败和断开用确定性 barrier/fault injection 验证。
- 问题/说明可能超出显示或编码预算：严格输入限额、逐题滚动、复核分页与提交前实际编码检查；拒绝保留草稿，不能静默省略答案。
- 较窄终端无法同时展示所有信息：允许局部滚动并让动作可达，固定 12x6 snapshot 验证，无 I/O render 或尺寸导致的语义变化。
- 当前提问会阻塞其后工具：这是本次刻意选择的线性决策契约；后台问题、自动代答及 Plan Mode 均需要新的独立设计。

## Migration Plan

本项目没有多版本兼容需求。本节仅描述当前契约替换，不增加迁移器：在同一 apply 中更新全部封闭 union、decoder/validator、两家 codecs、catalog/goldens 与 current fixtures。旧 shape 非法即拒绝，既有合法已提交 bytes 不被恢复流程改写。变更回退由版本控制整体回退代码与 fixtures，不在新程序中保留双 reader。

实现顺序见 tasks；完成后先补实际验收记录，再同步 main specs、总体/Tool 架构、Roadmap 和 pitfall，另按 archive 流程归档。本提案阶段不修改生产代码、main specs 或已实现进度。

## Verification

2026-11-05 实现前基线 `make verify` 已通过：分支规则回归、gofmt、vet、Staticcheck、架构守卫、全量测试与 race；其中 app race 包约 231.5 秒。该结果只证明起点健康。

实现验收必须包括：

1. 输入/回答 strict decoder、深拷贝、重复/未知字段、null、UTF-8、选择顺序、每个精确大小边界、完整预算及多次 finalize。
2. 双 Provider request/stream/result/current fixture/golden；随机 chunk、半包、跨 UTF-8、断线与取消；Question 前后 mixed calls 和 opaque reasoning 保真。
3. 单 owner 的回答与 stop/shutdown/父取消竞态、重复/stale、宿主断开、result Sync 失败及 cleanup timeout，全部用 barrier/故障注入，不用 sleep 证明。
4. 两家 uninterrupted/restored canonical bytes、原生 item 顺序、footprint、内容/请求 fingerprints；结果已 durable 而 output 未提交、started 无结果及完整性错误零执行恢复。
5. 实子进程 lease 竞争与崩溃释放、等待期间禁止 repair、清理完成前 lease 不释放、续写 seq 单调和拒绝路径 bytes 不变；复用已有 harness。
6. 80x24、34x12、12x6 和无色 snapshots，中文/emoji、单选/复选/其他/复核/跳过、paste/resize/草稿隔离/历史页缓存，以及 stale UI reply。
7. 使用本地假 Provider 的真实 PTY smoke：完整作答继续、stop、Ctrl+C、resize、退出 0/130 与终端恢复；不使用真实 key、不依赖外网。
8. 当前八能力原有回归、缓存来源矩阵、全仓 `make verify` 与 `openspec validate --strict`。实际支持的平台、无法执行项目及风险必须逐项记录，不能用交叉编译宣称原生平台验证完成。

## 实现核对

实现已按上述唯一当前契约落地，实际验证与平台范围见 [verification.md](verification.md)。共享值放置在 protocol、tool 用别名复用，是遵守既有单向依赖的内部布局选择；未增加新 scope、兼容 reader、格式版本或额外 owner。
