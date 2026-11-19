## MODIFIED Requirements

### Requirement: Context output is explicit ordered and isolated from one-shot output

stream-json v1 SHALL 增加 context.snapshot 与 compaction.started/completed/failed，由唯一 stdout writer 编码。snapshot SHALL 只包含有界数值、source kinds/revisions、配置/估算状态、活动状态与 committed scope，不包含正文；completed SHALL 只包含估算、normalized usage 和状态，不包含摘要正文、原生 payload 或权限数据。started 只在 durable start 后、completed 只在成功 Sync/finalize 后输出；每个 accepted compact 必须获得一个成功或失败终态，未获得 durable started 的失败不能伪造 started。durability 不确定时失败输出 MUST 显式标记 durable=false，不宣称已持久化收口。

context.query response MUST 先于 snapshot，compact response MUST 先于对应所有操作事件；独立手动压缩的成功/失败 terminal 和 cleanup 必须先于 queued 输入启动；自动压缩的 terminal 和 cleanup 必须先于原 turn 下一 sample，原 turn 结束前不启动另一 turn。自动压缩 SHALL 以 trigger=auto、原 submit 关联及真实 TurnID/sample index 输出 started/completed/failed，不生成虚构 compact response；manual 路径保持 response 先于事件。未启用自动压缩且没有显式新上下文命令时不得新增压缩控制输出。一次性 --print/--json 的词汇、slash 正文、退出规则保持原样。每个新 kind 必须有严格解码/校验与 golden，不直接序列化内部结构。

PreCompact 不增加 streaming v1 命令或帧。匹配非交互压缩 SHALL 在本次操作的 Hook 执行前检及启动命令之前返回 `hook_trust_required`，启动时观测的平台状态不能覆盖它。手动基础预检被拒绝时仍沿用原 control.response 拒绝路径。对成功接纳且已持久化 started 的操作，按原次序输出已有 `compaction.started` 和一个 `compaction.failed`。自动压缩仍关联原提交与真实回合，继续普通请求由 context/auto-compaction 的确认失败和预算规则决定。

`compaction.failed` 的稳定 v1 code 增加 `hook_config_invalid`、`hook_trust_required`、`hook_trust_denied`、`hook_denied`、`hook_failed`。最后一项承载运行器、输出和平台类的检查失败，细分分类只供内部语义。合并的摘要请求超预算使用既有 `compaction_budget_exceeded`，取消和 Session 写入失败使用既有码。网络前阻断只输出 unavailable 摘要 usage，durable 仅表示失败已 Sync。不得输出内部审查、活动、命令、stdout/stderr、摘要重点、脚本自由文本、原生数据或 secret。

#### Scenario: Query during an active operation
- **WHEN** 客户端在普通 turn 或压缩中请求 context.query
- **THEN** response 后得到最近 committed snapshot，active 状态可见，查询不触发网络或窗口变换

#### Scenario: Fail before started became durable
- **WHEN** compact accepted 后 started 的写入确定失败
- **THEN** 输出唯一 compaction.failed(durable=false)，不输出虚假的 started/completed

#### Scenario: Cancel auto compaction before started became durable
- **WHEN** 真实自动尝试在 started 接纳前收到取消，或 started 的 Sync 结果不确定
- **THEN** 只输出关联原 submit、真实 scope 和 CompactionID 的 `compaction.failed(durable=false)`，随后回合按各自取消或 Session 失败收口，无 started 或审查帧

#### Scenario: Preserve old headless output
- **WHEN** 客户端只使用既有命令或一次性输出模式
- **THEN** 既有字段不变；自动压缩可在 stream-json 普通 turn 中增加关联压缩事件，一次性模式不出现 context/compaction 控制帧

#### Scenario: Correlate an automatic summary
- **WHEN** stream-json 已接受的 submit 在工具续写前自动压缩
- **THEN** 压缩事件关联该 submit 和活动 turn，成功终态在下一普通采样前输出，不产生额外 control response

#### Scenario: Continue after an automatic failure
- **WHEN** 自动压缩 failed 已确定收口且旧窗口仍通过普通请求预检
- **THEN** 同一 submit 可继续产生普通采样事件，不额外发送 control response，不伪造 turn.failed；最终 turn 仅有一个终态

#### Scenario: Fail a matched headless compaction before execution
- **WHEN** stream-json 客户端请求合法手动压缩，且冻结快照有匹配的 PreCompact 声明
- **THEN** stdout 依次输出原 accepted response、真实 `compaction.started` 和 durable `compaction.failed`。失败码为 `hook_trust_required`，计量 unavailable，无信任帧、Hook 命令或 Provider 摘要请求

#### Scenario: Keep headless automatic fallback correlated
- **WHEN** 已接受的输入触发自动压缩且匹配 PreCompact，确定失败后旧窗口通过普通预算
- **THEN** 已有压缩帧携带真实 CompactionID、原请求关联及自动作用域。以 `hook_trust_required` 收口后只继续原普通请求，不出现新的 Hook/control/turn 帧类型。相同一次性执行仍只输出原任务投影
