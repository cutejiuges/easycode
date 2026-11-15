## ADDED Requirements

### Requirement: Hook replacement review shows actual effective execution

PreToolUse 信任审查和 /hooks 详情 SHALL 说明参数完整替换、必要执行事实保存及随结果向 Provider 披露用途，并保持默认拒绝、独立普通权限及现有精确关联。普通批准 SHALL 展示最终有效目标、命令、内容/diff，并以有界来源标记说明参数经 Hook 调整；不能让用户批准原参数而执行新参数，也不能在批准响应中编辑。

工具主线 SHALL 区分原始 O、检查候选 C、gate 已选择 E 与真正执行准入。检查期间不能称 C 已被选中，gate 拒绝后只显示候选审计与未执行；gate continue 后等待/运行/终态摘要与 E 一致。Ctrl+O 从已验证内存事实分别展示原始请求、候选前缀、是否选中及来源。替换只有成功 Sync 后才可显示已保存，tool started 前不得显示 Running；已准入也不能直接宣称 OS 启动成功。原始 stdout/stderr/reason 不自动展示，typed 输入按现有安全文本规则转义。

详情查询 SHALL 复用既有 generation/detail sequence、异步 owner、分页和有界缓存。旧调用/plan/序列或取消后的响应不能覆盖当前输入、授权、草稿或阅读锚点。浏览/getter/render 不执行 I/O。窄屏、长文本和 NO_COLOR 必须支持完整必要审查，不能把截断内容当成完整批准；Esc/Ctrl+C/paste 继续遵循现有 modal 和 cleanup 规则。headless 外部 JSONL 不增加字段或事件。

#### Scenario: Approve the actual changed command
- **WHEN** Pre 把原命令替换为另一完整合法命令
- **THEN** 普通批准显示有效命令与 Hook 来源，默认拒绝不启动工具，原请求可在详情辨认

#### Scenario: Inspect original and effective inputs after restart
- **WHEN** 用户恢复包含已保存参数变化的会话并打开 Ctrl+O
- **THEN** 原始/有效输入及来源与 live committed 语义一致，不读取旧脚本或工作区重建

#### Scenario: Keep replacement separate from execution
- **WHEN** effect 已保存但后续检查、普通批准或工具尚未完成
- **THEN** 界面不显示工具已执行或原命令测试成功，取消时等待唯一 owner 清理

#### Scenario: Ignore a stale replacement detail
- **WHEN** 当前调用已有更新 sequence，而旧 plan/旧调用/取消后的查询返回
- **THEN** 不覆盖有效参数、授权、草稿或阅读锚点

#### Scenario: Review in narrow and uncolored terminals
- **WHEN** 终端为 28×10 或 8×2、开启 NO_COLOR，或长参数包含 Unicode/控制字符且发生 resize
- **THEN** 必要字段可安全完整审查或明确停用无法完整呈现的批准，不能隐式授权；返回保留草稿

#### Scenario: Keep a rejected candidate visibly unexecuted
- **WHEN** 已保存 C 的参数变化，而后续检查或信任被拒绝
- **THEN** 详情保留候选审计，主线明确未选中/未执行，不把 C 当作实际工具目标

### Requirement: Pre hook snapshots are installed before bounded activity notifications

Pre 进度和详情 SHALL 先安装包含原调用、计划、已保存前缀和实际 gate 状态的不可变快照，再发送活动通知。detail_sequence MUST 来自成功持久化返回的真实 record sequence，不能用 ordinal、时钟或 native revision 替代；最后批次以 gate 的真实 sequence 表示选择。Sync 前不发布成功，Sync 后快照安装失败时不继续候选/普通批准/工具执行，不回滚或重跑已保存检查。

既有 HookActivity 的 Pre 投影 SHALL 封闭区分 none/candidate/selected/rejected；后三者只含真实 sequence、摘要、至多 160 rune 的显示摘要与有界统计，完整 canonical 块最多 4 KiB，整体消息仍满足既有 16 KiB。完整参数只通过内存详情分页提供，不塞入活动通知；Post/Stop 不接收 Pre 分支。无真实运行的 gate 拒绝不得伪造执行统计。

同一调用/plan 的旧相位、旧序列、迟到 checking 或旧 query 页 SHALL 不能覆盖较新的 selected/rejected。通知推进既有查询代次并失效相应缓存页；普通批准使用自身完整合法展示，不能依赖旧详情页补足。快照派生、校验与深拷贝在锁外完成，锁内只做有界状态替换，render/getter 不执行外部 I/O。

#### Scenario: Refresh a detail opened during checks
- **WHEN** 用户在中间 C 已保存时打开详情，随后最终 gate 选择不同 E
- **THEN** 先安装最终快照，再以 gate sequence 失效旧页并刷新；旧响应不能覆盖 E，阅读锚点保留

#### Scenario: Preserve original parameter history on live updates
- **WHEN** 同一调用多次更改 C，最终选中或拒绝
- **THEN** 原 ready 展示来源保持 O，候选和 E 独立呈现，live 与恢复均不丢原请求或伪造执行

#### Scenario: Bound encoded activity with escaped text
- **WHEN** 候选摘要包含控制字符、HTML 字符或 Unicode，完整输入大于单页
- **THEN** 活动块按实际编码保持 4 KiB/整体 16 KiB，摘要明确截断；完整审查通过有界详情或批准展示完成，不缩短参数对象冒充完整

#### Scenario: Fail after durable state but before host installation
- **WHEN** finished/gate 已 Sync，但宿主不可变快照校验或安装失败
- **THEN** 停止后续普通副作用并进入既有失败收口，不回滚 journal、不重跑 Hook，也不发布已安装成功
