## Purpose

定义仓库完整测试与质量门的执行集合、资源边界、结果可观测性及性能验收口径，使本地命令、Git hooks 和 CI 在保留业务与安全覆盖的前提下，以可复现的方式完成普通测试和竞态测试。

## ADDED Requirements

### Requirement: Verification preserves the complete test contract

完整质量门 SHALL 执行分支策略测试、格式检查、vet、Staticcheck、架构边界、当前平台的全部普通测试与经审计的必要 race 测试。本地、pre-commit、pre-push 与 CI MUST 复用同一质量门定义。性能验收 MUST 强制重新执行测试，不能以测试结果缓存命中、仅运行修改包、未经审计排除慢测试、关闭 race 或缩减普通测试断言证明达标。

#### Scenario: Match the platform test inventory
- **WHEN** 完整质量门采用分片或有界并行执行测试
- **THEN** 普通阶段覆盖当前平台所有包、Test、Example 和默认 fuzz seed；race 默认覆盖相同集合，仅移出有明确理由的已审计串行入口
- **THEN** 分片集合不重叠且不遗漏，包内子测试仍由原顶层测试执行
- **THEN** 沿用既有平台限制的 skip 必须可见；race 排除清单另行报告，不能伪装为通过或改变普通集合

#### Scenario: Keep hooks and CI equivalent
- **WHEN** 同一提交通过本地、Git hooks 或 CI 触发完整质量门
- **THEN** 检查项目和覆盖范围相同，环境参数只调整有界资源分配

#### Scenario: Overlap static checks without weakening the gate
- **WHEN** 静态检查与普通测试交错执行
- **THEN** 两者共享同一活动命令上限，且全部成功后才进入 race
- **THEN** 静态失败使完整质量门失败，并由同一 owner 停止派发和回收工作
- **THEN** 总墙钟包含重叠区间，不重复相加或扣除静态检查时间

### Requirement: Test work has bounded resources and owned cleanup

测试执行入口 SHALL 显式限制活动测试进程与包内并发，避免普通和 race 阶段因外层并行调用无界叠加。使用进程级环境变量或工作目录的测试 MUST 保持隔离。执行失败、超时或用户中断 SHALL 返回失败状态、停止派发新工作，并由唯一 owner 取消和等待已启动的工作，保留能够定位失败的输出。

#### Scenario: Isolate environment-sensitive scenarios
- **WHEN** 多个测试需要设置不同的 HOME 或其他进程级状态
- **THEN** 它们在互相隔离的进程中运行或按原语义串行执行
- **THEN** 不能为了提速在这些测试上引入不安全的同进程并行

#### Scenario: Fail a shard without losing its workers
- **WHEN** 任一分片发生断言失败、非零退出、超时或执行入口收到取消
- **THEN** 整体质量门失败，指出对应阶段和分片，保留已完成和未完成的范围
- **THEN** 已启动进程得到取消并完成回收，不留下后台测试继续占用资源

### Requirement: Race selection remains explicit and reviewable

默认 race SHALL 保留真实并发、共享状态、取消/提交交错、资源关闭、stream、writer、lease 与恢复生命周期。只验证串行值、字节、大小或静态源码的测试 MAY 从默认 race 移出，但 MUST 保留普通测试和所有原断言，并以精确包/测试名记录理由及相关保留覆盖。新增测试 MUST 默认进入 race，手动全量 race MUST 可用。

#### Scenario: Audit a race exclusion
- **WHEN** 已审核的串行测试或完整数据矩阵退出默认 race
- **THEN** 报告列出完整清单、所选集合、排除项与理由，普通测试仍完整执行
- **THEN** 复杂矩阵的并发/生命周期代表场景沿用原 helper 和实质断言
- **THEN** 清单重复、无效名称或替代覆盖被排除必须使质量门失败

#### Scenario: Include new and manually requested coverage
- **WHEN** 新测试尚未审核，或用户选择手动全量 race
- **THEN** 新测试默认纳入，手动全量模式不应用排除清单

### Requirement: Performance reporting measures the whole workflow

完整质量门 SHALL 记录提交、平台、Go 版本、CPU/内存、并发配置、命令、退出码、竞争工作负载、完整普通与所选 race 的耗时和执行集合。计时包含编译/链接及静态检查。测试结果缓存 MUST 禁用；正常构建缓存允许复用。默认 MUST NOT 因固定的总耗时阈值中断质量门，单进程超时、业务 deadline 与取消/失败清理保持有效。

#### Scenario: Record reproducible timings without a hard performance gate
- **WHEN** 同一最终实现连续两轮运行完整质量门
- **THEN** 完整普通和所选 race 均通过，报告阶段、合计时间及覆盖变化
- **THEN** 耗时超过历史 300/360 秒目标不会单独判定失败，历史超时记录不被追认为成功

#### Scenario: Opt into a diagnostic budget
- **WHEN** 调用方显式设置正值总预算，或发生失败、中断或测试进程超时
- **THEN** 达到所选限制时保留原取消与进程回收语义，返回失败并记录未完成范围

### Requirement: Refactoring preserves assertions and data semantics

测试重构 SHALL 以业务断言与故障边界为单位保留覆盖。合并重复测试前 MUST 记录原断言到保留测试的映射；真实子进程竞争、崩溃释放、故障注入、当前固定 fixture、双 Provider 原生恢复与 cache 等价不得被空实现或仅检查返回成功的测试取代。共享编解码性能重构 MUST 保持现有严格拒绝规则、当前固定 bytes、原生顺序、checksum 与 fingerprint。

#### Scenario: Optimize repeated decoding
- **WHEN** 对重复扫描、临时字段对象或解码装配进行性能优化
- **THEN** 非法 Unicode、重复/未知字段、大小写别名、尾随值、缺失必需字段与非法 null 的拒绝断言仍成立
- **THEN** 合法的当前 fixture 和 uninterrupted/restored 请求保持逐字节与语义等价

#### Scenario: Consolidate equivalent test setup
- **WHEN** 多个用例共享测试装配或将纯逻辑矩阵下沉到所属包
- **THEN** 原有边界场景具有明确覆盖映射，并保留必要的真实 I/O 集成验证
- **THEN** 测试代码行数减少本身不作为覆盖充分或性能达标的依据

#### Scenario: Reuse record encoding without changing bytes
- **WHEN** 写入方复用已编码的 checksum envelope 生成最终 Session record
- **THEN** 保留全部输入拒绝规则、完整 record 大小检查、原字段顺序和 SHA-256 计算范围
- **THEN** 与原 Sonic 整体编码路径的差异回归、固定 golden 和当前 fixture 均证明输出及 checksum 逐字节一致

#### Scenario: Reuse Hook encoding without changing validation or identity
- **WHEN** HookToolInput、HookInputEffect 与 HookFinished 内部复用当前调用的编码字节
- **THEN** 保留全部输入校验、大小限制、错误分类、字段顺序、escaping、canonical bytes 与 digest，不新增公开绕过入口或全局缓存
- **THEN** 与原 Sonic 嵌套编码的差异回归、坏输入与受限 fuzz 证明等价，完整普通/race、固定 fixture 和双 Provider 恢复/cache 继续通过
- **THEN** 不修改 Sonic 或 Go race 的检测机制，不以单项 profile 提速代替完整性能验收
