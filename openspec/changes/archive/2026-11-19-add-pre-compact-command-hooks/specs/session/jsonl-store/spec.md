## ADDED Requirements

### Requirement: PreCompact facts preserve compaction admission and replay order

单记录 batch 的 `compaction_started` SHALL 具有必需的 `pre_compact` 封闭联合。`none` 表示合法冻结配置没有匹配项，仅含分支标识。`invalid` 另含配置或匹配无效的封闭安全原因。`planned` 另含按执行顺序冻结的 1–8 个声明引用、原始规范化摘要重点的 UTF-8 字节数和摘要、以及绑定 CompactionID、scope、来源、原始重点证据和完整有序引用的 plan digest。PreCompact 引用 SHALL 包含来源种类、revision、声明身份、命令 fingerprint、timeout、披露策略及规范 matcher。回放 MUST 校验 matcher 适用于该 scope、来源顺序、来源内 ID 唯一和相同来源的 revision 一致。运行前引用须和对应冻结声明一致。缺失 `pre_compact`、不支持的分支及分支外字段 MUST 在 repair、补偿、追加或外部调用之前拒绝。当前 fixture SHALL 使用固定当前字节，不引入格式版本、旧 reader 或迁移。

`pre_compact_hook_started`、`pre_compact_hook_finished`、`pre_compact_hook_decided` SHALL 是独立、单记录 batch 的专属事实，分别具备 typed constructor、strict decoder 和 validator。`compaction_started` 与三种检查事实的完整行 MUST 在判定尾部修复之前校验 `batch_id=seq`、`batch_index=0`、`batch_size=1` 以及外层空 TurnID、ParentThreadID。payload SHALL 与同一 CompactionID、scope 和 plan digest 关联，自动压缩只在 scope 中使用实际 turn_id 和目标 `sample_index`。started/finished MUST 匹配同一引用、连续 ordinal 和运行身份。判别分支、引用、运行身份、计量和决定证据 MUST 交叉核对。每条合法的自动压缩记录 SHALL 算作所属活动 turn 的历史前缀，以便恢复停在任意检查记录后的活动尾部。

`planned` 状态 MUST 先 Sync 再审查信任与执行。各项 started 仅允许在前序全部 finished(continue) 后保存，且 MUST 在交付 runner 前 Sync。started 表示执行准入意图。runner 可确定返回取消而未启动进程。finished SHALL 在 runner 返回清理结论后 Sync，保存封闭 status/code、适用退出事实、取消/超时、清理结果及有界字节数和内容摘要。每项最多一次 started 和 finished。finished 必须来自实际执行链路，禁止恢复来源。显式阻断须有已知 exit 0、无取消或超时且已确认清理的结果，exit 2 在退出判定中只可归为退出失败。信任、配置、执行前输入或平台拒绝均不得伪造运行。

唯一 decided SHALL 匹配已观察有序前缀，并在摘要网络请求前 Sync。通过决定要求全部计划项合法 continue 且实际请求预检通过。披露证据只允许出现在 `feedback:model`、已确认清理且正常 exit 0 的 continue 结果中，每项非空重点记录 1–4096 个规范化 UTF-8 字节及 digest。回放 MUST 重算原始重点和各项非空重点长度，加上每两个相邻非空段间的 2 字节分隔符，并拒绝合计超过 16 KiB 的通过决定。无法接纳合并结果或最终预算预检失败时 SHALL 保存对应的预算失败决定。配置以外的计划级前检、信任拒绝无需运行证据，未启动后缀的期限耗尽或取消使用当时真实完成的前缀。命令阻断或执行错误则须匹配该命令已保存的结果。任何 live 决定前均须收口已准入的真实 runner 结果。

只有 `none` 或 `planned` 后的合法通过决定允许提交 `compaction_completed`，或保存已实际取得的摘要用量证据。`invalid` 只能以 `hook_config_invalid` 收口。`planned` 的明确拒绝、阻断或失败决定只能以其对应代码收口。信任缺失、信任拒绝、显式阻断、已确认清理的检查执行失败分别使用 `hook_trust_required`、`hook_trust_denied`、`hook_denied`、`hook_failed`，预算拒绝使用既有预算失败码。通过决定与之后的 `compaction_cancelled` 相容，网络接纳前取消的用量为 unavailable。`none` 和通过决定后的终态不得使用 Hook 失败码。若 runner 报告不确定清理，健康 writer SHALL 保存不确定 finished、失败决定和 `session_failed` 终态，随后关闭接纳。写入结果不确定时 MUST poison，不宣称 durable 终态。没有摘要计量证据时，`usage_state` SHALL 为 `unavailable`。

`none` 和 `invalid` SHALL 不含专属调用或决定事实。压缩期间不得插入普通 turn/input/sample/tool/Hook 事实、其他操作的记录或第二个终态。对未闭合但合法的 committed 压缩前缀，回放 SHALL 返回真实已存状态，允许在末尾按 session/resume 补唯一 `compaction_failed`，code 为 `compaction_interrupted`，无需补造 finished 或 decided。非中断路径必须遵循已验证的检查顺序与终态约束。

PreCompact 事实 MUST NOT 持久化指令原文、命令正文、stdout/stderr、原始 stdin、自由文本原因、配置或文件路径、环境值、进程身份、授权响应或 grant。摘要重点只记录用于关联的长度和内容摘要。检查事实不占用普通 sample、原生 ordinal 或工具调用身份。原生 checkpoint 与真实摘要 generation 仍按现有 Provider 契约持久化。

#### Scenario: Persist a checked operation in order
- **WHEN** 压缩匹配两条并获交互信任，两条依次继续，摘要随后成功
- **THEN** journal 单调保存 started(planned)、两组各自 started/finished、通过决定和完成记录，各必需同步先于对应 shell、网络和候选安装
- **THEN** 仅保留安全检查及摘要重点关联证据，没有脚本输出和重点原文

#### Scenario: Persist no-match and invalid starts without forged attempts
- **WHEN** 冻结配置合法且无匹配，或配置/匹配无效
- **THEN** started 按当前 schema 使用对应的 none 或 invalid，后者确定失败，二者均不保存调用或其结果

#### Scenario: Reject malformed or out-of-scope check facts
- **WHEN** 完整记录缺少必需计划字段、matcher 与 trigger 不匹配、声明超限、目标 scope、来源或 ordinal 错配，插入其他操作事实，或未通过检查便保存完成记录
- **THEN** 即使 checksum 合法且另有半行，本次加载与回放整体拒绝，原文件字节保持不变

#### Scenario: Reject a forged check batch before truncation
- **WHEN** 一条 checksum 合法的完整检查行声明属于多记录 batch，其后的文件结尾是未完成行
- **THEN** 解码拒绝完整非法行，不能生成可删除该记录的修复凭据

#### Scenario: Reject forged blocked or over-budget evidence
- **WHEN** 计划声称已显式阻断但 finished 记录已知 exit 2，或计划声称通过但已授权重点合计超过边界
- **THEN** 即使 checksum 和 plan digest 可验证，也拒绝完整记录及操作终态

#### Scenario: Accept an interrupted attempt without fabricating cleanup
- **WHEN** 具有合法启动检查的压缩在 durable hook started 或通过决定后中断，尚无 compaction terminal
- **THEN** 回放保留已保存事实并仅接受中断补偿关闭本次压缩，不要求添加不存在的 runner 结果
