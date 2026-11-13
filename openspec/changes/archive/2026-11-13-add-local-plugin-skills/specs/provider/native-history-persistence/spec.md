## MODIFIED Requirements

### Requirement: Native Skill content is frozen and restored without source I/O

两家 Provider SHALL 以唯一当前 user_inputs 契约保存原始用户正文和独立 Skill 附件的关联，原生请求按原文、带来源标记的附件顺序编码，保持同一真实 InputID。附件包装和展开正文在首次提交时冻结，不能以 system 权限消息或虚构工具调用持久化。tool_outputs SHALL 保存 Skill 工具已冻结的完整结果。strict decoder 必须校验附件字段、数量、关联、上限及原生内容一致性；不得引入旧 reader、格式版本或旁路 activation record。

同 turn 辅助资源资格 SHALL 由已验证且 durable 的显式附件派生，不能新增权限记录或凭恢复启动工具。

恢复 MUST 只消费已提交原生/结果事实，不读取 SKILL.md 或资源、不重新替换参数、不执行工具。result 已 durable 而 output 未提交时按 call index 补齐；started 无结果时沿用 outcome_uncertain 补偿。相同当前目录与配置下，uninterrupted/restored 的完整 canonical request、native item 顺序、opaque bytes、context footprint 和 fingerprints MUST 相同；当前目录变更只影响未来目录上下文，不能重写历史附件。

插件 Skill SHALL 使用相同当前附件/结果结构，严格校验 plugin scope、importer kind、插件 ID、限定名称 namespace 与复合 revision 的关联；来源 revision 绑定主文档与适用副文件状态，但恢复不重新计算来源文件摘要。导入不额外保存宿主路径、manifest/sidecar 原文、启用状态或安装记录，也不新增副文件格式 reader；后来明确的合法 resource 读取仍按现有冻结工具结果保存。离线恢复和 output 缺口补偿 MUST 不访问旧插件根、manifest、SKILL.md 或资源，也不从附件重新启用插件。当前来源快照只用于未来调用，在旧事实 reconciliation 后加载。

相同当前 SkillCatalog 下，插件 Skill 的 uninterrupted/restored 完整请求及 fingerprint 必须等价；若本次不再配置插件或 metadata 改变，只允许当前目录上下文与对应请求前缀发生可解释变化，历史原生附件/output、opaque bytes 和顺序仍须相同，不宣称变化后的完整请求与旧目录相等。

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

#### Scenario: Restore frozen plugin instructions after removing the plugin
- **WHEN** 插件显式附件或 Skill result 已 Sync，插件目录随后删除且本次没有 plugin-dir
- **THEN** 历史正文与原生 output 逐字节保留，恢复不读取或重新启用旧来源，未来目录不含该插件

#### Scenario: Reject a forged plugin source before repair
- **WHEN** checksum 合法但插件 source/name/revision 关联非法或与原生附件正文不一致
- **THEN** 整体恢复失败，原 journal bytes 不变，不返回部分 Conversation 或读取来源修补

#### Scenario: Compare plugin requests with an identical current catalog
- **WHEN** 两家分别从含插件附件、工具结果和 opaque reasoning 的固定 current bytes 恢复，并使用相同当前目录快照
- **THEN** 下一完整 canonical request、native item 顺序、opaque bytes、footprint 和内容/请求 fingerprint 均与不中断路径一致

#### Scenario: Restore after a sidecar policy or presence changes
- **WHEN** 含 Codex 调用策略的输入/工具结果已 Sync，此后副文件改变、消失或只在本次未来来源出现
- **THEN** 恢复及输出缺口补偿不读取副文件、不重新推导策略或复合摘要，原附件/原生 output bytes 保持，新目录仅由本次明确来源决定

#### Scenario: Preserve an explicitly read policy resource as ordinary frozen text
- **WHEN** 已准入的合法资源读取把 agents/openai.yaml 正文作为普通 Skill result 提交后恢复
- **THEN** 直接使用已冻结结果，不再加载该文件、不从结果正文恢复授权，也不创建新 record kind 或旁路 metadata 记录
