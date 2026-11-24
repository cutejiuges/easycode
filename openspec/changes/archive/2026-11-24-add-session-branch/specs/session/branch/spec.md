## Purpose

定义用户在交互式 EasyCode 中从当前对话派生独立续写线程的行为，包括分支命名、已提交历史切点、原子持久化与会话接管，以及双 Provider 历史继承和执行授权的边界。

## ADDED Requirements

### Requirement: Users can branch the current idle conversation

交互 TUI SHALL 支持 `/branch [标题]`，并以 `/fork` 作为完全等价的别名。用户显式提交即表达创建并切换的意图。创建只适用于具有已提交用户输入的当前线程，源 MUST 完全空闲、writer 健康，当前启动工作目录 MUST 与源已验证的规范化创建目录一致。分支沿用源创建目录、Provider family、wire 和 model。准备和接管 MUST 沿用同一终端已验证的工作区、私有存储和扩展来源身份。目录路径被替换时，替代目录不能成为读写、恢复或清理目标。无效参数、空会话、目录不匹配、忙或存储不可用 MUST 给出安全英文反馈。只读发现和恢复不创建分支。headless 文本及显式 Skill 调用沿用既有语义。

标题 SHALL 复用 `session/titles` 的值校验。完整原始参数非法时拒绝请求。参数省略或全为 Zs 时，若源在切点已有标题且带 ` (Branch)` 后缀的值合法，则使用该标题；其余默认 `Conversation (Branch)`。显式标题可以重名。分支命令和标题不作为模型输入或文件引用，创建本身为本地操作。

#### Scenario: Create and title a branch from the current conversation

- **WHEN** 源线程空闲、已有已提交对话且处于同一工作目录，用户提交 `/branch Parser alternative` 或等价的 `/fork Parser alternative`
- **THEN** 创建一个名为 `Parser alternative` 的新线程，成功后进入该线程并给出完整的源恢复命令，创建期间 Provider、Tool 和 Hook 调用次数均为零

#### Scenario: Choose a valid default from the committed tip

- **WHEN** 用户刚重命名源，随后省略分支标题；或源未命名、拼接后的名称超过既有限制
- **THEN** 从切点上的有效名称生成 `<源标题> (Branch)`，未命名或超限使用 `Conversation (Branch)`

#### Scenario: Keep the source when a branch is ineligible

- **WHEN** 用户在空会话、不同的启动目录、实际 worker 未完成、队列或输入准备未清空时申请分支，或参数包含禁用字符
- **THEN** 返回对应的本地诊断并保留可编辑草稿，没有分支事实、Artifact 操作或模型输入

### Requirement: A branch has a verifiable and self-contained lineage

分支 SHALL 使用源的 SessionID 和新 canonical ThreadID，父为命令接纳时的当前线程。切点为源 writer 已确认的最后完整提交，在完整 turn、工具调用、Stop 反馈和压缩生命周期之间。分支 journal SHALL 包含截至切点的准确源 JSONL bytes，随后追加唯一用于本次派生的 `thread_forked` 记录。该记录绑定父、最后序号、前缀 SHA-256、子身份和初始标题。普通恢复沿用现有追加和修复规则，保留被继承记录的身份、授权归属、checksum 和完整序号。源 journal 及工作区 SHALL 在分支创建前后字节一致。

分支及其再派生 SHALL 可在源忙或源文件不可用时独立预览和恢复。在任何修复或允许目标写入之前，系统 MUST 在目标的连续 lease 下验证整条链、当前身份、历史事实和 Provider 原生完整性。序号按切点后继续，新的记录和改名归所属分支。候选无有效分支标记或标记不能证明精确前缀时，MUST 拒绝将其作为可用分支。

#### Scenario: Keep the exact prefix when a source later diverges

- **WHEN** 从 A 创建 B，在 A 和 B 分别继续，再从 B 创建 C
- **THEN** B 以 A 的切点为界，C 继承 B 的已提交切点和正确的 A→B→C 血缘，各条分支保留独立的标题、续写和活动时间

#### Scenario: Restore without opening the ancestor journal

- **WHEN** 分支创建成功后，父 journal 的 lease 由其他真实进程持有，或父路径已不可用
- **THEN** 仅依据分支 journal 验证、预览或恢复，继承原生内容一致，分支可追加自己的记录

