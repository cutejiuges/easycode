## Context

动机和产品范围见 [proposal.md](proposal.md)。本设计基于当前源码与两次只读 explore，不将参考工程的实验开关或远程服务能力当作 EasyCode 已有能力。

当前接缝：

- `internal/runtime/input_commit.go` 在输入 Sync 前准备候选历史、预算与 canonical request；明确超限返还输入。`agent_loop.go` 是输入/取消/关闭的单 owner。
- `internal/context/planning.go` 按 profile、tools、项目指令、history、采样标记规划，使用 `max(semantic, native)` 覆盖估算；不能把压缩后的 native footprint 与全部历史的语义估算混用。
- 两家 `request.go` 从同一份原生历史编译请求；`history.go` 和 `transcript.go` 从这份历史投影。`internal/app/transcript_projection.go` 通过原始 entry ordinal/Session sequence 关联工具事实，不能清空或重新编号。
- `internal/provider/user_input.go` 的私有 prepared lifecycle 可复用；普通 sample/user_inputs/tool_outputs 不能冒充压缩内容。
- `internal/session/registry.go` 与 `replay.go` 目前只支持 metadata/turn grammar；`protocol/event.go` 的非空身份要求 turn，必须显式扩展操作作用域。
- 预算配置仅显式 JSON；估算是 `byte_heuristic_v1`，没有远程计数和自动窗口推断。Anthropic 普通请求默认输出 4096；OpenAI 普通请求没有摘要专属输出上限。

参考路径相对于主 checkout 的相邻仓库；当前 worktree 实际只读位置为 `/Users/cutejiuge/IdeaProjects/{claude-code-sourcemap,codex}`。实现不依赖这些目录存在。

| 参考证据 | 采用的优点 | 本项目的取舍 |
|---|---|---|
| Claude `restored-src/src/commands/compact/compact.ts`、`utils/processUserInput/processSlashCommand.tsx` | 本地命令、保留重点、成功后不自动普通续写 | 不把 synthetic command stdout 加入模型历史 |
| Claude `components/CompactSummary.tsx`、`components/messages/CompactBoundaryMessage.tsx` | 主线简短、Ctrl+O 展开 | 复用现有 transcript browser，不另建历史存储 |
| Claude `commands/context/context.tsx` | 统计实际模型窗口 | 查询共享不可变快照，不运行有副作用的变换 |
| Claude `services/compact/{compact,prompt,grouping,autoCompact,postCompactCleanup}.ts` | 工作交接、清晰边界、预算预留、熔断、按 API round 理解长任务 | 首期全窗口手动压缩；不复制全局状态、文件重读、实验分支或裁掉旧历史重试 |
| Codex `codex-rs/core/src/compact.rs`、`history/src/compaction_checkpoint.rs` | checkpoint 与来源身份、请求和恢复一致 | 普通文本摘要不冒充 encrypted compaction，不声明专用远端能力 |
| Codex `core/src/session/mod.rs` 的 `replace_compacted_history` | 安装前核对 generation、冻结恢复元数据 | 参考路径先改内存再持久化；EasyCode 改为 Sync 后 finalize |
| Codex `tui/src/chatwidget/compaction.rs` 及对应 tests | 操作身份、去重、旧完成事件隔离、压缩时输入排队 | 保持 Claude 的主线提示；不把回放耗时当实时计时 |

## Goals / Non-Goals

**Goals:** 保证模型窗口可缩减且原始业务事实完整；完整 transcript 与有效窗口始终可区分；任何取消/崩溃位置都能确定窗口归属；普通对话、查询和压缩共享宿主控制 owner；所有新边界具有强类型、恢复与消费者测试。

**Non-Goals:** 不提前建立通用任务、扩展 registry 或 Hook pipeline；不替换正常对话算法；不引入二级摘要文件、SQLite 事实或摘要权限凭据；不在本次实现自动压缩触发、跨 Provider 转换、保留任意后缀或多级分块摘要。详见 proposal 的范围排除。

## Decisions

### 1. Claude 风格的本地交互与明确的 admission

