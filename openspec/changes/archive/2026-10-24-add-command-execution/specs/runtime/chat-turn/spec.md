## ADDED Requirements

### Requirement: Command execution preserves durable ordering and cleanup ownership

Runtime SHALL将每个`process.exec`作为exclusive调用处理：ready batch与Provider finalizer成功后冻结候选并请求approval，matching allow fact与execution started依次成功`Sync`后才可把进程交给executor。前序调用results未durable、approval未收口或started未accepted时不得启动进程；命令result与匹配Provider-native output未durable时不得启动后序调用或下一sample。

execution started是命令副作用接收线性化点。此前取消不得创建进程；此后Runtime与executor的唯一owner MUST即使在turn取消、timeout、approval abort或shutdown下仍持有进程组、stdout/stderr readers与最终done信号，直到终止、wait、输出关闭和cleanup完成。shutdown等待期限到达时可以升级终止手段，但不得释放资源所有权或关闭Session writer；无法确认命令结果 SHALL poison Session并以`outcome_uncertain`配对收口。

#### Scenario: Persist approval and admission before spawn
- **WHEN** 用户批准一个Exec调用
- **THEN** ready、allow fact和execution started按顺序durable后executor才可启动进程

#### Scenario: Cancel an accepted command
- **WHEN** execution started已accepted后turn context取消
- **THEN** Runtime恰好调用一次executor并等待进程组终止、两个输出reader关闭和最终result
- **THEN** turn在cleanup完成前不发布terminal也不接受下一turn

#### Scenario: Keep an Exec exclusive
- **WHEN** 同一sample按顺序包含Read、Exec和Grep
- **THEN** Read完成且result durable后Exec独占执行，Exec result durable后Grep才可准入

#### Scenario: Retain process identity through cleanup
- **WHEN** 直接子进程已退出但原组清理尚未结束
- **THEN** process owner保留未回收leader身份，完成组信号和探测后才最终Wait
- **THEN** Runtime仍等待cleanup done且不得提前开启下一turn，回收后的旧PGID不得用于shutdown升级

#### Scenario: Incomplete output cleanup poisons the session
- **WHEN** 直接命令退出后输出reader在排空期限内无法完成
- **THEN** owner关闭自身pipe并等待reader退出，Runtime保存outcome_uncertain并禁止后续Provider或工具副作用
- **THEN** result不能代替cleanup done，尚未Wait的直接子进程仍由唯一owner持有

### Requirement: Command progress is typed bounded and transient

RuntimeEvent SHALL提供命令started、progress和terminal的强类型语义投影，全部绑定当前session/thread/turn/invocation identity。progress SHALL只包含规范化命令摘要、workspace相对workdir、elapsed time、累计stdout/stderr行数与字节数、truncated状态和有界近期输出；payload MUST验证UTF-8与大小，不得包含PID、绝对路径、完整环境、secret、Provider wire、Session record或无界输出。

progress属于宿主瞬态视图，不得写入Session事实源，也不能替代最终tool result。Runtime可以合并或丢弃中间progress以施加背压，但 MUST保持started先于progress、terminal晚于最后已交付progress，且任何progress都不能在turn terminal之后出现。

command terminal SHALL额外携带与durable tool result逐字节一致的typed result preview，UTF-8 bytes上限256 KiB，供TUI命令详情展示。started/progress MUST拒绝携带result preview；terminal preview不得增加Session record或headless JSONL词汇，不得在result durable或cleanup完成前发布。

#### Scenario: Project a frozen result only at terminal
- **WHEN** 命令cleanup完成且tool result已durable
- **THEN** command terminal携带相同preview bytes，TUI无需读取Session或executor即可展示详情
- **THEN** started/progress中的result preview字段被strict decoder拒绝

#### Scenario: Project bounded process output
- **WHEN** 活动命令持续产生stdout与stderr
- **THEN** Runtime发布有界typed progress并累计正确的行数、字节数与elapsed time
- **THEN** Session journal不增加progress record

#### Scenario: Reject progress after terminal
- **WHEN** executor在命令terminal或turn terminal后尝试发布输出更新
- **THEN** Runtime拒绝该协议顺序且不把迟到内容交给TUI或headless
