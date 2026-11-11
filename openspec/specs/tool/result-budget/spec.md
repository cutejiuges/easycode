# tool/result-budget Specification

## Purpose
定义九项当前工具的模型预览、显式后置反馈累计预算及工具特定处理规则，使多调用、多 sample 与同轮追加中的资源使用有界、结果可解释，并保持审批、文件观察、原生配对和恢复字节一致。

## Requirements

### Requirement: Tool preview budgets have explicit units and deterministic allocation

系统 SHALL 计量最终模型 preview 的 UTF-8 bytes，包含行号、结果说明、省略提示和 artifact 引用。Read/Exec/ReadArtifact 单次上限为 256 KiB、Glob/Grep 为 64 KiB、Skill 为 128 KiB，每 sample 总上限 512 KiB、每 turn 总上限 2 MiB。Patch/Write 返回紧凑摘要且服从累计额度。计数不是 token window 或完整网络请求大小；实际序列化膨胀 MUST 仍经过 Provider 编译及既有请求检查。

分配 SHALL 按 call index 与已冻结结果计费，为当前 sample 后续 calls 和本 turn 剩余 64-call 上限位置各保留每项 1 KiB 安全结果空间；两者分别约束 sample/turn 剩余额度，不重复扣费。当前 allowance 为单次上限、sample 扣除后续预留的余额、turn 扣除后续预留的余额中的最小值。加减 MUST 检测溢出/下溢；并行完成时间不能改变分配。预算只处理新结果，同轮追加不重置，已 durable 结果不能重新裁剪。

#### Scenario: Bound parallel aggregate output
- **WHEN** 多个只读调用完成顺序不同且各自原始结果都接近单次上限
- **THEN** 相同输入与保存可用性得到同一预算结果，最终 sample previews 合计不超过 512 KiB且按原 call index 提交

#### Scenario: Count the entire preview
- **WHEN** 正文恰好占满 allowance 而还需加入 artifact 引用与省略提示
- **THEN** 正文让出提示空间，最终 bytes 不超限，不能只计正文

#### Scenario: Keep steering in the same budget
- **WHEN** 同 turn 安全点提交两条追加输入后继续工具 sample
- **THEN** 原工具累计使用量保留，不因输入或 UI 操作重新获得 2 MiB

### Requirement: Budget finalization respects tool semantics

Read SHALL 优先返回原请求范围的既有有界结果；若该结果超过当前 allowance，返回安全 `result_budget_exceeded` 并提示缩小范围，不自动转存或事后默默缩短，完整观察不可用。ReadArtifact SHALL 在实际消费时按额度生成有进度的页，不能先推进游标后裁正文。Glob/Grep SHALL 使用稳定首尾与真实省略统计，并按策略保存本次采集结果。Exec SHALL 按冻结额度生成双流头尾，保存是否需要依据最终额度判断。Patch/Write SHALL 保留实际终态/计数，不因预算改变已发生副作用或缩短审批候选。

#### Scenario: Preserve Write evidence safety
- **WHEN** executor 完整读过文件但最终 Read 因当前预算拒绝
- **THEN** 模型得到有界错误，完整观察与该路径已有观察失效，后续 Write 不借用该次读取授权

#### Scenario: Save output made large by aggregate pressure
- **WHEN** Exec 输出小于 256 KiB 但超过本次冻结 allowance
- **THEN** 输出按私有策略保存且模型预览缩短，不提前以原单次上限判定 not_needed

Skill SHALL 完整返回经过展开及来源包装的主文档或资源全文；超出当前 allowance 时只返回有界 result_budget_exceeded，不裁剪指令、不报告加载成功、不自动保存 artifact 代替缺失正文。

#### Scenario: Reject a partial skill result
- **WHEN** Skill 完整 preview 小于单次上限但超过有序累计分配的 allowance
- **THEN** 返回配对预算错误，没有任何部分成功指令进入 Provider 历史

### Requirement: Exhaustion preserves accepted execution and pairing

turn 在安全结果预留外已无正文额度时 SHALL 停止新的 executor/approval admission。已 accepted 的调用 MUST 恰好执行一次并等待 cleanup，未 accepted 的 ready calls MUST 得到有界预算错误。全部当前调用结果与匹配 outputs 提交后 SHALL 以 `tool_result_budget_exceeded` 结束 turn，不再采样或领取 steering；sample 局部不足但 turn 尚有正文额度时允许下一 sample 请求更小范围。普通取消、拒绝和不确定结果必须保留原终态，不能被预算错误覆盖。

#### Scenario: Exhaust between exclusive calls
- **WHEN** 前序结果使 turn 正文额度耗尽而后序 Exec 尚未准入
- **THEN** 后序 Exec 不请求审批或启动进程，但产生匹配原 call 的预算错误 output，随后 turn durable 失败

