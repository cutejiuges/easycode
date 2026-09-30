## Purpose

定义供 Provider 请求与缓存诊断使用的强类型不可变上下文分段，使 canonical bytes、稳定前缀顺序、source revision 和 fingerprint 在复制、恢复与调用方修改下保持确定且可验证。

## ADDED Requirements

### Requirement: Cache segments are typed validated value objects

缓存分段 SHALL 由非空稳定 ID、合法 stability、非空 source revision 和有界 canonical JSON 内容构造。核心导出构造契约 MUST NOT 接受无约束动态值；非法 JSON、非 canonical 表示、未知 stability、空 revision 或超过大小边界的内容 MUST 被拒绝。分段 fingerprint SHALL 由实际 canonical bytes 确定性计算，调用方不得注入不匹配的 fingerprint。

#### Scenario: Build equivalent segments

- **WHEN** 两次使用相同 ID、stability、revision 和语义等价的 canonical JSON 构造分段
- **THEN** 两个分段的 canonical bytes 和 SHA-256 fingerprint 完全一致

#### Scenario: Reject invalid segment metadata

- **WHEN** 分段 ID 或 revision 为空、stability 未知，或内容不是合法且有界的 canonical JSON
- **THEN** 构造失败且不返回可加入计划的部分分段

### Requirement: Cache plans are immutable validated snapshots

缓存计划 SHALL 固定版本并按调用方给定顺序保存分段的深拷贝。计划必须拒绝重复 ID、未知版本、稳定分段出现在 volatile 分段之后以及分段 fingerprint 与内容不一致。所有返回 slice 或 bytes 的读取操作 SHALL 返回独立副本，调用方修改输入或 getter 结果不得改变计划内容或 fingerprint。

#### Scenario: Mutate caller-owned inputs

- **WHEN** 调用方在计划创建后修改原分段 slice 或 canonical byte slice
- **THEN** 计划中的分段顺序、内容和 fingerprint 保持不变

#### Scenario: Reject an unstable prefix ordering

- **WHEN** 一个 stable、project-stable 或 session-stable 分段出现在 turn-stable 或 volatile 分段之后
- **THEN** 计划构造失败并指出稳定前缀顺序非法

#### Scenario: Read an immutable plan snapshot

- **WHEN** 调用方修改计划 getter 返回的分段或 bytes
- **THEN** 后续读取和 stable prefix fingerprint 仍与修改前一致

### Requirement: Stable prefix fingerprint has explicit inputs

组合 fingerprint SHALL 只覆盖从计划起点开始连续出现的 stable、project-stable 和 session-stable 分段，并纳入每段的 ID、source revision 与内容 fingerprint。turn-stable、volatile、时间戳、随机 ID、cwd、环境变量和 Session 文件位置 MUST NOT 被隐式加入稳定前缀。

#### Scenario: Ignore a changed volatile tail

- **WHEN** 两个有效计划具有相同稳定前缀但 volatile tail 内容不同
- **THEN** 两者的 stable prefix fingerprint 完全一致

#### Scenario: Invalidate on source revision change

- **WHEN** 稳定前缀中任一分段的 source revision 改变而其他输入不变
- **THEN** stable prefix fingerprint 改变并可定位到该分段 revision
