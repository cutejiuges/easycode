## MODIFIED Requirements

### Requirement: Session records use a versioned extensible envelope

每条 JSONL 记录 SHALL 使用强类型、版本化的 envelope，至少包含 `schema_version`、`payload_version`、`replay_requirement`、单调 `seq`、UTC `timestamp`、`session_id`、`thread_id`、`event_kind`、完整性校验值和受控 JSON `payload`；与记录语义相关时 SHALL 同时包含 `parent_thread_id` 与 `turn_id`。同一 thread 文件中的 session/thread 标识 MUST 保持一致，未知 payload 只能作为有边界的 opaque JSON 传递，不得展开为跨层 `map[string]any`。

`replay_requirement` v1 只能是 `required` 或 `optional`。影响 Provider 原生历史、turn lifecycle、usage 归属、权限、工具副作用、幂等或恢复决策的记录 MUST 标记为 `required`；只影响索引、展示或可丢失统计且被省略不会改变后续副作用、Provider request 或已发布用量事实的记录才可标记为 `optional`。七种已知记录的当前 revision 全部 SHALL 标记为 `required`。已知 kind/revision 的 requirement、cardinality、顺序约束和 payload codec SHALL 由强类型语义 registry 唯一声明，记录中的声明与 registry 不一致时 MUST 拒绝恢复。

本切片 SHALL 定义 `session_meta`、`thread_meta`、`turn_started`、`provider_native_commit`、`sample_usage`、`turn_completed` 和 `turn_failed` 的已知 payload。`provider_native_commit` 表示一个由对应 Provider codec 定义的有序 native history 增量；`sample_usage` 表示紧邻的同一 completed sample 的 normalized usage。当前首版 native 增量是一次成功文本 sample 的输入与输出，不得把该形状或“一个 turn 只有一次 commit”固化到公共 envelope。

未来 Provider wire 中出现的 `tool_use`、`tool_result`、reasoning 或 MCP tool block SHALL 作为相应 sample 的 Provider 原生内容保留在 `provider_native_commit`；权限决策、工具副作用、幂等 ledger、MCP progress 和大结果 artifact 等共享执行事实不得伪装成 Provider 原生 payload，必须由后续 change 定义独立记录种类与持久化策略。

#### Scenario: Encode the initial record vocabulary

- **WHEN** 一个 root thread 完成一次文本 turn
- **THEN** JSONL 使用同一 envelope 记录 session/thread 元数据、turn 边界、该次 Provider sample 的原生提交和 normalized usage
- **THEN** 每条记录都包含可独立校验的 schema 与 payload revision
- **THEN** 七种已知记录的当前 revision 均标记为 `required`

#### Scenario: Preserve an unknown optional record

- **WHEN** 当前程序读取一个 envelope 版本受支持、结构和 checksum 合法但 kind/revision 未知且标记为 `optional` 的记录
- **THEN** loader 有界保留其 opaque payload 并报告 kind/revision，resume 可忽略该记录继续
- **THEN** 未知 payload 不进入日志、错误或语义投影

#### Scenario: Represent multiple samples in one future turn

- **WHEN** 后续工具循环需要在同一个 turn 下记录多次 Provider sample
- **THEN** 每次 completed sample 可按 seq 追加配对的 `provider_native_commit` 和 `sample_usage`，无需改变既有 envelope 或把原有文本 turn 改写为新形状

#### Scenario: Commit a future tool result before the next sample

- **WHEN** 后续工具循环已经 durable 完成工具结果，但下一次 Provider sample 尚未发起或完成
- **THEN** 后续 change 必须定义独立、强类型的 input-only native history commit shape；只有无法在已冻结 v1 中加法表达且必须兼容既有数据时才可引入新的 payload revision
- **THEN** input-only commit 不伪造 `sample_usage`，恢复不需要重复工具副作用，也不需要等待下一次模型输出才能保留 Provider 原生 tool result

#### Scenario: Preserve a future native tool pair without conflating execution state

- **WHEN** 后续工具循环的 Provider sample 包含原生 tool call 或 tool result item
- **THEN** Provider codec 在相应的强类型 commit shape 中无损保存该原生内容，而 JSONL envelope 和写入机制保持不变；新增 revision 必须满足冻结契约不兼容且需要并存的版本门槛
- **THEN** 权限、执行、幂等和 artifact 事实使用独立 record kind，不从 Provider 原生内容反推

### Requirement: Replay validation enforces record state transitions

在 loader 完成字节、envelope 和 batch 校验后，恢复消费者 SHALL 在创建可调用 Provider Conversation 前对已知 required records 执行强类型语义回放。首个 committed batch MUST 恰好建立唯一的 `session_meta` 与 root `thread_meta`；后续 metadata 不得重复。root thread 同一时刻最多有一个活动 turn，`provider_native_commit`、`sample_usage` 和 terminal 必须引用当前活动 turn，terminal 关闭该 turn 后才能开始下一 turn。