### Requirement: Publication has one durable and observable outcome

源交接许可 MUST 早于读源。源从 writer 持续拥有的独占句柄内提供最后一次已确认提交的序号与字节范围，不能用物理 EOF 扩大范围。已确认前缀不完整或验证发现源事实损坏时 MUST 拒绝创建，并停止源实例的新写入。分支候选 MUST 在私有且未公开的目标文件中完成写入、全量语义和 Provider 验证及文件 Sync。接纳排他发布时 MUST 再确认本次请求未取消，仍持有匹配许可且当前源实例健康。文件 Sync、排他原子发布、目录 Sync，以及正式路径从已验证存储根定位到实际持有的目录及 inode，均确认后才能报告已发布。目标 lease 自准备开始至 writer 同步关闭连续持有。

早于创建接纳的取消 MUST 没有创建副作用。发布接纳前取消或确定失败 SHALL 清理由本尝试实际拥有的暂存并反馈未发布。临时文件及崩溃遗留的未发布文件不得成为候选或阻断合法会话发现。rename 后确认完成前，仍持有的目标 lease 继续阻止竞争者读取和续写；Catalog 沿用未索引 busy 规则。原子发布接纳后 MUST 等待确定分类，即使源实例或终端已结束。发布确认时 SHALL 保留分支并交付完整可恢复 ID。若发布状态、正式路径归属或目录持久性无法确认，MUST 保留同一目标 ID 和不确定结果，停止目标候选的写入和接管，确认清理。自动重试、操作替换路径或删除可能已发布的分支均受禁止。线程资源的写入和 artifact 维护必须在已确认发布后开始。以后恢复仍可定位的有效分支时，MUST 先在目标 lease 下确认其规范路径和持久化，再开始修复、补偿或写入。

#### Scenario: Cancel a prepared candidate before publication

- **WHEN** 用户取消发生在原子发布接纳前，并且目标暂存仍在准备或验证
- **THEN** 暂存清理完成后恢复源会话，选择器、直接恢复和 Catalog 均没有本次分支

#### Scenario: Wait for a definitive result after publication admission

- **WHEN** 原子发布已接纳，用户取消时目录 Sync 尚未完成
- **THEN** 等待同一结果；Sync 成功时保留分支并反馈目标完整 ID，Sync 或发布结果不确定时保留诊断与目标 ID，候选不接收新输入

#### Scenario: Keep a competing canonical journal safe

- **WHEN** 发布前发现已有目标名、暂存文件或规范日期目录身份变更，或平台缺少等价安全发布能力
- **THEN** 拒绝发布，无替换。原活动线程仍健康且清理完成时可以继续使用

#### Scenario: Reject a detached publication directory

- **WHEN** 故障注入在原子 rename 后替换规范日期目录或最终文件名，而旧句柄仍能完成 Sync
- **THEN** 反馈结果不确定并提供尝试的完整目标 ID。目标不启动 writer 或接管，替代对象与可能已发布的对象均保持原样

#### Scenario: Distinguish source exit before and after publication admission

- **WHEN** 源实例在发布接纳前已退出或失去许可，或在发布接纳后退出
- **THEN** 前者无正式分支；后者由原 owner 收口实际发布结果。暂存和目标 lease 在各自操作结束后才清理，不挂载失去源许可的候选

### Requirement: Activation preserves source ownership until handoff

准备和发布期间 SHALL 由一个终端交接尝试持有源许可和目标生命周期。候选可接受时 MUST 校验源仍为当前健康实例、目标属于同一请求、发布已确认且源目标均未取消。接管后待源 Runtime、writer 和 lease 最终关闭，才允许分支接收输入。成功显示分支标题及完整 `/resume <源 ThreadID>` 提示。接管前取消、准备失败或接管失败 MUST 保留真实发布分类。源健康且目标清理已确认时，允许按原实例、模式和草稿返回。源或候选清理未完成时，由唯一 owner 保持责任并走现有退出边界。旧请求和迟到结果不得切换当前会话。已发布或不确定结果 SHALL 由宿主保持到完成交付。若取消、故障或源关闭阻止本次终态反馈，且进程仍能完成退出，宿主 MUST 在清理后通过安全 CLI 诊断交付分类和完整目标 ID，保持既有退出类别和错误优先级。

