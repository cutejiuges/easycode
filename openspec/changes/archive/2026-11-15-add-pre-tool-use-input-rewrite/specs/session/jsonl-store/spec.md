## ADDED Requirements

### Requirement: Pre hook input effects are strict required execution evidence

唯一当前 PreToolUse `hook_execution_finished` SHALL 在现有运行统计和空反馈之外保存必需封闭 input_effect。unchanged 分支仅含 kind/before_digest/after_digest 且二者相同；replaced 分支还必须含同能力完整 typed input，其规范摘要等于 after 且不同于 before。摘要 SHALL 绑定能力与规范参数；before MUST 等于原 ready 或已保存连续变化前缀推导的检查候选 C 摘要。

replaced SHALL 仅属于正常 execution-origin、exit 0、continue/ok、cleanup confirmed、非取消/超时的真实 Pre 运行。失败和 recovery finished 只能 unchanged；PostToolUse/PostToolUseFailure/Stop MUST 拒绝 Pre effect 字段并保留各自反馈结构。所有构造、decoder、validator、clone 和 replay MUST 严格校验事件、target、plan、ordinal、输入、大小及摘要关联。

保存和恢复 SHALL 都核验替换 canonical 对象最多 16 KiB、完整 finished 沿用当前 64 KiB，以及从 O 派生的 facade profile。不得因为输入在磁盘中使用未转义字符、对象排列不同或 hash 正确，就接受规范展开超限或匹配模式变化的参数。

原 ready SHALL 继续保存与 native sample 一致的 O；检查候选 C 只由该调用已保存 effect 前缀纯内存推导，只有完整 continue 决定才选择 E。没有 E 的 gate 非执行结果按 O 关联，选中后即使准备失败也按 E 关联。不得新增重复最终输入 record、原始 stdout/stdin、reason、可执行命令配置、信任/grant 或格式版本。替换参数作为必要执行事实保存，不得作为未知输出透传通道。此处必要 typed 参数以及按真实准入冻结的有效输入说明，是既有“Write 结果不回显正文”和“工具 gate 统计不承载正文”的明确有界例外；其内容、大小与来源严格遵循 extension/pre-tool-use-input-rewrite，不包括额外采集的磁盘快照、diff 或原始脚本输出。受影响 current fixtures SHALL 固定为唯一当前 bytes，缺失 effect 的旧 Pre finished 在 repair 前拒绝，不建立兼容 reader 或迁移。

#### Scenario: Record one accepted change with its result
- **WHEN** 正常 Pre 提出合法且不同的完整输入
- **THEN** 同一个 finished 原子保存真实统计、空反馈及匹配摘要的 replaced effect；无需新 record kind

#### Scenario: Reject a forged effect chain
- **WHEN** checksum 正确但 before/after、能力、plan/ordinal 或完整输入与已保存前缀不符
- **THEN** 整体拒绝，零 repair/append/工具/网络；即使另有半行也保持 journal bytes 不变

#### Scenario: Reject replacement attached to failure or another event
- **WHEN** recovery/非零/取消的 Pre 含 replaced，或 Post/Stop 携带 input_effect
- **THEN** 当前 decoder/replay 拒绝，不恢复部分有效输入

#### Scenario: Replace current fixtures without another reader
- **WHEN** 加载固定的当前正常/无变化/替换/拒绝/损坏 fixture 和缺失必需 effect 的旧 shape
- **THEN** 当前合法 bytes 通过，旧 shape 在 repair 前失败，测试运行时不由当前 encoder 生成输入 fixture

#### Scenario: Reject a bounded-on-disk but expanded parameter
- **WHEN** checksum 和前后摘要正确，磁盘字段用较短表示保存了 canonical 展开超过 16 KiB 的输入
- **THEN** 恢复在 repair 前拒绝，不绕过规范大小或扩大 finished decoder 上限

#### Scenario: Keep rejected candidate metadata separate
- **WHEN** C 的 workdir/目标改变后，下一 Pre 项拒绝且 gate 未 continue
- **THEN** 候选仅作审计，门禁结果按 O 的非执行 metadata 验证；冒用 C 或伪造普通批准/started 的记录拒绝

