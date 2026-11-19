# runtime/effort-control Specification

## Purpose

定义会话内推理强度选择的进程作用域、接收顺序、生效边界和只读观测，使用户能够为后续任务调整 effort，同时保持当前完整模型回合、工具与摘要请求的一致性，以及原生历史、权限和资源所有权的现有保证。

## Requirements

### Requirement: Effort selection preserves provider semantics and startup constraints

系统 SHALL 提供 reset、auto 和显式 level 三类封闭选择。reset 恢复本次启动配置的 effort，auto 省略原生 effort 字段，显式选择只替换当前 Provider 的 effort。Anthropic SHALL 允许 low/medium/high/xhigh/max；OpenAI SHALL 允许 none/minimal/low/medium/high/xhigh/max。auto MUST NOT 被编码为某个枚举值、disabled、零或推测的默认强度。

选择 MUST 保留启动冻结的 thinking、budget_tokens、display、summary、普通输出额度、model、缓存策略和窗口配置，复用同一推理组合及上下文预算校验。Anthropic disabled thinking 与 xhigh/max 的冲突、OpenAI 已配置窗口但新增推理选择没有显式普通输出上限等现有限制 MUST 在更新前拒绝。合法枚举不构成 endpoint 支持证明；系统 MUST NOT 按模型名猜测能力、自动降档、探测或隐式修改其他字段。

普通输出额度的显式/缺省来源 MUST 保留，不得以有效默认数值反填为显式配置。选择只受冻结配置与固定窗口/预留的组合约束，不以当前历史是否超限、估算 unknown 或 context/cache 观测失败拒绝合法 effort；真实操作仍须执行原有动态预检。

#### Scenario: Distinguish auto and reset
- **WHEN** 启动 effort=high，用户先选择 auto，再选择 reset
- **THEN** 两次选择分别得到字段省略和显式 high；均保持其他设置，操作本身不发起 Provider 请求

#### Scenario: Preserve family-specific choices
- **WHEN** 当前 Provider 为 Anthropic，而接收的合法公共选择为 none 或 minimal
- **THEN** 控制面以 invalid_effort 拒绝，原选择、活动策略、journal 和队列均不变

#### Scenario: Reject an incompatible update atomically
- **WHEN** disabled thinking 下选择 max，或已配置窗口而普通输出上限省略的 OpenAI 会话选择 high
- **THEN** 以 effort_conflict 拒绝，不夹取或补输出额度，不部分更新设置

#### Scenario: Preserve an implicit ordinary output cap
- **WHEN** Anthropic 普通上限为缺省 4096，用户改变 effort
- **THEN** 缺省来源继续保留，不被改为显式 4096；相同最终 wire 身份不因来源重建而失真

#### Scenario: Keep automatic omission distinct from an explicit none value
- **WHEN** OpenAI 用户分别选择 auto 与显式 none
- **THEN** 前者省略 effort，后者发送 effort=none；不映射为本地猜测的模型默认值，两者的请求身份按真实字段区分

### Requirement: One owner accepts process-local effort commands

effort 查询与设置 SHALL 由现有 thread 输入控制 owner 串行接收，具有 request identity、专属强类型构造、严格解码和 validator，拒绝不属于相应 kind 的字段。设置不进入输入 FIFO、不创建 turn/input/compaction identity、不写 Session 或配置文件，不进行网络、文件或命令操作，也不改变权限、工具预算或来源信任。

命令 context SHALL 只控制 owner 接收前的 handoff；调用前已取消或 handoff 未成功 MUST 不改变选择，取消与成功 handoff 竞争时以实际接收为线性化点。接收后 MUST 返回确定的 accepted/rejected 结果，不因调用方随后取消撤回更新。重复 request identity SHALL 沿用有界去重；closing、draining 或 poisoned 会话不得接受新设置，返回既有 session_closing/session_failed 安全拒绝。相同 wire 值的重复合法选择可更新来源说明，但 MUST NOT 制造 Provider 请求或缓存重置。

#### Scenario: Order a concurrent selection and new turn
- **WHEN** effort.set 与启动新 turn 的输入并发到达
- **THEN** owner 给出唯一顺序，新 turn 只捕获其启动前最后一个已接受的设置，不能发生预检/发送各取不同值

