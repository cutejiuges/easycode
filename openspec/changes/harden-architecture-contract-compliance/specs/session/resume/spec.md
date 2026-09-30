## ADDED Requirements

### Requirement: Resume keeps a descriptor-bound path ownership chain

resume SHALL 从已安全打开并校验的数据根开始，相对于父目录句柄逐级定位目标 journal；目录类型、权限、journal 类型与权限以及 exclusive lease MUST 绑定到实际参与后续 load/repair/append 的句柄。路径组件在检查后被替换 MUST NOT 改变本次恢复所读取、修复或续写的对象。

#### Scenario: Replace a directory component during resume

- **WHEN** 测试在 resume 路径解析期间确定性地把日期目录替换为指向数据根外的 symlink
- **THEN** resume 在 load、repair、Provider 恢复和网络请求前失败，或继续使用替换前已取得的安全目录句柄
- **THEN** 数据根外的目标不会被读取、截断或追加

#### Scenario: Replace the journal before lease acquisition

- **WHEN** 测试在 journal 路径检查与打开边界把目标替换为 symlink 或非普通文件
- **THEN** resume 不会在替换目标上取得 lease、执行 repair 或启动 writer
- **THEN** 应用返回安全英文 Session 错误

#### Scenario: Continue on the leased file handle

- **WHEN** resume 已安全打开 journal 并取得 exclusive lease
- **THEN** load、可允许的尾部 repair、ReplayPlan、writer transfer 和后续 append 使用同一连续句柄所有权链
- **THEN** 路径随后被重命名或替换不会把续写切换到另一个文件
