## ADDED Requirements

### Requirement: Post hook feedback shares the original deterministic output budget

冻结原结果与 Post 反馈 SHALL 合计受原能力单次上限、sample 512 KiB、turn 2 MiB 和后续每 call 1 KiB 安全预留约束，不设额外正文额度。仅存在 feedback=model 计划时，从当前调用可用额度 A 先预留 R=min(16 KiB,max(0,A-1 KiB))，原结果最多使用 A-R；预留按原 call index 确定且未用部分在 Post terminal 释放。预留本身不双重扣账，实际 result 与反馈 bytes 各只计费一次。model 模式即使返回空对象，也可能因预留缩短原预览；只保证省略空反馈段，不得追认与无 Hook 输出相同或重新扩展已保存预览。

每项 additionalContext 原文 SHALL 最多 4 KiB，每调用最终反馈段含来源/包装/宿主安全提示总计最多 16 KiB。接收必须验证完整包装和实际 Provider 编码上限；余额不足时拒绝整块，保存 hook_feedback_budget_exceeded，不截断指令、不修改已 durable 结果或扩大限额。在接收正文前 SHALL 为每个尚未解决的 model 项预留 512 bytes 宿主安全 code 槽和固定包装；候选接收必须保留后续项的槽。最大合法 provenance/封闭 code 编码须验证能放入单槽。若 R 不足最小提示集合及包装，则整次反馈 delivery 为 budget_unavailable，不接收正文或模型提示，只保存 typed 分类，检查仍可按计划运行。已有空间时失败用本项预留槽，成功空对象释放本项槽；已 durable 内容不得因后项失败被删除、裁剪或重排。无匹配或全部 feedback=none 不预留、不改变原输出 bytes。恢复 MUST 复用冻结正文与预算事实，不能重新裁剪、重新扣账或按当前配置分配。

调用预算 SHALL 具有 reserve、真实结果提交、按序反馈提交、close 四阶段。reserve 固定原调用额度、反馈预留及原结果 allowance，不退休调用；每段在确定 durable 后只计费一次。只有 close 在 Post terminal durable 后释放未用预留并将该调用计数恰好推进一次；无 Post 时结果提交后直接 close。尚未 close 不得准入后序调用或下一 sample，重复分段提交/close 必须拒绝。最终预算事实 SHALL 交叉核验原 A、R、base allowance=A-R、原结果 bytes、最终反馈 bytes 及释放余额R-feedback bytes，恢复不重新计费。

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
