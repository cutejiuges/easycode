# session/output-artifacts Specification

## Purpose

定义命令输出在私有会话存储中的保存、发布、配额、完整性与清理契约，使用户和模型能按需访问已提交结果，同时避免大输出破坏内存边界、会话恢复和跨进程所有权。

## Requirements

### Requirement: Storage layout is isolated from tool access

应用 SHALL 在启动和每次 Exec 启动前，以实际打开的目录身份核对存储根、workspace 与实际 sandbox 读写范围。存储根与 workspace 相同或互为祖先、存储根位于其他 sandbox 可读或可写目录内、目录身份无法确认或复核改变时 MUST 以 `unsafe_storage_layout` 失败关闭，不静默禁用保存或启动命令。权限位与字符串前缀不能替代该校验。

#### Scenario: Reject a home workspace
- **WHEN** workspace 为 home，存储位于 home 下
- **THEN** 启动失败且不接受 turn，不允许命令直接访问内部输出

#### Scenario: Recognize real directory aliases
- **WHEN** 不同路径通过系统别名指向相同或有祖先关系的目录
- **THEN** 校验依据实际句柄识别重叠，相似字符串前缀的独立目录不误拒绝

#### Scenario: Recheck before process start
- **WHEN** 启动后存储或 workspace 被替换而复核失败
- **THEN** Exec 不启动子进程，不扩大 sandbox，不泄漏底层路径

#### Scenario: Reject a sandbox readable storage root
- **WHEN** 存储位于实际 sandbox 可读系统目录内
- **THEN** 启动被拒绝，0700/0600 不被视为足够的隔离证据

### Requirement: Output saving preserves bounded stream prefixes

系统 SHALL 从 Exec 首个输出字节分别暂存 stdout/stderr 原始内容，每流保存连续前缀最多 16 MiB、每 invocation 最多 32 MiB。内存头尾和模型预览预算保持独立；只有两个流均确认 EOF 且可在现有模型预算内完整表示时才清除暂存而报告 not_needed，否则发布非空流的保存内容。每流 SHALL 返回 not_needed、complete、partial 或 unavailable；只有完整观察到 EOF 且保存所有 bytes 才能声明 complete。partial MUST 明确 observed/saved bytes 与原因，unavailable MUST 无可读引用。未确认 EOF 且没有可发布非空前缀时 SHALL 为 unavailable；已经确认 EOF 且可完整内联的小输出，即使保存预留失败，仍 SHALL 为 not_needed。

#### Scenario: Save content omitted from memory
- **WHEN** 命令产生超过内存预算但低于保存预算的输出并正常关闭流
- **THEN** artifact 包含该流全部原始 bytes，模型预览仍有界并可引用 complete 对象

#### Scenario: Stop saving at the stream limit
- **WHEN** stdout 超过 16 MiB 且持续输出
- **THEN** 磁盘只保存前 16 MiB，持续排空和统计，状态为 partial，内存和磁盘均不超限

#### Scenario: Keep small output inline
- **WHEN** 两个流均确认 EOF 且可在 256 KiB 总模型预览内完整表示
- **THEN** 暂存被清理，结果为 not_needed，不留下无用对象

#### Scenario: Fail saving without stopping draining
- **WHEN** 写入或 Sync 发生确定性故障而命令继续输出
- **THEN** 系统继续排空，能可靠发布的前缀为 partial，否则 unavailable；进程结果不因保存失败被伪造

### Requirement: Quotas are enforced across processes

每 thread 保存预算 SHALL 为 256 MiB，每存储根为 1 GiB。开始保存前 SHALL 跨进程原子预留每 invocation 的 32 MiB，已保存、暂存与未释放的预留均计入有效占用且不重复计费。不足时 MUST 报告 quota_exceeded，不删除既有引用、不以无界内存替代。崩溃后的预留 SHALL 保守对账，不能因 owner 退出忽略仍存在的数据 bytes。

#### Scenario: Compete for the last reservation
- **WHEN** 两个真实进程在根目录只剩一个预留额度时同时请求保存
- **THEN** 最多一个成功，另一个报告 quota_exceeded，总有效占用不超过根预算

#### Scenario: Recover crashed reservations
- **WHEN** 进程写入暂存后崩溃并释放 OS 所有权
- **THEN** 后续对账保留实际文件占用，无法验证的状态保守拒绝新保存，不超卖预算

#### Scenario: Retain committed output when full
- **WHEN** thread 或根预算耗尽
- **THEN** 历史已引用内容不被回收，新命令继续使用有界预览并报告未保存原因

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

### Requirement: Storage is private and verified on opened handles

目录和文件 SHALL 分别使用 0700/0600 私有权限，并在实际打开的每级目录/文件句柄上验证权限、类型和 containment；不得跟随 symlink、接受非普通对象或外来硬链接。保存对象 SHALL 不覆盖已有对象。读取 SHALL 在同一 FD 上验证长度与完整 digest 后才返回正文，禁止单独 Lstat 后按不可信路径 Open。缺失返回 unavailable，内容不匹配返回 corrupt；诊断、错误与 fixture MUST 不含真实输出或配置凭据。

#### Scenario: Reject a swapped path
- **WHEN** 故障注入在目录遍历期间将组件替换为 symlink 或非普通文件
- **THEN** 操作拒绝或继续使用已持有的安全句柄，根外文件不被读取或修改

#### Scenario: Detect modified content
- **WHEN** 保存对象内容被更改但长度相同
- **THEN** 回读返回 corrupt 且不返回任意正文，原 journal 与 native history 不变

### Requirement: Cleanup requires thread ownership and validated facts

已引用对象 SHALL 随 Session 保留，不自动过期。只有持有对应 thread 独占 lease 且 journal 全量校验/必要尾部修复完成后，才允许清理该 thread 无 durable 引用的暂存或孤儿；不得跨 thread 删除或把损坏 journal 当成无引用。未完成保存清理前 MUST 保留资源所有权；超时不是 done。

#### Scenario: Leave another thread untouched
- **WHEN** 恢复 thread A 并发现 thread B 有旧暂存
- **THEN** 只处理 A 的安全孤儿，B 文件 bytes 保持不变

#### Scenario: Reject cleanup without ownership
- **WHEN** 另一个真实进程占有 thread lease 或 journal 校验失败
- **THEN** 本次启动不读取后修复或清理 artifact，既有 bytes 不变

#### Scenario: Wait for blocked storage cleanup
- **WHEN** 写入阻塞且 shutdown 达到期限
- **THEN** owner 保留句柄、reservation 与 lease；阻塞解除后完成唯一清理并发出最终 done