TUI 在 composer submit 边界静态识别首行精确命令 `/context` 与 `/compact`，不做子串匹配。`/context` 不接受参数；`/compact` 的剩余 UTF-8 正文作为 instructions，可包含换行，去除外围空白后最多 16 KiB。`/contextual`、`/compactness`、绝对路径等按普通文本发送，不创造未知 slash 命令框架。命令文字与 UI stdout 不进入 Provider user_inputs，压缩指示仅进入临时摘要请求。接受前保留草稿，拒绝不清空；接受后本地 recall 可保留命令，但不伪装成已提交用户消息。

`context.query` 在 idle/turn/compacting 状态均可读最近已提交快照；显示 scope=committed、快照 revision、是否有活动操作，不含 streaming staging 或 queued 输入。无额外确认或网络请求。

`context.compact` 只有 owner 观察到 idle 且输入队列为空时才能接收；不能插队，忙碌返回 `session_busy`。纯内存预检不满足时返回 rejected，不分配 durable operation、不写 started。接受返回 CompactionID；接受不等于开始已持久化。之后输入允许按现有 FIFO 排队，压缩期间不 claim。

成功：简短 `Conversation compacted` 边界与 Ctrl+O 提示；摘要在详情中显示，不刷普通 assistant delta。没有排队输入则等待；有明确已接受输入则在 terminal 和 cleanup 后执行。普通失败/用户取消：queued auto 返回既有 returned tray，strict-next 保留并在清理后按既有规则执行；不自动重试压缩。shutdown/poison 遵守全局关闭规则。压缩期间 Ctrl+C 定向取消；Esc 优先取回 queued auto，无可取回项时定向取消；详情层 Esc 先关闭详情，与已有路由优先级一致。

替代方案：直接把 `/compact` 当 user prompt 会污染 history、usage 与 turn grammar；活动 turn 中插入压缩会扩大首期安全点范围，均不采用。

### 2. 有效窗口属于 Provider，完整记录保持稳定

Provider 实例保留完整原生提交，另持有有效窗口描述：最近 checkpoint、其覆盖的 native revision 和此后的原生增量。首次无 checkpoint 时窗口等于全部原生历史。成功全窗口压缩后窗口为一个明确标记来源的原生摘要上下文；下一输入及 samples/tools 按现有方式追加到尾部。第二次压缩只总结上次摘要加新增内容，不重新发送归档前文。没有新 native commit 时拒绝 `nothing_to_compact`；失败重试不受此限制。

保留 `ProjectHistory`/`ProjectTranscript` 的完整事实语义，新增小型只读 `ActiveContextView`：独立的 summary 文本段、有效窗口中的有序可见消息及 source revision；不使用伪 TurnID/InputID 表达 summary。ContextPlanner 与 prepared input preview 改读该视图；请求编译仍只读 Provider 私有窗口。

将原生记录的 `committed revision` 与窗口 `context revision` 区分：普通 native commit 同时推进两者；成功 checkpoint 只推进 context revision；失败、查询和 staging 均不推进。source identity 用 committed revision、前一 checkpoint 身份、有效原生内容 digest 绑定；首次 checkpoint 的前驱使用显式 root 状态，不用空字符串猜测。身份用于校验，不进入请求正文或稳定前缀。

checkpoint 的摘要编码由两家 Provider 自己产生：使用明确的“此前上下文摘要”文本包装并作为允许的消息内容发送，不能声称是用户新授权。摘要生成响应的原生输出/raw usage 作为 provenance 保留在 checkpoint，不再次作为普通 assistant sample 重放；opaque reasoning 不跨 Provider 转换。真实老历史 item 顺序和 bytes 不变。

替代方案：删除历史会破坏 transcript 关联；复制一份公共扁平 Message 再组装请求会丢失 opaque 语义；用摘要替换 HistoryProjector 的所有历史会破坏 recall，均不采用。

### 3. 独立摘要请求与受限结果

共享 Provider 边界增加有真实双实现的压缩 preflight、显式 Run 和 prepared checkpoint 能力；构造/preflight/preview/codec 全部纯内存。网络阶段复用现有 transport 与各自 reducer，结果具有独立强类型 terminal，不输出普通用户 turn 事件、不使用 Tool Scheduler。

