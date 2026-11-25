## MODIFIED Requirements

### Requirement: Verification preserves the complete test contract

完整质量门 SHALL 执行全仓测试规模硬校验、分支策略测试、格式检查、vet、Staticcheck、架构边界、当前平台精简后现存的全部普通测试与正向清单中的关键 race 测试。本地手动执行、pre-commit 与 CI MUST 复用同一 `make verify` 定义；仓库 MUST 仅保留 pre-commit 作为本地完整验证 Git hook，不保留重复的 pre-push。性能验收 MUST 强制重新执行测试，不能以测试结果缓存命中、仅运行修改包、遗漏现存测试、关闭关键路径 race 或把未运行入口伪装成通过。原生交互冒烟 SHALL 作为独立的必要交付检查，不要求每次 pre-commit 重复完整 PTY 矩阵。

#### Scenario: Match the platform test inventory
- **WHEN** 完整质量门采用分片或有界并行执行测试
- **THEN** 普通阶段覆盖当前平台所有包、Test、Example 和默认 fuzz seed；race 只执行有明确并发/生命周期理由的关键入口
- **THEN** 分片集合不重叠且不遗漏，包内子测试仍由原顶层测试执行
- **THEN** 沿用既有平台限制的 skip 必须可见；未选择的 race 入口另行报告，不能伪装为通过或改变普通集合

#### Scenario: Keep hooks and CI equivalent
- **WHEN** 同一源码快照通过本地手动执行、pre-commit 或 CI 触发基础质量门
- **THEN** 检查定义相同，环境参数只调整有界资源分配，平台差异显式报告
- **THEN** pre-commit 的分支检查通过后执行一次完整质量门；随后 push 不通过仓库 pre-push 再运行一遍
- **THEN** CI 独立验证实际提交，不能依赖本地成功状态跳过

#### Scenario: Overlap static checks without weakening the gate
- **WHEN** 静态检查与普通测试交错执行
- **THEN** 两者共享同一活动命令上限，且全部成功后才进入 race
- **THEN** 静态失败使完整质量门失败，并由同一 owner 停止派发和回收工作
- **THEN** 总墙钟包含重叠区间，不重复相加或扣除静态检查时间

### Requirement: Refactoring preserves assertions and data semantics

测试重构 SHALL 以关键业务断言与故障边界为单位保留覆盖。允许删除重复成功链路、普通值/构造/getter 断言、无消费者的测试装配、实现镜像及过度数据组合，并以简短组级说明记录删除依据和实际承接入口；不要求保留每个旧排列。真实子进程竞争、崩溃释放、故障注入、当前固定 fixture、双 Provider 原生恢复与 cache 等价不得被空实现或仅检查返回成功的测试取代。共享编解码性能重构 MUST 保持现有严格拒绝规则、当前固定 bytes、原生顺序、checksum 与 fingerprint。负向回归 MUST 实际到达目标故障边界，不能因无关前置错误通过。

#### Scenario: Optimize repeated decoding
- **WHEN** 对重复扫描、临时字段对象或解码装配进行性能优化
- **THEN** 非法 Unicode、重复/未知字段、大小写别名、尾随值、缺失必需字段与非法 null 的拒绝断言仍成立
- **THEN** 合法的当前 fixture 和 uninterrupted/restored 请求保持逐字节与语义等价

#### Scenario: Consolidate equivalent test setup
- **WHEN** 多个用例共享测试装配或将纯逻辑矩阵下沉到所属包
- **THEN** 当前选定的关键行为保留实质断言，并保留必要的真实 I/O 集成验证
- **THEN** 测试代码行数采用同一范围净减少统计，不能靠压行、搬文件或跳过现存入口达标；精简不声称与原穷举集合等覆盖
- **THEN** 共享机制集中验证，各适配器保留真实接线、协议差异与必要固定字节对照，不为测试暴露生产内部接口

#### Scenario: Reject a false-positive negative regression
- **WHEN** 负向测试旨在证明伪造输入在指定边界被拒绝
- **THEN** 对照输入先能通过该边界，负向变体只改变相关故障条件；允许复用已有有效对照，不要求每个负向分支再增加一份正向测试
- **THEN** 缺失身份等无关前置错误或测试自制逻辑的自我验证，不能替代对生产行为的断言

#### Scenario: Reuse record encoding without changing bytes
- **WHEN** 写入方复用已编码的 checksum envelope 生成最终 Session record
- **THEN** 保留全部输入拒绝规则、完整 record 大小检查、原字段顺序和 SHA-256 计算范围
- **THEN** 与原 Sonic 整体编码路径的差异回归、固定 golden 和当前 fixture 均证明输出及 checksum 逐字节一致

#### Scenario: Reuse Hook encoding without changing validation or identity
- **WHEN** HookToolInput、HookInputEffect 与 HookFinished 内部复用当前调用的编码字节
- **THEN** 保留全部输入校验、大小限制、错误分类、字段顺序、escaping、canonical bytes 与 digest，不新增公开绕过入口或全局缓存
- **THEN** 差异回归、坏输入、必要受限 fuzz、固定 golden 和当前 fixture 证明字节与语义等价；历史优化已验收后，持续回归可以由能检出同一关键风险的固定当前字节用例承接，不要求永久保存旧 encoder 实现镜像
- **THEN** 不修改 Sonic 或 Go race 的检测机制，不以单项 profile 提速代替完整性能验收

