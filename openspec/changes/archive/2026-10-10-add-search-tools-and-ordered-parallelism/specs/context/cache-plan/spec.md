## MODIFIED Requirements

### Requirement: Cache plans are immutable validated snapshots

缓存计划 SHALL 表达唯一当前的强类型 shape，并按调用方给定顺序保存分段的深拷贝；纯内存计划 MUST NOT 携带固定 format version 或按版本选择 validator。计划 MUST 拒绝重复 ID、稳定分段出现在 volatile 分段之后以及分段 fingerprint 与内容不一致。所有返回 slice 或 bytes 的读取操作 SHALL 返回独立副本，调用方修改输入或 getter 结果不得改变计划内容或 fingerprint。

source revision、segment fingerprint 和 stable prefix fingerprint SHALL 继续表示内容来源与缓存失效，不得被移除或当作格式 decoder 路由。

#### Scenario: Mutate caller-owned inputs
- **WHEN** 调用方在计划创建后修改原分段 slice 或 canonical byte slice
- **THEN** 计划中的分段顺序、内容和 fingerprint 保持不变

#### Scenario: Reject an unstable prefix ordering
- **WHEN** 一个 stable、project-stable 或 session-stable 分段出现在 turn-stable 或 volatile 分段之后
- **THEN** 计划构造失败并指出稳定前缀顺序非法

#### Scenario: Read an immutable plan snapshot
- **WHEN** 调用方修改计划 getter 返回的分段或 bytes
- **THEN** 后续读取和 stable prefix fingerprint 仍与修改前一致

#### Scenario: Avoid a plan-version router
- **WHEN** 调用方创建或校验当前缓存计划
- **THEN** API 不要求永远等于常量的 plan version，且生产代码不存在旧计划 decoder 分支
