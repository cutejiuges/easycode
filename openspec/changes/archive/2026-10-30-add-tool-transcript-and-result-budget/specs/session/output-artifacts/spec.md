## MODIFIED Requirements

### Requirement: Output saving preserves bounded stream prefixes

系统 SHALL 从 Exec 首个输出字节分别暂存 stdout/stderr 原始内容，每流保存连续前缀最多 16 MiB、每 invocation 最多 32 MiB。内存头尾和模型预览预算保持独立；只有两个流均确认 EOF 且可在本次已冻结实际模型 allowance内完整表示时才清除暂存而报告 not_needed，否则发布非空流的保存内容。每流 SHALL 返回 not_needed、complete、partial 或 unavailable；只有完整观察到 EOF 且保存所有 bytes 才能声明 complete。partial MUST 明确 observed/saved bytes 与原因，unavailable MUST 无可读引用。未确认 EOF 且没有可发布非空前缀时 SHALL 为 unavailable；已经确认 EOF 且可完整内联的小输出，即使保存预留失败，仍 SHALL 为 not_needed。

#### Scenario: Save content omitted from memory
- **WHEN** 命令产生超过内存预算但低于保存预算的输出并正常关闭流
- **THEN** artifact 包含该流全部原始 bytes，模型预览仍有界并可引用 complete 对象

#### Scenario: Stop saving at the stream limit
- **WHEN** stdout 超过 16 MiB 且持续输出
- **THEN** 磁盘只保存前 16 MiB，持续排空和统计，状态为 partial，内存和磁盘均不超限

#### Scenario: Keep small output inline
- **WHEN** 两个流均确认 EOF 且可在 本次不超过 256 KiB 的实际总模型预览额度内完整表示
- **THEN** 暂存被清理，结果为 not_needed，不留下无用对象

#### Scenario: Fail saving without stopping draining
- **WHEN** 写入或 Sync 发生确定性故障而命令继续输出
- **THEN** 系统继续排空，能可靠发布的前缀为 partial，否则 unavailable；进程结果不因保存失败被伪造

系统 SHALL 对 Glob/Grep 以独立 search_result 来源增量暂存本次选中结果的确定性文本，保存位置在预览单行/总预算裁剪之前，单调用最多 16 MiB，UTF-8 对象在完整字符边界结束。既有扫描/遍历/输入 limit 保持不变，不为保存扩展搜索范围。采集覆盖与保存完整性 MUST 分开表达；complete 仅表示采集文本全部保存，不代表查询空间完整。Read、ReadArtifact、Patch、Write MUST NOT 自动产生保存对象。

#### Scenario: Save a limited search completely
- **WHEN** Grep 命中调用 limit 且全部选中内容成功保存但预览被缩短
- **THEN** 保存状态为 complete，同时明确查询覆盖受 limit 限制，不能声称搜索全部完成

#### Scenario: Retain capture until final budget is known
- **WHEN** Exec 原输出小于 256 KiB 但超出本次实际 allowance
- **THEN** 不得提前删除暂存，结果引用按最终预算判断必要的保存内容

### Requirement: Quotas are enforced across processes

每 thread 保存预算 SHALL 为 256 MiB，每存储根为 1 GiB。开始保存前 SHALL 跨进程原子预留Exec 每 invocation 的 32 MiB 或 Glob/Grep 每 invocation 的 16 MiB，已保存、暂存与未释放的预留均计入有效占用且不重复计费。不足时 MUST 报告 quota_exceeded，不删除既有引用、不以无界内存替代。崩溃后的预留 SHALL 保守对账，不能因 owner 退出忽略仍存在的数据 bytes。

#### Scenario: Compete for the last reservation
- **WHEN** 两个真实进程在根目录只剩一个预留额度时同时请求保存
- **THEN** 最多一个成功，另一个报告 quota_exceeded，总有效占用不超过根预算

#### Scenario: Recover crashed reservations
- **WHEN** 进程写入暂存后崩溃并释放 OS 所有权
- **THEN** 后续对账保留实际文件占用，无法验证的状态保守拒绝新保存，不超卖预算

#### Scenario: Retain committed output when full
- **WHEN** thread 或根预算耗尽
- **THEN** 历史已引用内容不被回收，新命令继续使用有界预览并报告未保存原因

reservation SHALL 使用唯一当前严格来源与额度结构，拒绝旧 shape 和任意额度；复用现有共享配额与安全存储，而不是新增搜索私有配额。只读批次的搜索预留 MUST 由 owner 在 started durable 后按 call index 顺序进行，再把 capture 所有权交给 worker。其他进程可影响保存可用性，但本批次 worker 完成顺序不能决定预留顺序。

#### Scenario: Reserve search slots in call order
- **WHEN** 同一批次两个搜索竞争最后一个 16 MiB thread 额度且后一个更快完成
- **THEN** owner 按 call index 分配预留，超额者保留有界预览并报告 unavailable，不删除已引用对象

### Requirement: Durable references authorize immutable output

可访问 descriptor SHALL 绑定不透明 artifact ID、thread、invocation、stream、保存长度、完整性 digest 与 coverage，不包含宿主路径。只有对象内容与发布目录成功 Sync 且对应 result 成为 durable fact 后才可授权访问。冻结 preview 和 descriptor 不得因后续文件缺失、预算或显示策略变化而重算；后续 Provider sample SHALL 等待配对 native output 提交。

#### Scenario: Crash after publication before result
- **WHEN** 文件已发布但 result 尚未 durable 时进程崩溃
- **THEN** 文件不授予读取权限，恢复按既有 ledger 处理，不依据文件推断执行成功或重执行命令

#### Scenario: Crash after result before native output
- **WHEN** result 已 durable 而 native output 尚未提交
- **THEN** 恢复仅使用冻结结果补齐配对 output，不打开 artifact，预览 bytes 与不中断路径相同

#### Scenario: Reject an uncommitted object
- **WHEN** 调用方提供实际存在但没有当前 thread durable 引用的 artifact ID
- **THEN** 读取返回安全 unauthorized，不能泄漏内容或宿主路径

来源 SHALL 为封闭 stdout/stderr/search_result，前两者仅属于 Exec，后者仅属于 Glob/Grep。descriptor 与冻结 result MUST 验证能力、来源、归属、长度和完整性一致。保存错误、取消和 shutdown SHALL 复用唯一 cleanup owner；阻塞操作完成前不释放 reservation、store 或 thread lease。

#### Scenario: Authorize a search object only after result Sync
- **WHEN** 搜索对象已发布而 result Sync 失败或无法确认
- **THEN** 不授权读取，无法确认提交时 poison；孤儿仅在持有 thread lease 且 journal 校验后清理
