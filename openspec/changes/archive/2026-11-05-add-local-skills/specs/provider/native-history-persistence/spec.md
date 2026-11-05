## ADDED Requirements

### Requirement: Native Skill content is frozen and restored without source I/O

两家 Provider SHALL 以唯一当前 user_inputs 契约保存原始用户正文和独立 Skill 附件的关联，原生请求按原文、带来源标记的附件顺序编码，保持同一真实 InputID。附件包装和展开正文在首次提交时冻结，不能以 system 权限消息或虚构工具调用持久化。tool_outputs SHALL 保存 Skill 工具已冻结的完整结果。strict decoder 必须校验附件字段、数量、关联、上限及原生内容一致性；不得引入旧 reader、格式版本或旁路 activation record。

同 turn 辅助资源资格 SHALL 由已验证且 durable 的显式附件派生，不能新增权限记录或凭恢复启动工具。

恢复 MUST 只消费已提交原生/结果事实，不读取 SKILL.md 或资源、不重新替换参数、不执行工具。result 已 durable 而 output 未提交时按 call index 补齐；started 无结果时沿用 outcome_uncertain 补偿。相同当前目录与配置下，uninterrupted/restored 的完整 canonical request、native item 顺序、opaque bytes、context footprint 和 fingerprints MUST 相同；当前目录变更只影响未来目录上下文，不能重写历史附件。

#### Scenario: Restore after the skill file disappears
- **WHEN** 含附件的输入或 Skill 工具结果已 durable，文件随后删除
- **THEN** 历史正文仍逐字节相同；恢复没有 Skill 文件读取，工具不会重跑

#### Scenario: Repair the output gap
- **WHEN** Skill result 已 durable 但 native output 未提交
- **THEN** 仅用冻结 result 补齐匹配输出，原生顺序和请求 bytes 与未中断路径一致

#### Scenario: Reject a forged attachment
- **WHEN** native 附件关联、字段或内容与当前编码约束不一致
- **THEN** 整体恢复失败，不返回部分会话且不 repair 合法前缀之外的语义错误

#### Scenario: Compact without reactivating from disk
- **WHEN** 已加载 Skill 正文进入已压缩历史区间
- **THEN** 续写按既有 checkpoint 使用摘要，不从文件重新注入正文或恢复永久授权
