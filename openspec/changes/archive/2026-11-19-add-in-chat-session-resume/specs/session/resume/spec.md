## ADDED Requirements

### Requirement: In-chat resume validates the selected root under its exclusive lease

聊天内选择器 SHALL 以用户选中的完整 canonical thread ID 定位目标。直接输入完整 canonical UUIDv7 SHALL 跳过 Catalog 查询、列表和预览，使用同一个显式恢复入口。指向当前源 thread 的请求 MUST 在尝试取得目标 lease 前识别并返回，保留现有聊天实例。Catalog 候选与预览只提供选择依据，缺失或陈旧的索引行不改变 journal 的恢复资格。

系统 MUST 在读取目标 journal 前取得其跨进程 exclusive lease。在同一描述符与连续 lease 下，先校验 Session/thread 身份、root 归属、目标 `creation_cwd` 与规范化启动 cwd 的精确匹配、冻结 Provider family/wire/model 的精确匹配以及现有回放、原生历史和 checkpoint 契约。只有通过上述校验后才允许执行现有尾部修复与中断补偿，并将 lease 转交给目标 writer。目录检查采用既有启动路径规范化语义，保持实际工具工作区和进程目录。启动选择器、`--resume`、`--continue` 和 headless 继续使用各自的工作目录与恢复规则。

聊天内恢复 SHALL 使用首次激活时冻结的实际工作区和私有存储根权限。目标操作 MUST 使用原目录描述符或在实际取得后验证身份等价的描述符，再进入 journal 读取、修复、补偿、artifact 维护或其他写入。若采用重新打开路径的方式，验证结果必须绑定到实际用于操作的描述符。发现替换或身份无法确认时，系统 SHALL 在相关操作前拒绝，不得改用同名目录、空存储或其他权限根。

#### Scenario: Locate a target outside the picker results
- **WHEN** 用户输入当前列表范围外的完整 canonical thread ID，且目标与当前工作区及配置匹配
- **THEN** 应用直接取得该目标的 lease、验证 journal 并恢复原 Session/thread，不打开列表或选择替代会话

#### Scenario: Keep the active thread when its ID is supplied
- **WHEN** 用户输入当前聊天的完整 thread ID
- **THEN** 应用提示当前已在该会话，且不重新取得源 lease、装配或挂载聊天

#### Scenario: Reject stale metadata before repair
- **WHEN** Catalog 显示目标可恢复，但 lease 下读到的 journal 目录或 Provider 配置不匹配，且尾部存在未提交数据
- **THEN** 应用在 repair 前拒绝目标，目标的 committed records 和未提交尾部逐字节保持原样
- **THEN** 应用保留源会话，跨目录时指引从目标创建目录使用独立 CLI 恢复

#### Scenario: Reject a target acquired by a competing process
- **WHEN** 用户确认目标前后，另一进程已取得目标的活动 writer lease
- **THEN** 应用返回 `session_busy`，目标不发生读取后修复或补偿，源继续持有原 lease

#### Scenario: Reject a substituted workspace or storage root
- **WHEN** 确定性屏障在冻结目录身份后、取得目标操作实际使用的描述符前暂停恢复，期间原工作区或私有存储根被替换为同路径的另一目录
- **THEN** 系统拒绝不匹配的描述符，且替代目录未被用于读取 journal、修复、写入或 artifact 维护。源会话及原数据保持原状

### Requirement: In-chat recovery isolates the source until handoff acceptance

从目标恢复请求接收至交接被接受前，源会话 MUST 保留原身份、writer、持续 lease 和已保留的聊天状态。接收前取消 MUST 阻止目标恢复。接收后取消 SHALL 请求停止并等待目标的已知持久化结果或确认其不确定，再等待资源清理。接管前作废的成功结果亦由原 owner 清理。恢复使用现有仅依据持久化事实的本地补偿，不自动执行模型请求、历史工具或 Hook。

目标恢复 SHALL 区分 journal mutation 的接收、Sync 成功确认和 durability 不确定。已合法提交的目标尾部修复、补偿以及既有孤儿 artifact 回收结果 SHALL 保留。artifact 回收只能在目标连续 lease 下，以完整验证的目标引用集合为依据，仅处理目标的孤儿，不改动已引用 artifact 或任何源会话数据。

系统 SHALL 将前序写入造成的 poison 与最终 Sync、描述符关闭和 lease 释放的结果分别判断。交接前失败或取消时，只有目标最终清理确认成功且源仍健康，系统才 SHALL 让用户返回源会话并准确报告目标结果。前序 append 或 Sync 不确定的目标实例 MUST 停止接受操作，报告恢复结果不确定，并禁止自动重试，即使最终 Sync 和释放成功也须报告该不确定性。最终清理失败或超时 SHALL 以错误结束交互。超时须沿既有升级路径保留唯一清理 owner，观察最终完成及资源释放后才报告退出。

#### Scenario: Cancel after a durable target repair
- **WHEN** 合法的目标尾部修复已 Sync，但取消早于交接接收
- **THEN** 修复保留，目标在释放全部资源后退出准备状态，源以原运行实例和 lease 恢复交互

#### Scenario: Keep completed orphan maintenance confined to the target
- **WHEN** 目标在持有连续 lease 且完整核对 artifact 引用后，已回收自身孤儿并同步目录，随后在接受交接前取消
- **THEN** 目标孤儿保持已回收状态，目标的已引用 artifact、源 artifact 与源 journal 均保持原样，目标释放全部资源后返回源会话

#### Scenario: Isolate an uncertain target write
- **WHEN** 目标中断补偿 Sync 失败而使 writer poisoned，后续最终 Sync、描述符关闭和 lease 释放均确认成功
- **THEN** 应用报告目标恢复结果不确定，不自动再次恢复目标，源仍可接受用户显式提交

