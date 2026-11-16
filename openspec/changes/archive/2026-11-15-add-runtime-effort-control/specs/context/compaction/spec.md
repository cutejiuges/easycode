## MODIFIED Requirements

### Requirement: Summary requests preserve frozen reasoning with independent output budgets

手动及自动摘要 SHALL 保留本次操作捕获的 Anthropic thinking/display/effort 或 OpenAI reasoning.effort/summary，与普通采样使用相同的显式/省略语义，不因用途变为 summary 删除字段、夹取 budget 或降低 effort。自动摘要继承活动 turn 捕获的策略；独立手动摘要在纯预检接纳时捕获当前选定策略。任一活动操作期间后续接受的 effort 更新 MUST NOT 改变该摘要的预检、实际请求或收据。

摘要保持当前模型、原生有效窗口、项目/Skill、稳定工具目录和缓存策略；tool choice 必须禁用，输出上限只取 compaction_output_tokens，不使用普通 max_output_tokens。策略一致只维护本地请求语义，不保证服务端缓存命中或摘要必然产生正文。

摘要预检、估算、实际发送和 receipt MUST 来自同一摘要计划，其输入证据包含独有指导后缀、属性包含工具禁用和真实摘要上限；不从普通请求再次生成 evidence。准备/预检/失败不安装普通 baseline，completed Sync 后才切换有效窗口；摘要 generation 的推理与 raw usage 按当前 provenance 保存，但不发布普通推理正文或外部推理事件。成功摘要不得在活动 turn 内应用后续 effort，既有 manual budget 冲突、自动来源抑制及三次熔断规则保持。

独立摘要的 starting/running/finishing 以实际 started/terminal/cleanup 观察推进，terminal 后清理完成前仍持有原捕获策略。摘要失败不撤销已接受的未来选择，不安装普通成功 baseline；既有摘要连接隔离、关闭和 full 重建规则仍生效，不能承诺保留摘要前 socket。系统 MUST NOT 参照其他产品的摘要优化自动禁用 thinking、降低 effort 或扩大额度。context 查询的 ordinary committed 估算不得标为真实摘要输入或 usage。

#### Scenario: Preserve adaptive display and effort during compaction
- **WHEN** 普通操作策略为 adaptive/display summarized/high，摘要上限 4096 且预检合法
- **THEN** 摘要保留该 thinking/display/effort，并发送独立 max_tokens 4096；成功后当前 turn 的下一普通请求仍使用原策略和普通上限

#### Scenario: Preserve OpenAI reasoning controls
- **WHEN** 普通操作 effort high、summary detailed、上限 32768，自动摘要上限 4096
- **THEN** 摘要保留同一 reasoning，只用 4096 上限与 tool choice none；不能删 summary 或降 effort 追求成功

#### Scenario: Keep a failed summary inert
- **WHEN** 摘要取消、截断或完成 Sync 结果不确定
- **THEN** 分别沿既有取消/失败/poisoned 契约收口，不安装部分摘要、不改变操作策略、不重跑工具；取得可信终态但候选失败时单独保存真实摘要 usage

#### Scenario: Select a future effort during automatic compaction
- **WHEN** high 工具回合的自动摘要正在进行，low 设置被接受
- **THEN** 摘要和该回合后续普通请求仍为 high，low 只影响下一个独立操作，旧摘要失败计数不清零

#### Scenario: Keep manual admission and execution consistent
- **WHEN** low 手动摘要通过预检并被接纳，随后 reset 恢复启动 high
- **THEN** 当前摘要的预算、编码、evidence 和 receipt 仍描述 low，下个独立操作才使用 high

#### Scenario: Keep selection and summary failure independent
- **WHEN** high 自动摘要期间接受 low，摘要失败后旧窗口仍可请求
- **THEN** 当前回合继续使用 high，后续选择仍为 low；摘要失败不使选择回滚，也不恢复已经失效的连接资格

#### Scenario: Hold a manual policy through final cleanup
- **WHEN** high 手动摘要 terminal 已发布，而清理被确定性暂停，后续选择已是 low
- **THEN** 查询仍显示 high/manual_compaction/finishing，原 CompactionID 保留；清理完成后才显示 idle 或启动下一操作
