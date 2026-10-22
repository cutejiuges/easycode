## MODIFIED Requirements

### Requirement: Runtime commands are versioned and strongly typed

内部输入控制面 SHALL为`submit_input`、`resolve_tool_approval`、`interrupt`和`shutdown`提供唯一当前的强类型command、专属constructor和validator；内部command不携带format revision，也不存在按revision选择的decoder。每条command MUST携带非空request identity；`submit_input` MUST携带合法UTF-8、去除外围空白后非空且不超过4 MiB的文本；`resolve_tool_approval` MUST携带expected active turn ID、approval request ID、invocation ID、candidate fingerprint、允许的typed decision和仅适用于deny的可选16 KiB反馈；`interrupt` MUST携带预期活动`turn_id`，`shutdown` MUST NOT接受无关payload。

command kind与typed payload不匹配、非法ID/fingerprint/decision、非法UTF-8、超限内容和不适用于该kind的字段 MUST在产生排队、approval fact、turn ID、Session写入或Provider/工具副作用前失败。command、提交结果与拒绝原因 MUST NOT使用或暴露无约束`any`。外部headless stream-json的`version: 1`仍只投影当前已支持的外部command；本变更不向无交互外部协议暴露可阻塞的approval response wire，也不得把外部wire version复制进Runtime核心类型。

#### Scenario: Accept a valid submit command
- **WHEN** 宿主提交具有唯一request identity和合法文本的当前`submit_input`
- **THEN** 控制面返回强类型提交结果，并且后续生命周期可以通过同一request identity关联

#### Scenario: Accept a correlated approval command
- **WHEN** 交互式宿主提交与当前active turn、request、invocation和fingerprint匹配的`resolve_tool_approval`
- **THEN** 控制面返回强类型accepted结果并把decision交给唯一turn owner

#### Scenario: Reject an invalid command without side effects
- **WHEN** command包含未知kind、非法target turn、空白/超限文本、过期approval identity或kind/payload不匹配
- **THEN** 控制面返回稳定英文`invalid_command`、`invalid_input`或`stale_approval`错误
- **THEN** command不进入队列、不写approval fact、不分配turn ID且不调用Provider或executor

#### Scenario: Adapt the current external protocol once
- **WHEN** headless adapter成功解码一个stream-json`version: 1`command
- **THEN** adapter构造不含version的内部强类型command
- **THEN** Runtime不暴露外部JSON decoder或第二套版本化command DTO

### Requirement: One owner decides admission and turn execution

每个活动thread SHALL只有一个输入控制owner串行决定command admission、活动turn身份、活动approval、follow-up queue转换和关闭状态。空闲时接受的`submit_input` SHALL得到`starting` disposition；存在活动turn时接受的输入 SHALL得到`queued` disposition并进入该Session实例持有的FIFO follow-up queue。`resolve_tool_approval`只能路由到同一owner当前记录的一个活动request，不进入follow-up queue，也不得启动新turn。

并发调用方不得通过先读取状态再自行启动turn或完成approval的方式绕过owner。任意时刻最多一个`RunTurn`可以修改该Conversation；先被owner接受的输入 MUST先开始，输入不得合并、重排或并行执行。活动turn期间接受的普通输入 SHALL形成后续独立turn，不得注入当前Provider stream、approval feedback或称为same-turn steer。

#### Scenario: Serialize concurrent submissions
- **WHEN** 两个submit在空闲thread上并发到达
- **THEN** owner按唯一顺序接受它们，恰好一个得到`starting`，另一个得到`queued`
- **THEN** Provider任意时刻只存在一个活动stream

#### Scenario: Route approval without creating a turn
- **WHEN** 当前turn等待approval且匹配response到达
- **THEN** owner把response交给该turn并保持原turn identity
- **THEN** response不进入普通输入FIFO、不生成用户turn也不建立并发Provider stream

#### Scenario: Preserve one input per turn
- **WHEN** 活动turn期间连续接受两个follow-up输入
- **THEN** 当前turn先收口，两个输入再按FIFO各自启动新的独立turn
- **THEN** 系统不拼接两个文本，也不把它们加入当前turn的Provider请求

#### Scenario: Close the lost-wakeup window
- **WHEN** 新输入或approval response在owner观察状态转换之间到达
- **THEN** owner重新观察已接受工作并最终处理它
- **THEN** 命令不会滞留到下一条无关命令到达后才执行

## ADDED Requirements

### Requirement: Approval response admission is correlated and race-safe

owner SHALL只在expected turn仍活动、request仍pending且invocation/fingerprint完全匹配时接受`resolve_tool_approval`，并返回`resolving` disposition。不存在活动approval SHALL返回`no_active_approval`；turn已替换 SHALL返回`turn_mismatch`；request或fingerprint过期 SHALL返回`stale_approval`。所有拒绝都不得改变grant、Session、队列或文件。

同一response request identity与同一approval的重复resolution MUST被去重。resolution一旦被owner接受，调用方context后续取消不得撤回decision；owner负责返回确定的durable decision/result或失败终态。interrupt/shutdown与resolution竞态必须由同一owner排序，不能误批准随后turn的request。

#### Scenario: Reject a response for an old turn
- **WHEN** approval response指向已结束turn而另一turn当前活跃
- **THEN** owner返回`turn_mismatch`且不解决当前request

#### Scenario: Deduplicate an approval response
- **WHEN** 相同response request identity被并发或重复提交
- **THEN** owner至多接受一次，且只产生一个durable approval outcome

#### Scenario: Caller cancellation follows accepted resolution
- **WHEN** owner已返回`resolving`后调用方context取消
- **THEN** owner继续完成decision的durable结果，调用方取消不能造成未知approval状态