#### Scenario: Exhaust after read admission
- **WHEN** 只读批次已 accepted 后前序结果耗尽正文额度
- **THEN** 所有已 accepted 的调用仍完成唯一执行/清理，各自得到有界真实终态或预算结果，不能遗留 owner 或悬空 call

### Requirement: Final results freeze budget and saving decisions before persistence

每个结果 SHALL 在 durable 前冻结实际 preview、分配额度、实际 bytes 与封闭处理原因，搜索/命令保存状态与引用同时冻结。保存失败 MUST 返回有界预览和明确 unavailable，不回退大正文。恢复 SHALL 仅使用已保存结果补齐原生 outputs，不按当前预算重算、不访问 workspace/artifact、不重复执行；补偿结果遵守安全结果上限。预算与 UI 状态不得污染 stable catalog。

#### Scenario: Restore after a policy or artifact change
- **WHEN** result 已 durable 而 outputs 未提交，此后 artifact 删除或当前内存预算参数变化
- **THEN** 恢复得到原 preview bytes、同一原生顺序及等价 canonical request/fingerprint，不读取对象

#### Scenario: Keep state out of stable prefixes
- **WHEN** 仅改变使用余额、页面位置、活动 ID 或保存配额
- **THEN** 静态 catalog canonical bytes 与 stable prefix fingerprint 不变

### Requirement: User decisions are complete before acceptance and persistence

提问最终 preview SHALL 最多 128 KiB，并计入现有每 sample 512 KiB/每 turn 2 MiB 和后续安全结果预留。到达提问 slot 时确定 allowance；不足以表示最小合法结果时在发布问卷前返回有界预算错误。回答接收前必须纯内存验证完整 preview、必要 metadata、Session record 与 Provider output 的实际编码大小，不能仅计原始文字字节。

超限回答 MUST 被拒绝并允许宿主保留草稿修改，不写回答、不关闭请求、不截断选择/说明、不保存部分成功决定、不自动转存 artifact。合法回答接收后，既定 allowance 不得被其他结果抢占；同轮 steering 不重置预算。已 durable 回答恢复只复用原 bytes，不重新裁剪或渲染成新模型结果。

#### Scenario: Reject escaping expansion
- **WHEN** 回答原始文字小于 16 KiB 但 JSON 转义后的完整 preview 超过当前 allowance
- **THEN** 拒绝 admission，问卷和草稿仍可编辑，journal 与预算账本不变

#### Scenario: Exhaust before asking
- **WHEN** 前序结果已耗尽提问可用正文额度
- **THEN** 不发布问卷或等待用户，按原 call slot 生成预算错误并沿用既有耗尽收口规则

#### Scenario: Keep an accepted answer complete
- **WHEN** 完整回答已通过编码和预算检查并被接收，随后 turn 取消
- **THEN** owner 先取得完整冻结结果的确定 durable resolution，不把已接收选择缩短成成功片段

### Requirement: Post hook feedback shares the original deterministic output budget

冻结原结果与 Post 反馈 SHALL 合计受原能力单次上限、sample 512 KiB、turn 2 MiB 和后续每 call 1 KiB 安全预留约束，不设额外正文额度。仅存在 feedback=model 计划时，从当前调用可用额度 A 先预留 R=min(16 KiB,max(0,A-1 KiB))，原结果最多使用 A-R；预留按原 call index 确定且未用部分在 Post terminal 释放。预留本身不双重扣账，实际 result 与反馈 bytes 各只计费一次。model 模式即使返回空对象，也可能因预留缩短原预览；只保证省略空反馈段，不得追认与无 Hook 输出相同或重新扩展已保存预览。

每项 additionalContext 原文 SHALL 最多 4 KiB，每调用最终反馈段含来源/包装/宿主安全提示总计最多 16 KiB。接收必须验证完整包装和实际 Provider 编码上限；余额不足时拒绝整块，保存 hook_feedback_budget_exceeded，不截断指令、不修改已 durable 结果或扩大限额。在接收正文前 SHALL 为每个尚未解决的 model 项预留 512 bytes 宿主安全 code 槽和固定包装；候选接收必须保留后续项的槽。最大合法 provenance/封闭 code 编码须验证能放入单槽。若 R 不足最小提示集合及包装，则整次反馈 delivery 为 budget_unavailable，不接收正文或模型提示，只保存 typed 分类，检查仍可按计划运行。已有空间时失败用本项预留槽，成功空对象释放本项槽；已 durable 内容不得因后项失败被删除、裁剪或重排。无匹配或全部 feedback=none 不预留、不改变原输出 bytes。恢复 MUST 复用冻结正文与预算事实，不能重新裁剪、重新扣账或按当前配置分配。