#### Scenario: Retain a durable branch if activation is interrupted

- **WHEN** 目标已发布，用户在接管接受前取消，或准备首屏失败
- **THEN** 健康源在候选关闭、许可释放确认后恢复原草稿，界面反馈目标完整 ID，目标可通过现有恢复路径打开

#### Scenario: Report a saved branch when the terminal stops receiving results

- **WHEN** 发布已确认但目标尚未完成接管，终端输出失败、进程收到可处理信号或源关闭失败
- **THEN** 等待资源关闭后，CLI 安全诊断仍含原发布结果和完整目标 ID，原终端错误或退出类别保持

#### Scenario: Wait for the source writer to close after activation is accepted

- **WHEN** 分支接管已接受，源 writer 的最终 Sync 被阻塞
- **THEN** 分支暂不接收输入，旧 lease 仍被持有；源最终关闭后启用目标，并显示源恢复提示

### Requirement: Inherited native history preserves continuation with fresh execution authority

分支 SHALL 继承已提交的 Provider-native 历史、真实工具 call/output 配对、冻结用户输入和 Hook 反馈、有效 checkpoint、任务状态及可见历史预览。输入、历史事实、原生 item 与依赖证据使用原有字节和次序。继承事实供回放、显示和语境使用。分支以当前启动配置和 Default 模式建立新控制实例，新样本的用量、进程内缓存诊断、临时审批、信任、文件读取资格、待审方案、effort 覆盖和输入队列从新实例开始。

历史 artifact descriptor MUST 按原归属验证。分支当前的工具与宿主读取权限，以及 artifact 清理，MUST 只由当前段中属于该分支的 durable 引用建立。历史工具详情 SHALL 展示冻结预览、保存时的状态及完整的生成线程 ID。继承工具项的详情不显示本线程的原文分页入口，也不预取原文。宿主直接收到祖先条目的原文请求时仍须按条目归属拒绝。原文可从实际生成它的线程依照现有策略查看。分支不改变冻结模型 output 及预算结果，也不补偿、重跑或重新计费已收口的历史操作。

给定相同的分支已提交历史、下一输入、上下文来源和捕获设置，立即续写与重启后续写的请求 canonical bytes、原生顺序、footprint 和 fingerprint MUST 等价。两种 Provider 的首次分支采样 SHALL 使用完整的有效原生窗口及本次输入。已有压缩时使用当前有效 checkpoint 和其后原生项，压缩前的归档正文只用于本地历史验证与展示。OpenAI 首个分支 WebSocket 请求不使用源的 `previous_response_id`，按当前策略派生分支的 thread cache key。Anthropic SHALL 延续原有断点和 TTL 策略。源 Provider 的关闭不得关闭分支后续请求所用的资源。分支元数据仅服务持久化和宿主，稳定内容仍以现有来源和原生事实计算。

#### Scenario: Continue the same native history before and after restart

- **WHEN** 含原生推理、已配对工具调用和有效压缩 checkpoint 的分支，在相同设置下即时续写或重启后续写
- **THEN** 两种 Provider 的分支请求分别满足等价要求。压缩前的原始事实可在本地历史读取，发送窗口由近期 checkpoint、checkpoint 后截至切点仍有效的原生项和新输入组成。OpenAI 首次完整请求使用本分支的路由身份和新连接续写状态

#### Scenario: Use historical evidence without granting mutation or artifact access

- **WHEN** 源有本地临时批准、成功的 workspace Read、历史 Plan offer 和已保存 artifact，用户在分支尝试 Write 或读取源 artifact
- **THEN** 分支按当前 Write 读取证据与审批规则判定，源 artifact ID 不获分支访问权限，历史冻结预览可读，历史方案仍可在 transcript 阅读

#### Scenario: Apply independent task progress and compaction

- **WHEN** 源已有任务清单和 checkpoint，分支更新任务、执行压缩并重启
- **THEN** 分支以继承的有效窗口和任务更新开始，后续 checkpoint 绑定真实分支证据，回放顺序一致，源在其原有切点继续拥有自己的状态