#### Scenario: Cancel on either side of admission
- **WHEN** 分别在 owner 接收前和接收后取消命令 context
- **THEN** 前者保持状态不变，后者等待确定结果且已接受设置继续有效

#### Scenario: Reject duplicate and closing updates
- **WHEN** 相同 request identity 重复设置，或关闭已开始后新设置到达
- **THEN** 分别返回 duplicate_request 或 session_closing，既有 worker 和选定设置不被二次修改

### Requirement: Every operation captures one immutable inference policy

新普通 turn SHALL 在其唯一 worker 启动前捕获选定完整推理策略。该 turn 内的首次输入预检、所有普通 sample、工具输出后的续写、同轮自动追加、Stop 续轮及自动摘要 MUST 使用相同策略。工具完成 Sync、Stop continued 决定、成功摘要和输入领取均 MUST NOT 成为应用新 effort 的边界。

独立手动摘要 SHALL 在其纯预检和成功接纳过程中捕获当时选定策略，后续执行不读取更新后的选择。活动操作期间接受的更新只影响之后启动的新 turn 或独立手动摘要；此前已排队但尚未启动的输入在真正启动时取最新选择。自动输入若仍进入当前 turn 则保留当前策略。选择不得自动取消、压缩、重试或启动任何操作，也不得重置自动压缩来源抑制、连续失败计数或熔断状态。

捕获 SHALL 早于 worker 的首次预检或持久化开始；未发布 durable started 时不得宣称 TurnID 已建立。普通 terminal 或摘要 terminal 到达只表示进入 finishing，旧捕获策略 MUST 保留到真实 worker/stream 清理完成，之后才清除活动身份、刷新 idle 规划并允许下一操作。取消接收不代表已经清理，也不授予在旧回合应用新 effort 的许可。

#### Scenario: Freeze across tools and stop continuation
- **WHEN** low 回合等待工具或 Stop 检查期间接受 high，并继续同一回合
- **THEN** 该回合全部后续普通/自动摘要请求仍为 low，之后新回合才为 high，工具执行次数不增加

#### Scenario: Keep queued input and automatic input distinct
- **WHEN** 当前回合为 low，严格下一回合输入已排队，然后设置 high，另有自动输入被当前回合领取
- **THEN** 被领取的自动输入仍在 low 策略下交付，严格输入启动时使用 high，输入顺序和正文不变

#### Scenario: Freeze a manual summary before a later update
- **WHEN** high 手动摘要已接纳，执行期间设置 low
- **THEN** 预检、摘要 body 与 receipt 都使用 high，下个独立操作使用 low

#### Scenario: Preserve the automatic circuit breaker
- **WHEN** 自动摘要已因三次失败熔断，用户接受新的 effort
- **THEN** 不清零计数或隐式重试；只有既有成功手动/自动压缩规则能够解除熔断

### Requirement: Effort snapshots distinguish selection from an active operation

查询及成功设置 SHALL 返回有界只读快照，包含 scope=process、family、startup、selected、source、selection_revision、activity、适用的 active、pending 和 allowed_levels。startup/selected 使用明确 omitted 或 explicit/value 状态，不能以 null/空串代替省略。allowed_levels SHALL 按当前 Provider 原生顺序提供至多七个无重复、通过本地组合校验的档位，不构成远端支持声明。

active SHALL 是封闭联合，各分支必需捕获的 effort、selection_revision、cancel_requested 和 phase：turn_starting 只使用发起输入原有 request_id，禁止 TurnID/CompactionID，phase 只允许 starting 或启动失败后的 finishing；turn 只使用 durable started 已发布的 TurnID，phase 只允许 running/finishing；manual_compaction 只使用已接纳的 CompactionID，phase 允许 starting/running/finishing。各分支只允许本分支身份；idle 不带 active。自动摘要仍是 turn 分支并以 activity=compacting 表示，不成为独立操作。启动失败中的错误 TurnID 不得冒充 durable started。

快照 SHALL 明确表示操作已捕获的配置，不能据此宣称已发送请求、服务端已采用或已计费；starting 或本地预检失败时可能完全没有 Provider 请求。pending SHALL 只取决于活动捕获 effort 与选定 wire 值是否不同，同值不同来源不能标为待切换。cancel_requested 只表示取消已请求，finishing 仍保留捕获策略和身份。

