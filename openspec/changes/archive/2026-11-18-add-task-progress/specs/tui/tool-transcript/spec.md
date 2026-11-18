## MODIFIED Requirements

### Requirement: All current tools have consistent user-facing activity

Read、Glob、Grep、Patch、Write、Exec、ReadArtifact、Skill、Question、TaskProgress SHALL 有稳定身份、可辨认名称、操作对象和等待/审批/执行/终态展示。相同能力在两家 Provider 下 MUST 使用一致交互；Exec 展示 Shell，不因 wire facade 名称宣称 Bash 专属语法。普通主线 SHALL 使用紧凑工具行与缩进结果，隐藏 seq、invocation ID、ledger、descriptor、fingerprint 及持续调试状态行。用户与 assistant 正文保持原有顺序与内容。

终态 SHALL 依据成功持久化的结果；非零 exit、权限拒绝、冲突、取消和 outcome_uncertain 必须可区分。未确定 cleanup 完成不能恢复新 turn admission。运行进度允许合并，不能冒充完整历史输出或覆盖终态。

#### Scenario: Show equivalent provider activity
- **WHEN** 两家 Provider 分别请求同样的读取、搜索、修改和命令
- **THEN** 主线呈现相同能力名称和状态规则，仅实际输入/结果不同

#### Scenario: Show nonzero command completion
- **WHEN** 命令确定退出且 exit code 为 1
- **THEN** 用户看到失败退出与退出码，不显示为成功测试或进程启动失败

#### Scenario: Show equivalent task update activity
- **WHEN** Anthropic 使用 TodoWrite、OpenAI 使用 update_plan 更新等价任务清单
- **THEN** 两者显示同一 TaskProgress 能力和真实保存状态，模型工具名差异不改变交互，只有成功结果 Sync 后才改变当前任务清单

### Requirement: Read and search grouping preserves meaningful boundaries

主线 SHALL 折叠相邻正常 Read/Glob/Grep，区分规范相对路径去重后的文件数与搜索调用次数。用户输入、非空 assistant 正文、修改、命令、任务进度更新、审批和异常 MUST 打断分组；ReadArtifact 独立显示保存输出读取且不计为 workspace 文件。分组内异常 MUST 展示明确异常项，不伪装成成功计数。分组只改变显示，不影响调度、调用位置或模型历史。

#### Scenario: Count repeated reads correctly
- **WHEN** 同一文件被读取两次，随后另一个文件被读取并执行两次搜索
- **THEN** 文件数为 2、搜索数为 2，展开仍保留全部五次调用

#### Scenario: Expose an error within a group
- **WHEN** 同一只读批次有一个结果失败而其他成功
- **THEN** 用户能直接辨认失败项，不能只显示绿色成功汇总

#### Scenario: Preserve a task update between reads
- **WHEN** 两组成功 Read/Search 之间出现任务进度调用及其结果
- **THEN** 前后读取分别汇总，任务更新保留独立原调用位置，不合并成文件计数或让后续清单替换该位置

## ADDED Requirements

### Requirement: Task progress history preserves each frozen update

任务进度主线 SHALL 在原调用位置显示可辨认更新活动、真实终态及紧凑数量摘要，不将当前清单的后续变化回填到旧活动。Ctrl+O SHALL 可在同一位置读取完整冻结原输入、更新说明、任务内容与状态以及已提交结果；两家 facade 的已知差异 SHALL 在原输入与规范清单之间准确区分，Anthropic 保留原 activeForm，OpenAI 保留原 step 且规范 activeForm 为空，不伪造模型曾提交的字段。未成功调用的详情 SHALL 保留原候选和真实失败/取消状态，不能将候选显示成已生效清单。

历史详情 SHALL 使用既有 typed 宿主查询、共享最多四页及每页 64 KiB 原始正文上限，结果按调用身份和查询 generation 关联；不重读当前项目文件、重新运行任务工具或把当前任务快照当作历史来源。浏览、折叠、分页、恢复、resize 和 NO_COLOR SHALL 只改变安全显示副本，不修改任务原文、模型 preview、原生历史或 fingerprint。审批、问卷及 Hook 审查 SHALL 保持最高焦点优先，返回时保留原调用锚点和草稿。

#### Scenario: Inspect an older update after replacement and clear
- **WHEN** 用户先后成功提交清单 A、清单 B 和显式清除，随后查看 Ctrl+O 中清单 A 的工具详情
- **THEN** 显示 A 当时的完整冻结输入、任务与结果，当前清除状态和 B 不覆盖 A，零模型请求及零 Session 写入

#### Scenario: Inspect a failed candidate independently
- **WHEN** 清单 A 已生效，而后续候选 B 未成功提交结果，用户查看 B 的历史活动
- **THEN** B 显示真实失败或取消及其原输入，当前任务保持 A，B 不被标记为已保存任务

#### Scenario: Resume historical progress without mutation
- **WHEN** 恢复线程后用户打开一个较早任务更新的详情并在窄屏与 NO_COLOR 下阅读
- **THEN** 已知内容与原提交事实可见语义一致，控制字符只做显示转义，原生 bytes 不变且不重新执行更新

#### Scenario: Keep history browsing bounded under a newer update
- **WHEN** 用户停留在旧任务调用详情时较新任务更新成功持久化，或者旧详情查询迟到
- **THEN** 当前任务区域可以更新而旧调用详情保持其冻结事实，过期回复丢弃，主线与详情合计不超过既有四页缓存