#### Scenario: Finish cleanup after failed candidate release
- **WHEN** 交接前停止候选后，目标最终 Sync 或关闭描述符失败，或最终清理达到关闭时限
- **THEN** 清理责任仍由原 owner 持有，交互进入错误退出并等待其最终完成信号，不发布目标或将源会话作为成功回退

### Requirement: Accepted in-chat resume commits ownership transfer once

系统 SHALL 为每次目标恢复提供唯一交接接收点，并拒绝重复接收。只有目标初始可见 transcript 的首个有界页面已取得并验证、目标控制实例就绪且候选会话生命周期仍有效时，才允许接受交接。空历史使用已验证的空页面。接管前不得把候选控制进度转发到源聊天，也不得向候选路由用户输入。初始投影或控制装配失败、控制实例提前结束或候选已取消时，应用 SHALL 按交接前失败清理目标，保留仍可用的源。

接收成功后，系统 MUST 在保持目标连续 lease 的同时，完成源会话的控制循环与资源关闭，确认源 writer 最终 Sync、描述符关闭和 lease 释放完成，之后才挂载仍健康的目标、展示其初始历史并启用输入。接收后 Esc 无取消、回退或退出行为，Ctrl+C 与外部退出信号进入宿主退出及清理流程。源关闭失败或超时，或目标在挂载前意外关闭，均 SHALL 以错误结束当前交互，并由各原 owner 收尾，获得最终完成信号后才报告退出。等待期间不得回到源或开放目标输入。启动入口的取消和退出规则保持原有语义。

#### Scenario: Retain the source if the candidate never becomes ready
- **WHEN** 目标已完成 journal 恢复，但首次 transcript 投影失败、目标控制提前结束或候选 context 已取消
- **THEN** 系统拒绝接管，完成候选清理后保留源聊天，候选控制进度及输入没有进入任一聊天

#### Scenario: Keep input disabled while the previous writer closes
- **WHEN** 交接已被接受，源 writer 的关闭暂未完成
- **THEN** 目标 lease 继续由宿主持有，源及目标均不接纳新输入
- **THEN** 源关闭与 lease 释放完成后，以目标原身份挂载一次，展示已验证的初始 transcript 并启用目标输入

#### Scenario: Keep cleanup ownership after the old writer exceeds its deadline
- **WHEN** 交接已被接受，确定性屏障将源 writer 的最终 Sync 阻塞至关闭时限之后
- **THEN** 原 owner 仍等待源 writer 的最终完成和 lease 释放。系统不启用目标输入，也不提前报告退出
- **THEN** 屏障解除后，宿主等待源与目标资源清理结束，再报告关闭超时

#### Scenario: Exit only through the host after handoff acceptance
- **WHEN** 交接被接受且正在关闭源，用户按 Esc，随后按 Ctrl+C 或发送外部退出信号
- **THEN** Esc 保持正在进行的交接，宿主退出信号启动有 owner 的退出流程。系统等待两侧清理完成，未回到源，也未启用目标输入

### Requirement: In-chat resume reconstructs target state from target facts

系统 SHALL 从目标的已验证 native history、checkpoint 和当前 journal 重建独立控制实例、可见 transcript、工具和 Hook 历史、任务清单以及 artifact 访问范围。连接设置、启动推理配置、项目指令、Hook 声明和 Skill、Plugin 来源 SHALL 使用同一交互进程的冻结启动快照，沿用来源访问时的既有身份与内容校验。临时工具授权、Hook 信任或拒绝、Read 观察、运行中 effort 覆盖及瞬态缓存 continuation SHALL 按新实例初始化。接管前后的控制观察和 UI 结果 SHALL 绑定各自的实例身份。

在目标持久化事实、冻结设置与下次输入相同的条件下，聊天内恢复和独立启动恢复 SHALL 生成等价的 canonical 请求、原生 item 顺序和缓存指纹。交接本身只使用已有 Session record/checkpoint 契约，无额外记录、版本或数据迁移。

#### Scenario: Continue the target's native history on either Provider
- **WHEN** 切换到已保存原生推理、工具调用配对与 checkpoint 的目标，随后提交首条新输入
- **THEN** 请求只从目标的已验证原生事实续写，canonical bytes、原生顺序和缓存指纹与相同条件下的独立恢复一致，新 records 追加到目标的下一 seq

#### Scenario: Switch back within the same process
- **WHEN** 用户为源会话临时授权工具和 Hook 并覆盖 effort，切换期间磁盘上的配置或声明发生变化，之后切回源
- **THEN** 新实例使用冻结的启动设置和声明，恢复此前 durable 历史与任务状态，工具权限、Hook 信任及 effort 从各自的初始状态开始

## MODIFIED Requirements

### Requirement: Cross-directory picker recovery requires explicit workspace acknowledgment

启动选择器（`easycode resume`）中目标 creation cwd 与冻结的启动 cwd 不同时，系统 SHALL 在提交恢复前展示二者，并说明当前项目指令和工具 workspace 使用启动 cwd。确认默认 SHALL 为返回列表，只有明确选择继续才恢复。系统 MUST NOT chdir、自动查找其他 worktree、生成可执行 cd 命令或写剪贴板。该确认不改变既有显式 `--resume` 的跨目录契约。

#### Scenario: Decline a different-directory target
- **WHEN** 用户选中另一目录会话后按 Esc 或接受默认返回选项
- **THEN** 回到列表，未执行恢复、读取项目指令或修改 journal

#### Scenario: Confirm recovery in the current workspace
- **WHEN** 用户明确确认在启动 cwd 继续另一创建目录的会话
- **THEN** cwd 不变，项目指令和工具权限来自当前 workspace，原 metadata/native history 不被重写