设置快照与内部 context 查询 SHALL 从同一 owner 的一致视图生成；窗口数值描述 ordinary committed 来源及对应操作 profile，不代表摘要请求、流式 staging 或 usage。后续选择另行标识，不把新选择标为旧请求已使用。查询 MUST 无 I/O、无持久化、无修订推进、无缓存或权限变化。快照 MUST 不含 prompt、raw usage、opaque reasoning、密钥、header 或绝对路径，最大编码 16 KiB。

#### Scenario: Explain a pending change
- **WHEN** high 操作仍活动，而 low 已被接受
- **THEN** 查询明确显示 active high、selected low、来源和 pending；不将 low 标为活动请求的参数

#### Scenario: Change source without changing wire identity
- **WHEN** 启动 high 的活动回合中显式再次选择 high
- **THEN** 来源可以变为 runtime，但 pending=false、wire/profile fingerprint 和增量资格保持

#### Scenario: Inspect an omitted default
- **WHEN** 当前选择为 auto 且无活动操作
- **THEN** 显示 effort 未发送和服务端默认未知，无活动 ID，不推测 medium/high

#### Scenario: Observe an automatic summary without changing its policy scope
- **WHEN** 活动 turn 内正在自动摘要，同时查询 effort
- **THEN** activity=compacting，活动策略仍绑定原 TurnID；独立手动摘要则只绑定其 CompactionID，不能混用两种身份

#### Scenario: Query before a durable turn identity exists
- **WHEN** 新 turn worker 已启动，但 started Sync 被确定性屏障暂停，owner 尚未收到 durable started
- **THEN** 查询返回 turn_starting、原 request_id 与捕获 effort，不伪造 TurnID，不增加额外写入或网络；发布 durable started 后才转换为 turn

#### Scenario: Preserve the active policy after terminal delivery
- **WHEN** terminal 已发布但 worker 的最终清理被暂停，此时接受新 effort 并查询
- **THEN** active.phase=finishing，旧捕获值仍保留，后续选择独立显示；不刷新成伪 idle 或启动下一操作

#### Scenario: Capture does not prove transmission
- **WHEN** 操作已捕获 high，随后在首次 Provider 请求前因本地预算拒绝或取消结束
- **THEN** 可以显示捕获值，但不得显示 high 已发送或被远端接受；Provider 请求数为零

### Requirement: Selection revisions order process-local observations

selection_revision SHALL 是从 0 开始的必需 uint64 序号，由当前进程中的单个活动 thread 控制实例持有；选择分支、档位或来源发生变化时严格递增，同样选择的新合法 request 不递增但仍返回确定 accepted。active 保存捕获时序号且不得大于当前序号。来源变化但 wire 相同可以递增，MUST NOT 进入请求、profile、native 或 journal 身份，也不得影响 pending 的 wire 比较规则。

序号 MUST NOT wrap；上限处真正改变选择以 invalid_command 拒绝，旧值和序号保持，同值查询/选择及关闭仍可用。序号不是内部格式版本、持久化 revision 或跨重启去重身份。每个新控制实例从 0 开始，宿主 SHALL 为新输出流重置观测水位，MUST NOT 跨实例比较序号，即使两次挂载使用同一 thread ID。UI SHALL 结合控制实例身份、序号、request/generation 和操作身份隔离迟到回复，不回退权威选择，也不恢复旧草稿、通知或焦点。快照仍使用 `scope=process`。

#### Scenario: Distinguish source changes from wire changes
- **WHEN** startup high 的活动回合中接受显式 high，之后再次显式 high
- **THEN** 首次来源变化增加选择序号，第二次不增加；两次 pending 均为 false，缓存身份不变

#### Scenario: Ignore a delayed older observation
- **WHEN** 同一控制实例旧查询的 low 序号快照在较新 high 序号已经观察后到达
- **THEN** 不回退选择或草稿；同序号的过期操作回复仍须通过请求/操作身份检查

