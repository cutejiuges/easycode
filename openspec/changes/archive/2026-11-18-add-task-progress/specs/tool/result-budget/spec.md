## MODIFIED Requirements

### Requirement: Tool preview budgets have explicit units and deterministic allocation

系统 SHALL 计量最终模型 preview 的 UTF-8 bytes，包含行号、结果说明、省略提示和 artifact 引用。Read/Exec/ReadArtifact 单次上限为 256 KiB、Glob/Grep 为 64 KiB、Skill 为 128 KiB，每 sample 总上限 512 KiB、每 turn 总上限 2 MiB。Patch/Write 返回紧凑摘要且服从累计额度。task.progress 的成功 preview SHALL 仅为固定英文计数确认，最多 1 KiB，不回显任务正文、activeForm 或 explanation，沿同一每 call 1 KiB 安全预留及累计额度分配，不建立额外正文预算。计数不是 token window 或完整网络请求大小；实际序列化膨胀 MUST 仍经过 Provider 编译及既有请求检查。

分配 SHALL 按 call index 与已冻结结果计费，为当前 sample 后续 calls 和本 turn 剩余 64-call 上限位置各保留每项 1 KiB 安全结果空间；两者分别约束 sample/turn 剩余额度，不重复扣费。当前 allowance 为单次上限、sample 扣除后续预留的余额、turn 扣除后续预留的余额中的最小值。加减 MUST 检测溢出/下溢；并行完成时间不能改变分配。预算只处理新结果，同轮追加不重置，已 durable 结果不能重新裁剪。

#### Scenario: Bound parallel aggregate output
- **WHEN** 多个只读调用完成顺序不同且各自原始结果都接近单次上限
- **THEN** 相同输入与保存可用性得到同一预算结果，最终 sample previews 合计不超过 512 KiB且按原 call index 提交

#### Scenario: Count the entire preview
- **WHEN** 正文恰好占满 allowance 而还需加入 artifact 引用与省略提示
- **THEN** 正文让出提示空间，最终 bytes 不超限，不能只计正文

#### Scenario: Keep steering in the same budget
- **WHEN** 同 turn 安全点提交两条追加输入后继续工具 sample
- **THEN** 原工具累计使用量保留，不因输入或 UI 操作重新获得 2 MiB

#### Scenario: Count a task confirmation in the existing ledger
- **WHEN** 任务更新与其他工具在同一 sample/turn 生成结果
- **THEN** 固定英文确认计入原 sample 512 KiB、turn 2 MiB 和原 call index 分配，任务状态或 UI 折叠不会重新获得额度



Task 的最多 1 KiB SHALL 仅为该能力最终模型 preview 的单次 cap，不是完整规范 metadata 的大小，也不表示 sample/turn 的全局正文额度已经耗尽。额度查询、初始结果预算、reserve/freeze/finalize、恢复验证 SHALL 使用同一能力 cap 和安全结果预留口径；完整规范 TaskUpdate 仍单独受 64 KiB 及实际 record/native 编码上限限制，不按 1 KiB 裁剪 metadata。全局正文耗尽只由累计池和后续安全预留判断，MUST NOT 因当前 Task 局部 allowance 恰好等于 1 KiB 而提前终止仍有正文额度的 turn 或拒绝后续 Read。

#### Scenario: Do not mistake a small task cap for global exhaustion
- **WHEN** 健康预算中 Task 的局部 allowance 因单次 cap 为 1 KiB，而 sample/turn 累计池仍有充足正文，随后同组包含 Read
- **THEN** 任务成功确认完整计费且只推进一次调用，BodyExhausted 为 false，后续 Read 仍取得按其自身 cap 与原累计池分配的正文额度，不提前结束 turn

#### Scenario: Validate a frozen task budget with the same cap everywhere
- **WHEN** 相同成功或失败任务结果分别经过初始预算、reserve、freeze/finalize、持久化和离线恢复
- **THEN** 所有阶段认定同一 1 KiB preview cap、原 allowance/actual bytes 及唯一扣账事实；错误恢复 allowance 或以完整 metadata bytes 冒充 preview 用量必须拒绝

### Requirement: Budget finalization respects tool semantics

Read SHALL 优先返回原请求范围的既有有界结果；若该结果超过当前 allowance，返回安全 `result_budget_exceeded` 并提示缩小范围，不自动转存或事后默默缩短，完整观察不可用。ReadArtifact SHALL 在实际消费时按额度生成有进度的页，不能先推进游标后裁正文。Glob/Grep SHALL 使用稳定首尾与真实省略统计，并按策略保存本次采集结果。Exec SHALL 按冻结额度生成双流头尾，保存是否需要依据最终额度判断。Patch/Write SHALL 保留实际终态/计数，不因预算改变已发生副作用或缩短审批候选。

#### Scenario: Preserve Write evidence safety
- **WHEN** executor 完整读过文件但最终 Read 因当前预算拒绝
- **THEN** 模型得到有界错误，完整观察与该路径已有观察失效，后续 Write 不借用该次读取授权

#### Scenario: Save output made large by aggregate pressure
- **WHEN** Exec 输出小于 256 KiB 但超过本次冻结 allowance
- **THEN** 输出按私有策略保存且模型预览缩短，不提前以原单次上限判定 not_needed

Skill SHALL 完整返回经过展开及来源包装的主文档或资源全文；超出当前 allowance 时只返回有界 result_budget_exceeded，不裁剪指令、不报告加载成功、不自动保存 artifact 代替缺失正文。

#### Scenario: Reject a partial skill result
- **WHEN** Skill 完整 preview 小于单次上限但超过有序累计分配的 allowance
- **THEN** 返回配对预算错误，没有任何部分成功指令进入 Provider 历史

Task.progress SHALL 保留完整 typed 任务 metadata；它不是可截断模型 preview。成功确认必须能完整放入当前 allowance；完整 raw input、canonical snapshot、结果 metadata、Session record 与 Provider output 的实际编码分别须通过当前上限检查。确认额度不足或完整结果的实际编码超限时，必须生成匹配原调用的安全预算错误，保留旧任务状态，不裁剪任务文字、不省略条目、不改为部分成功，不通过 artifact 绕过限制。只有确定成功 durable 的完整任务结果才生效；恢复不重新裁剪确认或任务快照。

#### Scenario: Refuse an entire task update under budget pressure
- **WHEN** 当前 allowance 不足固定确认，或合法原始任务文字在完整 JSON 编码后超过实际 record/output 上限
- **THEN** 返回配对预算错误且当前清单不变，不截断 metadata 后保存成功，也不把预算错误当作新任务快照

#### Scenario: Restore the frozen task result
- **WHEN** 完整任务结果已 durable 且进程重启时当前预算参数或 UI 状态变化
- **THEN** 使用原任务更新和原确认 bytes 重建状态及原生 output，不重新分配、扣账或渲染成新的模型结果
