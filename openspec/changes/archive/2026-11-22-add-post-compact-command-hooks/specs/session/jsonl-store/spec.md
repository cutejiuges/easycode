## ADDED Requirements

### Requirement: PostCompact facts bind to the committed checkpoint and a closed run prefix

当前单记录 `compaction_completed` MUST 含必需 `post_compact` 封闭联合。`none` 表示合法无匹配，仅含分支名。`skipped` 含匹配超限、headless 信任缺失或完整计划超成功记录编码限额的封闭原因。`planned` 包含 1–8 条按稳定来源顺序冻结的引用和 matcher，以及绑定真实 CompactionID、scope、包含原生 payload 的实际 checkpoint 摘要及有序引用的规范 plan digest。引用的事件、none 披露策略、timeout、fingerprint、来源、revision、ID 和 matcher MUST 验证。缺字段、旧形状、错配或非法联合在任何 repair 或写入之前拒绝。

`post_compact_hook_started`、`post_compact_hook_finished` 和 `post_compact_hook_closed` SHALL 各具 typed constructor、strict decoder、validator。三种事实与 `compaction_completed` 的完整行 MUST 在判断尾部修复前验证单记录 batch（`batch_id=seq, batch_index=0, batch_size=1`）和空外层 TurnID/ParentThreadID。只有成功 checkpoint 的 planned 分支可接纳专属尾部。每个 started MUST 在 runner 准入前成功 Sync，并采用实际 PostCompact target、plan 和连续 ordinal。finished MUST 匹配该 started，记录 runner 返回的事件专属真实安全结论。单项成功仅允许已知 exit 0、确认清理、无取消或超时且限额内的输出统计；控制、反馈和恢复生成的 finished MUST 拒绝。none/skipped 无专属运行事实。后置未关闭时禁止普通 turn/input/sample/tool 记录、另一操作或第二个压缩终态；已校验的自动后置事实属于同一活动 turn 的尾部。

唯一 closed MUST 匹配已保存前缀、状态/原因、真实 finished 数及 live/recovery 来源。所有已准入项须有真实 finished 才能 live 关闭。恢复见孤立 started、或前缀已有实际不确定 finished 时使用 `outcome_uncertain`；后者优先于取消。真实取消 finished 使用 `cancelled` 并禁止后续执行。确定前缀已完成全计划时，全部通过记 `succeeded`，否则记 `failed` 及首个确定错误。全量确定 finished 后的取消不改变检查分类。未全部完成且无悬空 started 时，live 取消、零执行跳过及执行预算耗尽使用相应的稳定原因，recovery 则用 `interrupted`。预算耗尽只允许已完成真实前缀且仍有未开始项，禁止伪造 timeout。关闭 writer 接纳后等待确定提交结果。

后置关闭不推进 checkpoint/native revision，成功 checkpoint 的序号继续代表窗口安装、摘要计量及阅读锚点。有成功 checkpoint 的后置未闭合尾部与无成功 checkpoint 的悬空压缩 SHALL 分开，元数据与离线回放 MUST 校验同样的状态顺序和证据。事实不得复制摘要正文、stdin、命令、原始输出、路径、自由文本理由、授权响应或环境值；只有稳定状态、必要关联和有界执行计数可以持久化。恢复或清理未确认不得制造退出结果。

#### Scenario: Bind execution to an installed success

- **WHEN** 所有审查通过并完成两条命令
- **THEN** 先持久化唯一成功 checkpoint 与计划，再按顺序保存两组实际 started/finished 和唯一独立关闭，checkpoint 的顺序和计量保持原样

#### Scenario: Reject a checksum-valid misplaced run

- **WHEN** 一条校验和合法的成功记录或后置记录声称属于多记录 batch 且其后为半行，或完整事实的事件、计划、作用域、序位或关闭分类错配
- **THEN** 在 repair 或补偿前拒绝整份恢复，原文件全部字节保持

#### Scenario: Save the confirmed verdict when cancellation arrives late

- **WHEN** 所有声明的确定 finished 均已 Sync，随后取消与 closed 的写入交错
- **THEN** 从真实前缀保存通过或首个确定错误；取消仍停止后续采样，不增加 cancelled finished 或改变检查结果
