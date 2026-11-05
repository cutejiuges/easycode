## ADDED Requirements

### Requirement: Explicit Skill inputs retain original text and frozen attachments

显式 Skill 输入 SHALL 在 admission 前完成可取消准备，保存用户原文及最多一个强类型 Skill 附件；附件包含来源、名称、revision、参数和完整展开正文，不能替换用户原文或伪造第二个用户身份。admission 继续纯内存校验，原文和附件共同计入每条 4 MiB、64 条/16 MiB 队列、native/record 16 MiB 与 batch 64 MiB 限额。候选预算和请求预检必须包含附件；Sync/finalize 前不得报告 committed。

queued 后文件变化不得改变已准备内容；取回或 returned tray 只恢复用户原文，再次提交重新准备。取消和 uncertain 沿用既有 owner 与 durable resolution，不能为了读取 Skill 创建独立无界队列或重试提交。

#### Scenario: Freeze an admitted skill
- **WHEN** 输入已 queued，随后磁盘 Skill 改变
- **THEN** 该输入只使用已准备附件；取回再提交会重新读取并进行来源检查

#### Scenario: Reject a preparation or encoding failure
- **WHEN** Skill 读取失败或原文加附件无法满足编码/预算约束
- **THEN** 不写该输入、不请求 Provider，用户原文保持可编辑

#### Scenario: Resolve a Skill input after Sync
- **WHEN** 带附件输入 Sync 成功后取消
- **THEN** 仍报告 committed，不把它返还为可自动重试输入