### Requirement: Effective input recovery never reruns hooks or tool effects

Pre 的计划、started、finished、gate 及拒绝 result SHALL 保持现有 batch grammar：中间通过项 finished 单批，最后通过项与 gate continue 同批，实际阻断/错误/取消项 finished、gate/result 同批。系统只能在 finished 成功 Sync 后更新 C，在 gate continue 后建立 E 并进入普通候选/审批。无法确认提交时 MUST poison 并停止后续接收。

恢复 SHALL 从 O 加完整已验证 effect 前缀推导 C，结合实际 gate 决定是否存在 E，按相应分支校验审批 metadata、result、说明模式与后置事实；O 仍对照 native sample。未 started 的下一项只关闭旧 gate/call，started 无 finished 仅对该项补 recovery uncertain/unchanged；没有 continue 就没有 E，均不安装未保存输出、不读取配置/脚本或准备工具。gate continue 后无批准、批准后无 started、started 无 result 继续使用既有取消或不确定补偿；短说明不能伪装正常已准入结果。已有 durable result 原字节复用，output 缺口按原 call index 补齐。

已经确定完成的 Hook 结果 SHALL 在其原操作的必要完成批次收口，不被晚到取消改写；最终正常项仍与唯一 continue 同批。全通过后再出现 gate cancelled、同一次真实结果被保存为 recovery uncertain、已选中但冒用 gate_rejected 的 metadata 均是 corruption。取消只阻止尚未接收的后续检查/普通执行。

lease SHALL 连续覆盖验证、repair、补偿、writer Sync/Close；取消线性化、失败 poison、关闭超时持有 owner 规则保持。重复崩溃不能重跑脚本/工具、丢失已保存变化或重复结果/预算。完整语义错误必须在任何尾部 repair 前拒绝。

#### Scenario: Recover after an intermediate replacement
- **WHEN** 第 0 项 replaced finished 已保存，进程在下一项 started 前退出
- **THEN** 保留变化前缀并本地关闭旧调用，零配置/脚本读取、零候选/进程/文件副作用

#### Scenario: Recover an unfinished rewrite attempt
- **WHEN** 第 1 项 started 已保存但没有 finished，之前已有有效替换
- **THEN** 仅第 1 项补 recovery uncertain/unchanged，工具取消，未保存替换不会安装或重跑

#### Scenario: Recover after approval without execution
- **WHEN** 有效候选 allow 已保存而 tool started 缺失
- **THEN** 使用有效调用补 session_interrupted_before_execution，不复用批准、grant 或重新询问

#### Scenario: Recover a finished tool before native outputs
- **WHEN** 改写后的真实 result 已保存，Post 或 native outputs 尚未闭合
- **THEN** 复用冻结 preview/metadata 与既有后置补偿，按原调用顺序补 outputs，零工具重跑

#### Scenario: Retain writer ownership during recovery
- **WHEN** 恢复清理或 Sync 被确定性阻塞，另一真实进程竞争同一 thread
- **THEN** 竞争者 session_busy 且不 repair；原 owner 完成 Sync/Close 后才释放，续写 seq 单调且失败 bytes 不变

#### Scenario: Reject cancellation that rewrites a completed gate
- **WHEN** 最后正常 finished/continue 已完整保存，随后又记录 gate cancelled 或将其当作未完成恢复
- **THEN** 语义回放拒绝；合法晚到取消只能在原已选输入的普通执行路径中收口

#### Scenario: Preserve the selected input before ordinary approval
- **WHEN** E 的 workdir/目标与 O 不同，gate continue 已保存而普通批准缺失
- **THEN** 按 E 补非执行取消和短说明，不能因没有工具 started 而回退 O 或恢复 C 执行

#### Scenario: Reject a recovery note that hides a normal result
- **WHEN** 有合法准入及正常实际 result，却用恢复短说明替换完整 E 或伪造恢复预算
- **THEN** 在 repair 前拒绝；合法缺口补偿与原冻结结果必须按真实来源区分