调用预算 SHALL 具有 reserve、真实结果提交、按序反馈提交、close 四阶段。reserve 固定原调用额度、反馈预留及原结果 allowance，不退休调用；每段在确定 durable 后只计费一次。只有 close 在 Post terminal durable 后释放未用预留并将该调用计数恰好推进一次；无 Post 时结果提交后直接 close。尚未 close 不得准入后序调用或下一 sample，重复分段提交/close 必须拒绝。最终预算事实 SHALL 交叉核验原 A、R、base allowance=A-R、原结果 bytes、最终反馈 bytes 及释放余额R-feedback bytes，恢复不重新计费。

成功/失败两个互斥后置分支 SHALL 共用同调用唯一 reserve/base/feedback/close 生命周期。任一分支存在 model 声明时只计算一次 A/R；每分支独立按其 model 项及包装计算 delivery，不将互斥分支的提示槽相加。只实际选中且存在的分支可提交反馈 bytes；未选择分支不生成正文、宿主提示或扣账。route=none 或 success/failure 对应分支缺失时，全部反馈为空、processed/unstarted_from=0；唯一 terminal 仍只 close 一次并释放未用预留，不要求不存在的分支拥有未运行后缀。

原 error 保存后当前取消 SHALL 保留 failure 选择，沿已选分支的既有取消反馈规则收口，不借用另一分支提示槽；恢复新建终态不得制造后缀提示。预算不足或释放额度只影响 delivery/终态，不得参与 success/failure/none 选择。重复编排及恢复不能重新 reserve、提交同一段或 close。

只有未选中分支配置 model 时，执行前预留仍可能缩小原结果；系统 MUST NOT 在结果保存后重新扩展预览。每段的 event/plan/ordinal 与所选事件一致，实际反馈和释放余额可由固定事实验证；恢复不重新分配或计费。

#### Scenario: Reserve before a successful command fills its output
- **WHEN** Exec 命中 model 反馈计划且执行预览接近当前额度
- **THEN** 原结果预览使用扣除 R 后额度，完整反馈只能使用 R；两者合计不超过原单次及累计上限

#### Scenario: Refuse an entire escaping-expanded block
- **WHEN** 反馈原文合法但完整来源/JSON 包装或实际编码超过可用预算
- **THEN** 不保存部分正文，原成功结果不变，失败分类不回显正文

#### Scenario: Reuse frozen feedback after restart
- **WHEN** 结果及反馈已 durable、native outputs 缺失且当前预算/配置发生变化
- **THEN** 使用原 bytes 补齐输出，预览及反馈均不重算

#### Scenario: Empty model feedback cannot restore a shortened preview
- **WHEN** A 为 256 KiB、R 为 16 KiB、原预览需 250 KiB，而 model Hook 返回空对象
- **THEN** 原预览按 240 KiB 额度冻结，空反馈不增加正文；不能恢复旧 250 KiB 预览或宣称完整输出字节未变

#### Scenario: Count a call once across result and feedback
- **WHEN** 同一调用先保存真实结果，再保存多个反馈块并关闭
- **THEN** bytes 按分段累计但 sample/turn 调用计数只推进一次；重复提交或未关闭便开始后序调用被拒绝

#### Scenario: Preserve an earlier diagnostic when a later check fails
- **WHEN** 前序正文已 durable 且最后一项检查失败
- **THEN** 失败提示仅使用预留槽，不驱逐前序正文，最终合计不超 R；最小槽空间不足时从开始便不接受任何正文

#### Scenario: Reserve once for two alternative model branches
- **WHEN** 同调用成功和失败分支都有 model，实际工具执行失败
- **THEN** 仅预留一次 R、只按失败分支完整反馈扣账，另一个分支零扣账，call count 只推进一次

#### Scenario: Release a reservation belonging to an unselected branch
- **WHEN** 只失败分支配置 model 而原工具执行成功
- **THEN** 原预览仍按已冻结 A-R 保存，未用 R 在唯一 terminal 释放，不运行失败脚本或重扩展预览

#### Scenario: Reject cross-branch feedback and duplicate closure
- **WHEN** 失败调用提交成功事件反馈，或两个分支尝试分别 close
- **THEN** 拒绝且不改变已确认预算、冻结结果或正文

#### Scenario: Close an unmatched or none selection once
- **WHEN** route=none 或所选事件没有配置，且另一分支的 model 已使预览按 A-R 冻结
- **THEN** 所有反馈为空，在唯一 terminal 释放 R 并只推进一次 call count，不重扩展结果或伪造后缀提示

#### Scenario: Keep selection independent of budget and cancellation
- **WHEN** 同一已准入 error result 分别在额度不足、当前取消或离线恢复路径收口
- **THEN** 选择均为 failure，实际反馈只按相应门禁和冻结事实计费，不二次预留/关闭或改用成功分支
