## MODIFIED Requirements

### Requirement: Resume validates before tail repair

resume/continue SHALL 使用连续 exclusive lease 只读加载，先完成 replay、Provider 兼容及原生/checkpoint 校验，再显式修复尾部并 Sync，然后启动 writer 和本地 reconciliation。校验失败 MUST 保持包括未完成尾部在内的原始 bytes 不变。Catalog 扫描 MUST NOT 执行尾部修复。修复失败或 durability 不确定 MUST 阻止 writer 和新副作用。

原生记录、计量与 checkpoint 的校验 SHALL 使用恢复请求的取消，至少在相邻记录的有界验证之间观察取消。校验阶段接受取消时，恢复 MUST 作为取消收口并等待原 lease 和其他资源释放。已有格式、关联、计量、回放与 checkpoint 投影的阶段错误优先级 SHALL 按现有入口保留，另行定义的计量对账按其契约处理。

执行接纳前的确定取消或超时 SHALL 以匹配调用的结果收口。尚有未收口的工具组且无法完整写入结果与原生输出时，Runtime MUST 阻止新的副作用并保留可回放的 committed 前缀，将该尾部交给既有恢复补偿，不能提前持久化 `turn_failed`。

同一 turn 在尚有未 started 的只读调用时耗尽正文预算，回放 SHALL 按已提交调用次序重建额度，只有额度已耗尽才能接受未启动调用的有界 `result_budget_exceeded` 结果。其他未启动的普通失败和成功结果仍拒绝。

同一连续只读段的已接纳 started MUST 先于该段任何工具结果。Runtime 主回放、metadata 和活动尾部的恢复检查 SHALL 就此顺序作相同判定。前段完整闭合后，跨独占屏障的下一段可继续执行。若独占调用已确定无法继续且其 Post 或预算收口未完成，Runtime MUST 保存原失败及已提交前缀，不追加跨越未收口调用的取消结果。

无审批的 Task 调用在配置策略要求确认而无法执行时 SHALL 允许匹配当前调用的执行前拒绝 `permission_denied`。回放接受该终态和其原生配对，且不能将其解释为 started、批准或需要恢复执行。保持其他未批准调用的既有严格检查。

#### Scenario: Apply the same read-batch order to every replay surface
- **WHEN** 同一只读段前项已有结果，之后才记录后项 started
- **THEN** 活动尾部、已经配对 outputs 的完整日志和元数据回放均拒绝该次序

#### Scenario: Retain uncertainty with an unfinished exclusive barrier
- **WHEN** 独占调用输出不确定结果，Post 或预算还未收口，工具组仍有后续调用
- **THEN** 返回原失败，后续未准入调用不执行，恢复可继续收口原有前缀

#### Scenario: Replay a Task rejected by an ask policy
- **WHEN** 自定义策略对无交互审批能力的 Task 要求确认
- **THEN** 保存未启动的关联拒绝及其输出，在线结束和后续回放均接受

#### Scenario: Replay budget-rejected calls after an execution barrier
- **WHEN** 同一工具组的前序结果耗尽 turn 正文预算，后续只读调用在执行前收到有界预算错误
- **THEN** 重新加载接受已提交的结果与原生输出，未获接纳的工具不执行且不会在恢复时重试

#### Scenario: Reopen a journal after read-only tool admission fails
- **WHEN** 已有 durable ready，而只读工具的 started 接纳前出现确定超时，或写入故障阻止工具组收口
- **THEN** 未获接纳的工具不执行，保存的 journal 可回放并按规则收口，恢复时不会重执行该工具

#### Scenario: Reject an incompatible session with a partial tail
- **WHEN** 当前模型与 Session 不兼容且文件末尾存在半行
- **THEN** 拒绝恢复，journal 全部 bytes 不变

#### Scenario: Repair before local reconciliation
- **WHEN** committed 前缀及 Provider 完整验证通过，尾部存在未完成 batch
- **THEN** 同一 lease 下先修复并 Sync，再从正确 next seq 本地收口，不释放后重新打开文件

#### Scenario: Cancel while validating restored native records
- **WHEN** HTTP 或 WebSocket 尚未开启，恢复请求在原生回放两个记录的验证之间被取消
- **THEN** 恢复停止后续校验并等待当前 lease 关闭，不执行 repair、writer 接管、补偿或 Provider 请求