## ADDED Requirements

### Requirement: Tests are admitted only for critical behavior and independent risk

仓库 SHALL 长期采用保守克制的测试风格，只为关键业务链路、必要契约节点、已知缺陷以及高风险并发、持久化、安全或资源所有权边界维护实质回归。新增测试 MUST 说明独立检出的风险和现有覆盖缺口，优先复用或扩充现有用例；没有独立价值的 getter、字段透传、实现镜像和排列组合 MUST NOT 因新增功能、函数或比例尚有余量而引入。测试支持代码 SHALL 具有明确消费者并减少净维护成本，不为假想扩展建设框架。

#### Scenario: Reuse existing risk coverage
- **WHEN** 新功能或修复与既有用例具有相同机制和故障边界
- **THEN** 扩充必要输入或复用既有装配，新增独立整套矩阵必须具有不同的关键风险依据
- **THEN** 不以覆盖率、测试数量或每函数配测试作为验收目标

#### Scenario: Preserve a minimal real regression
- **WHEN** 已知缺陷、必要接线或高风险边界尚无有效用例
- **THEN** 增加能实际检出该问题的最小代表性回归，并记录必要耗时
- **THEN** 仍须通过全仓规模约束，不能以新增风险为由自动放宽阈值

### Requirement: Repository test size obeys a persistent three-to-two gate

仓库 SHALL 对被验证的完整源码快照执行业务 Go 与测试 Go 的物理行数约束 `2 × P >= 3 × T`。该要求 MUST 作为本次交付及后续 `make verify` 的持续硬门禁；独立 `make test-ratio` SHALL 使用相同统计实现。T MUST 包含所有 `_test.go` 及测试专属 Go 支持代码，P MUST 排除这些代码以及固定 fixture、第三方或生成产物；统计 SHALL 包含所有平台的源码及本地未忽略的新文件，不依赖当前 build tag 或本次增量。

测试专属 Python/Shell 脚本、冒烟、runner、度量工具及支持代码 SHALL 单列总行数并受一个版本管理的总预算约束；fixture 的数量及字节另列。质量门 MUST 只读检查，不自动刷新预算，不提供降低阈值、仅告警、包级豁免或按 PR 重置基线的执行选项。脚本预算增长 MUST 在对应已批准的变更中说明必要风险和成本。搬文件、改名、压行、生成测试、扩大排除或增加无业务价值的生产代码 MUST NOT 被用作达标手段。

#### Scenario: Enforce the exact ratio boundary
- **WHEN** 检查得到 `2 × P = 3 × T`
- **THEN** 比例检查通过
- **WHEN** 检查得到 `2 × P < 3 × T`
- **THEN** 检查非零退出，报告 P、T、实际比例、允许的测试上限及超额行数，不能由显示舍入判为通过

#### Scenario: Count test support without changing the denominator
- **WHEN** 公共测试 helper 从 `_test.go` 移到测试专属普通 Go 文件
- **THEN** 这些行仍属于 T，不增加 P；同一文件快照在不同宿主平台得到相同计数
- **THEN** 未忽略的新测试文件不能因尚未加入 Git index 而被漏计

#### Scenario: Reject script growth and accounting evasion
- **WHEN** 测试脚本总量超过已验收预算
- **THEN** 质量门失败且预算文件不被修改
- **WHEN** 测试在 Go、脚本或数据文件之间迁移
- **THEN** 评审同时记录两侧变化及实际承接入口；新增可执行测试逻辑仍需纳入适当统计，不能伪装为 fixture 排除

#### Scenario: Keep the target when simplification is insufficient
- **WHEN** 精简尚未达到 3:2，或删除业务代码后最终比例跌破门槛
- **THEN** 该快照不能作为通过质量门的完成交付
- **THEN** 不自动下调门槛或删除独立关键风险保障；需要改变既定取舍时提交明确的规范变更

### Requirement: Critical terminal interactions run in native automation

关键真实交互 SHALL 由本地 `make smoke` 与原生 macOS CI 共用的代表性执行计划验证，记录实际场景完成、业务断言、退出和终端恢复。普通测试中未启用的终端 helper、平台跳过或只出现展示文本 MUST NOT 计作相应 PTY 已执行。由冒烟承接的重复交互断言 MUST 在对应自动化入口实现并在本地实际执行通过后才迁移；包含该迁移的完成交付 MUST 同时获得对应原生 macOS CI job 的实际成功记录，并将该检查列为必要交付项。冒烟不能替代 durable、lease、安全或 Provider 原生协议的确定性故障验证。

#### Scenario: Run the same native acceptance locally and in CI
- **WHEN** 在支持的原生 macOS 环境运行本地或 CI 交互验收
- **THEN** 执行相同的关键场景与实质断言，复用构建产物，保留失败和清理结果
- **THEN** 不要求每个功能重复 Provider、色彩、尺寸和宿主的全部排列
- **THEN** 本地成功和 CI workflow 文件存在不能替代 CI job 的实际成功运行；未完成远端 required-check 配置时明确报告保护缺口

#### Scenario: Report an unsupported or incomplete run honestly
- **WHEN** 平台不支持必要终端能力，或场景未产生完成证据
- **THEN** 不能报告该原生交互验收成功，并明确未验证范围
- **THEN** CI workflow 与 required checks 文档的更新不能被声称为已自动启用托管平台保护
