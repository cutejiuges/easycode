## Purpose

定义仓库完整测试与质量门的执行集合、资源边界、结果可观测性及性能验收口径，使本地命令、Git hooks 和 CI 在保留业务与安全覆盖的前提下，以可复现的方式完成普通测试和竞态测试。

## ADDED Requirements

### Requirement: Verification preserves the complete test contract

完整质量门 SHALL 执行分支策略测试、格式检查、vet、Staticcheck、架构边界、当前平台的全部普通测试与全部 race 测试。本地、pre-commit、pre-push 与 CI MUST 复用同一质量门定义。性能验收 MUST 强制重新执行测试，不能以测试结果缓存命中、仅运行修改包、排除慢包、关闭 race 或缩减安全断言证明达标。

#### Scenario: Match the platform test inventory
- **WHEN** 完整质量门采用分片或有界并行执行测试
- **THEN** 普通与 race 阶段分别覆盖当前平台默认测试集合中的所有包、顶层测试、Example 和默认执行的 fuzz seed
- **THEN** 分片集合不重叠且不遗漏，包内子测试仍由原顶层测试执行
- **THEN** 沿用既有平台限制的 skip 必须可见，不能新增性能用途的 skip

#### Scenario: Keep hooks and CI equivalent
- **WHEN** 同一提交通过本地、Git hooks 或 CI 触发完整质量门
- **THEN** 检查项目和覆盖范围相同，环境参数只调整有界资源分配

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

### Requirement: Performance acceptance measures the whole workflow

性能验收 SHALL 记录提交、平台、Go 版本、CPU/内存、并发配置、命令、退出码、是否存在竞争工作负载及计时范围。在 Go 工具链与依赖已准备好的参考环境中，从完整质量门开始至结束的墙钟时间 MUST 小于 300 秒，且同时包含普通与 race、测试编译/链接以及静态质量检查。测试结果缓存 MUST 被禁用；允许使用正常构建缓存，但必须记录其状态。纯测试两阶段合计时间 SHALL 同时报告，不能把每阶段各五分钟误作总流程五分钟。

#### Scenario: Accept a reproducible five-minute run
- **WHEN** 同一最终提交在无其他重测试竞争的参考环境中连续执行两次完整质量门
- **THEN** 两次均通过，均重新运行全部测试，且各自总墙钟时间小于 300 秒
- **THEN** 报告同时包含普通/race 分阶段时间、慢包/慢测试、实际覆盖集合和资源记录

#### Scenario: Expose a performance miss
- **WHEN** 性能验收超过 300 秒，或无法取得目标平台的实际执行证据
- **THEN** 报告明确标记未达标或未验证，并保存计时与失败证据
- **THEN** 不得以增加超时、缓存命中或交叉编译替代运行验收

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
