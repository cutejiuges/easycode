## Why

当前完整质量门的关键路径是全量 race，最新 main 的 Linux CI 中 app 普通测试为 69.132 秒、race 为 682.821 秒，验证步骤合计约 798 秒；本地既有记录达到 15–17 分钟。需要以可重复计时定位并减少重复工作，让包含普通与 race 的完整流程在参考环境中低于 300 秒，同时保持业务、安全、恢复和架构断言。

## What Changes

- 建立统一测试执行与计时入口，区分构建、普通、race、包与顶层测试耗时；性能验收强制执行测试，不能用结果缓存证明达标。
- 优化 `internal/codec` 严格解码中重复 JSON 扫描、字段拆分和形状遍历，保留现有公开函数、非法输入拒绝规则、canonical bytes、checksum 与 fingerprint。
- 复用 Session checksum envelope 的唯一编码结果生成当前 record，消除同一 payload 的第二次编码；保留校验、字段顺序、checksum 和固定字节，以原 Sonic 整体编码作为差异回归参考。
- 在 HookToolInput 的校验与 digest、HookInputEffect 的校验和嵌套编码、HookFinished 的嵌套编码中复用当前调用的编码字节；保持严格校验、错误分类、Sonic/race 检查、canonical bytes 和 digest，并增加原编码路径的差异回归。
- 基于实测为重量级测试采用有界调度和独立进程隔离；禁止在使用 `t.Setenv` 的测试上直接增加并行。让 Makefile、CI、pre-commit 与 pre-push 使用同一完整质量门。
- 审核重复测试装配和大数据构造，将纯逻辑断言放在相应层级。只有存在明确等价覆盖映射时才合并重复工作，保留真实进程、故障注入和恢复矩阵。
- 记录参考机器、完整执行集合、失败传播、资源峰值和前后耗时，更新测试策略与踩坑文档。

## Capabilities

### New Capabilities

- `engineering/test-execution`: 全量普通/race 的覆盖集合、资源有界调度、失败与取消语义、可复现计时以及 300 秒验收预算。

### Modified Capabilities

无。`architecture/single-current-contract` 的严格解码和唯一当前格式要求保持不变；`engineering/branch-governance` 的完整质量门要求保持不变。

## Impact

- 主要涉及 `Makefile`、测试调度/计时脚本及其测试、`internal/codec`、Session 内部 record 编码、domain 中 Hook 值对象内部编码与相邻回归/benchmark；按实测涉及 `internal/app` 等包的测试装配。
- 更新 `.github/workflows/verify.yml`、README、总体架构测试策略和 pitfall；hooks 继续调用 `make verify`。
- 不改变 Provider、Runtime、Tool、Session 的公开行为或持久化格式，不引入历史 reader、共享可变全局 codec 或外网测试依赖。
- 所有改动需在 proposal 审阅后由后续明确 apply 请求触发。此处保留提案阶段范围；实施证据见 [verification.md](verification.md)。
