## MODIFIED Requirements

### Requirement: Context output is explicit ordered and isolated from one-shot output

stream-json v1 SHALL 增加 context.snapshot 与 compaction.started/completed/failed，由唯一 stdout writer 编码。snapshot SHALL 只包含有界数值、source kinds/revisions、配置/估算状态、活动状态与 committed scope，不包含正文；completed SHALL 只包含估算、normalized usage 和状态，不包含摘要正文、原生 payload 或权限数据。started 只在 durable start 后、completed 只在成功 Sync/finalize 后输出；每个 accepted compact 必须获得一个成功或失败终态，未获得 durable started 的失败不能伪造 started。durability 不确定时失败输出 MUST 显式标记 durable=false，不宣称已持久化收口。

context.query response MUST 先于 snapshot，compact response MUST 先于对应所有操作事件；独立手动压缩的成功/失败 terminal 和 cleanup 必须先于 queued 输入启动；自动压缩的 terminal 和 cleanup 必须先于原 turn 下一 sample，原 turn 结束前不启动另一 turn。自动压缩 SHALL 以 trigger=auto、原 submit 关联及真实 TurnID/sample index 输出 started/completed/failed，不生成虚构 compact response；manual 路径保持 response 先于事件。未启用自动压缩且没有显式新上下文命令时不得新增压缩控制输出。一次性 --print/--json 的词汇、slash 正文、退出规则保持原样。每个新 kind 必须有严格解码/校验与 golden，不直接序列化内部结构。

#### Scenario: Query during an active operation
- **WHEN** 客户端在普通 turn 或压缩中请求 context.query
- **THEN** response 后得到最近 committed snapshot，active 状态可见，查询不触发网络或窗口变换

#### Scenario: Fail before started became durable
- **WHEN** compact accepted 后 started 的写入确定失败
- **THEN** 输出唯一 compaction.failed(durable=false)，不输出虚假的 started/completed

#### Scenario: Preserve old headless output
- **WHEN** 客户端只使用既有命令或一次性输出模式
- **THEN** 既有字段不变；自动压缩可在 stream-json 普通 turn 中增加关联压缩事件，一次性模式不出现 context/compaction 控制帧

#### Scenario: Correlate an automatic summary
- **WHEN** stream-json 已接受的 submit 在工具续写前自动压缩
- **THEN** 压缩事件关联该 submit 和活动 turn，成功终态在下一普通采样前输出，不产生额外 control response

#### Scenario: Continue after an automatic failure
- **WHEN** 自动压缩 failed 已确定收口且旧窗口仍通过普通请求预检
- **THEN** 同一 submit 可继续产生普通采样事件，不额外发送 control response，不伪造 turn.failed；最终 turn 仅有一个终态