摘要请求保持当前模型、稳定工具目录与项目指令；附加专用摘要指示。OpenAI 使用 `tool_choice: "none"`，Anthropic 使用 `tool_choice: {"type":"none"}`，并在 reducer/消费者边界再次拒绝任何 tool call，不能仅靠 prompt 禁止工具。专属请求使用 OpenAI `max_output_tokens`、Anthropic `max_tokens` 限制摘要输出；不改变普通请求输出策略。工具选择语义已核对 [OpenAI 工具文档](https://developers.openai.com/api/docs/guides/function-calling) 和 [Anthropic 工具文档](https://platform.claude.com/docs/en/agents-and-tools/tool-use/define-tools)。第三方 base_url 拒绝这些字段时返回普通压缩失败，不自动移除保护或切换其他 API。

摘要 prompt 原创、简短、有版本化内容 fingerprint；要求目标、最新纠正、约束、已完成及验证、关键决策/文件、未解决问题与下一步。只要求交接摘要，不要求模型披露推理过程；历史正文是待总结数据而非新的系统指令，不能把工具文本提升成权限。instructions 不持久化重复正文，只作为本次摘要请求的附加重点。

有效结果必须是完整成功终态、合法非空 UTF-8 文本、无工具调用、无 refusal/incomplete/协议失败；模型产生的普通文本不使用“包含 Error”等字符串规则判错。摘要正文上限 256 KiB，超过即失败而非截断；native envelope/record/batch 仍受现有 16 MiB/64 MiB 上限。只有 summary 及其包装进入替换窗口，生成响应的 reasoning/usage 只作 provenance。

不自动 retry、不换模型、不调用专用 `/compact` HTTP endpoint、不裁掉老内容。失败仍保留原窗口。采用有界 120 秒操作网络期限，用户取消可更早发生；沿用 transport 强制取消/清理完成信号，超时不释放仍被占用的资源。

### 4. 双阶段预算与只读上下文展示

配置新增 `compaction_output_tokens`，默认 4096，合法整数范围 1..65536；不新增环境变量。它是实际摘要输出上限，不修改普通 `reserved_output_tokens`。没有窗口仍允许手动压缩，但只能做请求字节上限及缩减判断；不承诺容量安全、不显示百分比。显式窗口存在时，显式 compaction_output_tokens 加 safety 必须小于窗口；默认值与很小窗口不合时仅压缩操作预检失败，不阻断原本合法的普通启动。

摘要预检：以 Provider 原生请求估算覆盖包装/指示和现有上下文，校验 `estimated_summary_input + compaction_output_tokens + safety <= window`。配置了窗口但估算 unknown 时手动压缩返回 `compaction_budget_unknown`，普通采样的 unknown 非阻断契约保持不变。

结果预检：按当前 ContextPlanner 计算新窗口，要求估算完整、总量严格小于旧窗口；窗口已配置时还必须满足普通有效输入上限。否则 `compaction_no_reduction` 或 `compaction_budget_exceeded`，不安装 checkpoint。固定的工具/项目内容过大时允许明确失败，不为了“成功”删除稳定来源。

`/context` 返回分类数值、估算方法/质量、窗口配置/预留、source revisions 与预算状态，不含 prompt/raw JSON/opaque 内容。展示可用字符条或网格；小窗口降级成表格，无色模式仍可辨识。显示当前估算与预留，不把摘要调用 usage 或累计计费 token 当窗口占用。未配置/unknown 不生成确定比例。Context 查询结果不写 journal、不推进 revision、不访问文件/Provider。

### 5. 独立操作身份与内存状态机

`CompactionID` 使用现有 UUIDv7 生成基础，领域值具有独立 validator。Runtime 命令新增 `query_context`、`compact_context`、`cancel_compaction`；取消必须带 expected CompactionID。Runtime Event 按 kind 严格区分 thread 级 context snapshot、compaction 级事件、turn 级事件；压缩事件没有 TurnID，普通 turn 规则不放宽。RequestID 只用于进程内 control 关联，不持久化。

AgentLoop 保持一个控制 owner 和最多一个活动 worker（turn 或 compaction）。用封闭 typed 状态分支扩展现有 owner，不新增无用途的通用 Operation interface。ctx 仅控制命令 handoff 前；之后 owner 拥有结果。worker 发布不可变 snapshot 给 owner 查询，避免查询与 Provider 写锁跨层竞争。不会在 owner select 内同步等待网络而阻断取消和查询。

```text
Idle -> preflight -> accepted -> started/Sync -> summarizing
                         |                         |
                         |                         v
                         |                     candidate
                         |                         |
                         |                    validate/encode
                         |                         |
                         v                         v
                    failed/Sync <----------- append admission
                                                   |
                                               terminal/Sync
                                                   |
                                                finalize
                                                   |
                                                completed
```

append admission 后必须等待确定结果；失败是否可继续由 writer poison 状态判断，不能把 Sync 错误包装成普通可重试失败。已成功 Sync 的 completion 即使同时取消也只报告 completed；不得再追加 failed。accepted 后 started 写入未发生的确定失败也须给宿主唯一失败事件，但无伪造 durable started；持久化不确定时只给安全 session 错误和失败状态，不声称已 durable 收口。

### 6. Session grammar 与 checkpoint 编码

Session schema/payload 当前常量统一从 6 到 7；Provider 当前 payload 从 2 到 3。不是双 reader 或 revision router。原有 native 三种 kind 保持意义；新增独立 checkpoint envelope，由 `compaction_completed` 保存，不插入普通 `provider_native_commit` 序列，不改变 transcript 的原生 ordinal。

新 records 使用现有 envelope 的 session/thread identity，TurnID 为空，CompactionID 存放在专属 typed payload，不扩大通用 envelope 的可选身份字段。placement 新增操作 start/terminal，replay 中普通 turn 与 compaction 互斥。

| record | 必需内容 | placement |
|---|---|---|
| `compaction_started` | CompactionID、source committed/context revisions、前驱 checkpoint 或 root、有效原生内容 digest、family/wire/model、prompt revision | 单记录 batch；idle，来源必须对应当前窗口 |
| `compaction_completed` | 同一身份/来源、Provider-owned checkpoint envelope、normalized usage、前后估算 | 单记录原子 batch；对应唯一活动压缩 |
| `compaction_failed` | 同一身份、封闭英文 code、明确 observed/unavailable usage 状态与适用的计量 provenance | 单记录 batch；对应活动压缩 |

checkpoint envelope 持有有界强类型 provider 元数据及受控 opaque payload；Provider 校验摘要、包装、原生生成响应/raw usage、来源及 payload canary。Session 不解析 wire。summary 不在 Session 公共字段再复制一份；UI 通过 Provider 的只读 checkpoint 投影取摘要。

失败 usage：收到完整合法生成响应但候选因预算/缩减失败时，记录实际 normalized usage 和对应 Provider 私有计量 provenance；没收到可信终态则记录 unavailable，不推测零。失败记录不携带可安装窗口。计量 provenance 不包含失败请求/响应正文，known raw usage 字段保留在 Provider-owned 有界计量编码中。

序列：started Sync -> network -> prepared candidate -> source 重验和全部编码上限预检 -> completed Sync -> finalize -> UI/query 状态更新。普通网络/候选失败：discard -> failed Sync -> terminal。候选 finalizer 只做有界内存交换，不读文件，不发事件，不做校验失败后重新请求；不可恢复的内部 finalize 故障也 poison。

completion provenance 只保存本次模型原生输出，不复制完整旧窗口。覆盖范围通过 source references 和 digest 验证；完整 journal 保持追加。SQLite Catalog 继续用最终 seq/timestamp 重建最近会话，不增加摘要正文或压缩专属可执行索引。

### 7. 恢复、投影和取消线性化

长期恢复顺序决策见 [ADR-0007](../../../../docs/architecture/adr/0007-validate-before-journal-repair.md)。

恢复顺序修订：现有 Loader.Load 在读到半行时立即 Truncate/Sync，早于 app 的 replay 和 Provider 校验；这一顺序不能满足组合损坏时 bytes 不变的要求。改为唯一当前的两阶段生命周期，不保留隐式修复入口：

1. 持有连续 exclusive lease，只读 Load 完整 committed 前缀，产生绑定本次 lease、原始文件大小与内容 digest 的不可伪造待修复凭据；扫描本身不截断、不 Sync。
2. app 验证 replay、family/wire/model、全部原生记录/checkpoint 来源，再调用显式 Repair；Catalog 只读扫描并索引完整前缀，不消耗凭据或修复文件。
3. Repair 拒绝不同 lease、失效凭据、扫描后文件变化及重复消费。执行前核对同一打开的 handle 与内容 digest，取消在 Truncate 前检查；从接受截断开始不再因取消返回模糊结果，必须完成 Sync。截断或 Sync 失败使 lease poisoned，禁止转交 writer；关闭仍由原 owner 执行。
4. 扫描与修复期间禁止并发 transfer/使用同一 handle，Close 等待已接收 I/O 的完成信号，锁内只转换状态。Repair 成功后才启动 writer，并执行 interrupted 操作的本地补偿。
5. LoadResult 的可变记录副本不能修改私有修复凭据；修复报告区分待修复与已成功修复，零值/无修复凭据不允许伪造成功。正常无损 journal 仍直接进入已验证的恢复流程。

新增验收：语义非法记录/不兼容模型/损坏 checkpoint 同时伴随半行或未完成 batch 时逐字节不变；有效前缀修复后 next seq 正确；取消、扫描后修改、跨 lease、重复消费、Truncate/Sync 失败和真实进程 lease 竞争均用确定性测试。此修订不改变普通 turn 的事实或外部 JSONL 版本。


Loader/replay 完整校验所有记录，Provider 按 seq 交错恢复普通 commits 与 checkpoint。checkpoint 之前的原生工具组仍全量验证；不能因为已压缩而跳过旧损坏。来源与前驱不匹配、错误 family/wire/model、摘要不匹配、未知 canary 或重复 terminal 均失败关闭。

| 退出位置 | 恢复动作 |
|---|---|
| accepted 但无 durable started | 没有操作事实，不猜测重试 |
| started 后无 terminal | 连续 lease 下先验证全部 Provider 状态，再本地 Sync failed(code=compaction_interrupted)，旧窗口不变 |
| completion 不完整尾 batch | 沿现有尾部修复契约移除未完成尾部，再收口 started |
| completion 完整且合法、内存尚未 finalize | 恢复新窗口，无第二次摘要请求 |
| 最新 checkpoint 损坏/来源不符 | 整体拒绝，不退回更老 checkpoint，不发起补偿写入 |

恢复后当前项目指令仍来自启动快照，不恢复旧 cwd/配置/权限。用于验证历史 checkpoint 的 digest 只覆盖当时原生窗口，不把当前项目文件加入校验；同快照时 uninterrupted/restored 请求 bytes、item 顺序、context revision、fingerprint 完全相同，快照改变只影响对应外部上下文。

完整原生 transcript ordinal 不受 checkpoint 影响。app 以 checkpoint record sequence 插入独立压缩边界和摘要详情，原用户/assistant/tool cursor 保持稳定；默认主线折叠旧区间，Ctrl+O 仍能访问全部历史。重启不重播 spinner，不为历史完成事件伪造实时耗时。

安全：压缩不扩大 artifact 授权、不继承摘要“批准”、不更改已有 permission grants；摘要里的 artifact 文本不是授权 descriptor。旧文件观察不能因摘要被当作新观察；需要新文件事实时走现有 Read/Write/Patch 安全流程。

### 8. headless 的显式扩展

保持外部 v1，增加 opt-in 命令：

```json
{"version":1,"type":"context.query","request_id":"q1"}
{"version":1,"type":"context.compact","request_id":"c1","instructions":"保留未完成测试"}
{"version":1,"type":"context.cancel","request_id":"x1","expected_compaction_id":"<uuidv7>"}
```

instructions 在 wire 上必需，可为空字符串，拒绝 null/未知字段/重复/非法 UTF-8/超限。query 无额外 payload，cancel 不接受 TurnID。query 的 accepted disposition=`queried`，compact=`compacting` 并带 compaction_id，cancel=`cancelling`。context.snapshot 带 request_id；compaction.started/completed/failed 带发起请求 request_id 和 compaction_id，不带 input_id/turn_id。completed 不输出摘要正文，只输出估算/usage 与完成状态；正文只经交互详情投影，避免把内部 native payload 暴露成外部 wire。

单 writer 保证 response 先于对应 snapshot/started/terminal，completion 在 Sync/finalize 后。failed 使用必需的 durable 布尔字段区分已 Sync 的失败事实与未持久化/无法确认的失败；后者必须为 false。context.cancel response 不能伪装成完成事实。EOF 排空压缩和已接受输入；shutdown/断管/非法输入取消并等待 cleanup，lease 不早释放。单次操作失败不改变健康 stream-json 的最终退出码规则。

一次性 `--print`/`--json` 输入继续作为正文，不解释 `/compact`；本次不增加 CLI flag/subcommand。旧命令的字段与顺序保持原样，只使用新命令的客户端才收到新控制结果。

### 9. 代码边界、模式与验收定位

建议文件名是实施定位，不要求生成空文件：

| 区域 | 职责与拟新增测试 |
|---|---|
| `domain/compaction.go`、`protocol/context.go` | 身份、值对象、专属构造/decoder；零值、非法 scope、复制隔离 |
| `context/compaction.go` | 纯预算与候选判定；精确边界、溢出、unknown、无缩减 |
| `provider/compaction.go`、两家 `compaction.go` | 小接口、prepared lifecycle、原生摘要编译/结果/checkpoint；`compaction_test.go`、`compaction_restore_test.go`、golden |
| `session/compaction.go`、replay/codec/registry | 强类型 facts/互斥 grammar；`compaction_test.go`、`compaction_process_test.go`、固定 current fixtures |
| `runtime/compaction.go`、AgentLoop | 独立 worker、admission、队列、取消、poison；`compaction_test.go`、`compaction_control_test.go` |
| `app`/transcript queries | 恢复、窗口与完整历史组合；`compaction_e2e_test.go`、`compaction_resume_test.go` |
| `tui/context_commands.go`、`headless`、`config` | 本地分发、展现、wire/config；固定尺寸 snapshot、NDJSON golden |

采用组合式 Provider 策略和封闭状态机；复用 prepared 私有 resolution 骨架、Session writer/lease、transport、输入队列、transcript browser。禁止把 Provider 编码/存储/权限塞进 ContextManager，禁止 embed shadow 模拟模板方法，禁止增加未被调用的扩展接口或全局 registry。

验证矩阵至少包含双 Provider：普通文本、工具闭环、失败但已提交输入、opaque reasoning、连续两次压缩、压缩后 steering、相同/变化项目指令、artifact 不存在仍能恢复、旧历史详情、nil/重复 resolution、错误 scope、取消与 Sync 竞态、真实子进程 lease/崩溃、尾部半行/批次和中段损坏、stdout 背压/断管、窄屏/无色/草稿与 recall。请求和恢复验证必须逐字节比较 canonical bytes、顺序及 fingerprint，不能只验证摘要包含某个单词。

## Risks / Trade-offs

- 摘要有损且无法自动证明事实完整性 → prompt 关注最新意图和验证状态，原历史可查，模型摘要不成为权限/文件事实。
- 极长窗口可能连摘要请求也放不下 → 明确失败且原窗口不变；不承诺本次解决任意溢出，后续单独规划自动/分块压缩。
- byte heuristic 尤其对 opaque 数据不精确 → UI 明示 estimated，unknown 保真，不用 usage 补算；固定预算边界回归。
- 保留全部记录使常驻内存仍随会话增长 → 本次解决模型窗口；不宣称降低完整 transcript 内存，持久化分页另行评估。
- 工具目录/输出控制字段可能影响服务端缓存 → stable source fingerprint 与真实 cache hit 分开，固定普通请求 golden 不变，摘要请求单独 golden，不宣称缓存命中保证。
- 首期只能 idle 压缩 → 用户可能需要先停止长工具 turn；自动安全点压缩是后续 change，当前不暴露配置开关。
- 独立操作会触及身份和 replay grammar → typed scope、跨包架构测试、故障注入与单一当前 canary 同步更新。

## Migration Plan

1. Apply 时先落地强类型契约、编解码/恢复与测试，再接 Provider/Runtime 和宿主消费者；所有新增可执行契约在本 change 完成真实闭环。
2. 一次性替换 Session 当前 7 / native 当前 3，更新 `testdata/current` 固定 bytes 和所有 canary 断言；未知/旧 shape 在 repair/append 前失败，不迁移旧 journal。
3. 外部 headless v1 保持独立。现有配置省略新字段保留正常启动行为，无自动网络工作。
4. 同步 README、总体架构、Roadmap、pitfall 和新的长期决策 ADR；保留自动压缩/Hook 的未实现标记，核对真实 record vocabulary 与测试路径。
5. 完成 `openspec validate --strict`、`make verify`、双 Provider restore/cache golden 后再 review/archive。回退代码不删除或改写新 journal；旧二进制必须拒绝新 canary，需要继续工作时使用匹配当前契约的版本或新建会话。