#### Scenario: Ignore an observation from an earlier mount of the same thread
- **WHEN** 用户从 A 切换到 B 再切回 A，初次挂载 A 的查询回复在新的 A 已挂载后才到达
- **THEN** 新的 A 仍以其自身的序号和选择为准，旧回复即使 thread ID 与序号看似匹配也不能更改观察水位或界面

#### Scenario: Reject revision overflow without wrapping
- **WHEN** 测试将序号固定在 uint64 上限后请求不同选择
- **THEN** invalid_command 拒绝且原选择/序号不变，同值查询/选择与正常关闭仍可完成

### Requirement: Setting admission is atomic and independent from observation failures

新选择、控制结果及其有界关联设置快照 MUST 在状态交换前完整预检；验证或构造/编码失败 SHALL 拒绝且保留原选择和序号，不能先改变状态再报告 rejected。接收后结果确定，调用方取消、UI 结束反馈或 stdout 背压/断管 MUST NOT 撤回选择、重试模型或制造持久化设置事实。

context/cache 观测 SHALL 独立于选择接收：重建观测失败不得把成功设置改报 rejected，不得以旧 profile 的数值冒充新 profile。effort.query SHALL 仍可返回合法设置；context 查询沿既有安全失败规则处理。当前已接收的模型工作及 shutdown cleanup 仍由原 owner 完成。

#### Scenario: Reject before changing a selection
- **WHEN** 确定性注入使候选设置快照或控制结果预检失败
- **THEN** 返回拒绝，原选择/序号/窗口保持，无 Provider 或磁盘副作用

#### Scenario: Keep a successful setting after a context observation failure
- **WHEN** high 设置合法且已接收，但 idle context 观测重建失败
- **THEN** 设置仍 accepted，effort.query 显示 high；context 查询安全失败而不显示不匹配旧数值，不隐式压缩或重发

#### Scenario: Lose output after an accepted setting
- **WHEN** 成功设置的 response 已交付而 snapshot 因 stdout 断管无法交付
- **THEN** 不回滚选择或重发控制/模型请求；按原输出故障路径等待 worker 和 lease 清理，不声明设置持久化

### Requirement: Restart restores native history and initializes a new selection

effort override 和选择序号 SHALL 只存在于当前进程的活动 thread 控制实例。重启恢复 SHALL 使用本次启动配置；同进程的聊天内成功接管 SHALL 为目标创建新控制实例，选择初始化为冻结启动配置，来源为 startup，序号为 0。返回此前访问的 thread 同样初始化。接管前取消或可恢复失败 SHALL 保留原控制实例的选择、来源和序号。宿主 MUST NOT 从 journal、摘要正文、Skill 或旧 UI 内容推断设置。

历史不新增 effort record、配置原生 item、格式版本或兼容 reader；恢复不自动联网、不重跑工具或 Hook。用户显式重新选择相同设置并提交相同新输入后，uninterrupted/restored 的完整逻辑 request canonical bytes、原生顺序和 request/native/profile fingerprint MUST 等价，WebSocket 恢复按既有 full 重建规则处理。两边选择序号即使不同也不能影响这些身份。headless 查询及设置的运行与协议语义保持原样。

#### Scenario: Restart after a process-only override
- **WHEN** 进程以 high 配置启动并在内存选择 low，退出后以 high 配置恢复
- **THEN** 初始选定值为 high，旧 journal bytes 不变；只有新显式输入后才可生成请求

#### Scenario: Reset selection across two switches in one terminal
- **WHEN** 启动配置为 high，A 选择 low 后切换到 B，B 选择 auto 后切回 A
- **THEN** 每次成功切换后的初始选择均为 startup high、序号 0，原生历史及已持久化任务从目标恢复，切换不发出模型请求

#### Scenario: Keep the original selection when a switch is not adopted
- **WHEN** A 已选择 low，聊天内恢复尝试在接管前取消或被拒绝
- **THEN** A 的选择、来源和序号保持不变，释放交接许可后继续按原捕获规则处理输入

#### Scenario: Reapply a selection after restore
- **WHEN** 同一历史分别在不中断路径和恢复路径显式选定 low，再提交相同输入
- **THEN** 两家请求各自逐字节等价，opaque 哨兵完整且已执行工具和 Hook 次数为零