当前 v1 的唯一合法完成路径 SHALL 为独立 committed `turn_started(v1)`，随后是同一 batch 中严格按顺序出现的 `[provider_native_commit(v1), sample_usage(v1), turn_completed(v1)]`；`sample_usage` 与前一 native commit 配对，不能缺失、重复或脱离 completed sample。本变更前的 `[provider_native_commit(v1), turn_completed(v1)]` 开发期结构不再是合法回放路径，loader MUST 在 repair、append 或 Provider 请求前 fail closed，且不得补写 usage 或把它作为同版本的历史变体继续运行。合法失败路径 SHALL 为 `turn_started` 后的唯一 `turn_failed` 且不包含当前 turn 的成功 sample facts。只有位于 committed journal 末尾且尚无后续记录的单个未闭合 `turn_started` 可被识别为 interrupted-tail replay state；它是需要显式收口的业务状态，不是可由 loader 截断的文件损坏。未来工具循环 MAY 通过新的 required kind 扩展状态转换；只有冻结后的 v1 无法加法表达且旧数据必须并存时才可新增 revision。

#### Scenario: Build a valid text replay plan

- **WHEN** journal 包含唯一 metadata batch、一个失败文本 turn，以及一个当前 v1 三记录完成 batch
- **THEN** replay validator 按 seq 生成包含 committed native commits、sample usage 和 terminal 状态的完整计划
- **THEN** Provider 只接收计划中通过语义校验的 native commits

#### Scenario: Reject a checksum-valid illegal transition

- **WHEN** journal 的 JSON、seq、batch 和 checksum 均合法，但 v1 completion 缺少配对 usage、usage 顺序错误、存在重复 usage、commit 位于 turn 之外、重复 terminal 或新 turn 覆盖未结束 turn
- **THEN** resume 以稳定英文 session corruption 错误失败且不创建可调用 Conversation

#### Scenario: Report a committed interrupted tail

- **WHEN** journal 以完整 committed `turn_started` 结束且没有该 turn 的 native commit、sample usage 或 terminal
- **THEN** replay validator 返回显式 interrupted-tail 状态而不截断该记录、不补造成功历史或 usage

### Requirement: Known record revisions expose typed construction and decoding

每个已知 Session `event_kind`/`payload_version` 组合 SHALL 具有专属的强类型 draft constructor、严格 decoder 和语义 validator。公共 record draft、writer、replay 与生命周期接口 MUST NOT 接受或返回无约束动态值；未知扩展只允许作为有大小边界的 opaque JSON 保留。构造和解码都 SHALL 拒绝字段缺失、未知字段、尾随 JSON 和不满足该 revision 语义不变量的 payload。

新增 `sample_usage(v1)` MUST 使用专属 typed codec；`turn_completed` 保持 payload v1，并由 v1 ReplayPlanner 校验其配对 usage。七种当前 payload 的 canonical JSON、envelope 和 checksum SHALL 通过重写后的单一 v1 fixture 固定；实现不得保留旧两记录完成结构的 decoder/replay 成功分支，也不得为本能力引入 v2 codec。

#### Scenario: Construct every v1 record through a typed API

- **WHEN** 调用方创建 `session_meta`、`thread_meta`、`turn_started`、`provider_native_commit`、`sample_usage`、`turn_completed` 或 `turn_failed` 的受支持 draft
- **THEN** 对应 constructor 只接收该 kind/revision 的强类型字段或 payload
- **THEN** draft 不能被调用方改造成 kind、revision 与 payload 不匹配的记录

#### Scenario: Strictly decode a known payload revision

- **WHEN** checksum 合法的已知 record payload 含未知字段、尾随 JSON 或非法 usage 语义值
- **THEN** 该 kind/revision 的 decoder 在 replay 前拒绝记录
- **THEN** 错误不包含 opaque payload 正文

#### Scenario: Preserve historical v1 bytes

- **WHEN** 当前实现加载本变更重写并冻结的 v1 fixture，并以相同固定 identity、时间和 batch 参数编码等价记录
- **THEN** 解码后的 typed replay model 与当前 v1 期望一致
- **THEN** canonical record bytes 和 checksum 与重写后 fixture 完全一致

## ADDED Requirements

### Requirement: Sample usage is committed atomically with its sample

当前 v1 成功文本 sample SHALL 在一个 durable batch 中依次写入 `provider_native_commit(v1)`、`sample_usage(v1)` 和 `turn_completed(v1)`。三条记录 MUST 使用相同 session/thread/turn 与 batch identity并获得连续 seq；只有整个 batch 成功 Sync 后才能确认任一事实。`sample_usage` payload MUST 完整包含五个 normalized metrics 及合法状态，不得包含 Provider raw usage、价格或请求正文。

本变更前的 `[provider_native_commit(v1), turn_completed(v1)]` 开发期 batch SHALL 被拒绝为不兼容的旧基线，不得继续回放、补写全零 `sample_usage`、原地升级或恢复后追加新 turn。原 journal bytes MUST 保持不变并可供人工检查。

#### Scenario: Commit a new sample usage batch

- **WHEN** 当前 writer 收到合法 prepared sample 并完成文本 turn
- **THEN** journal 以连续顺序原子追加三个 v1 record：native commit、normalized sample usage 和 completion
- **THEN** durable success 只在三条记录均 Sync 后返回

#### Scenario: Fail closed on the superseded development baseline

- **WHEN** loader 读取本变更前生成的 v1 两记录完成 batch
- **THEN** resume 在 repair、append 或 Provider 请求前以稳定英文不兼容错误失败
- **THEN** journal bytes 不被修改且不生成全零 usage或兼容 replay plan

#### Scenario: Fail an incomplete new usage batch

- **WHEN** 尾部新 completion batch 只写入 native commit 或 native commit 加 sample usage而未完整提交
- **THEN** loader 按既有 batch repair 规则移除整个未完成 batch
- **THEN** 不恢复部分 native history 或孤立 usage
