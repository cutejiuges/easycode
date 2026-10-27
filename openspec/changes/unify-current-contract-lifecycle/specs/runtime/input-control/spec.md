## ADDED Requirements

### Requirement: Every chat host shares the current control lifecycle

TUI、一次性文本/JSON headless 和 stream-json SHALL 通过同一控制生命周期执行输入接收、定向中断、turn 调度和关闭。宿主 adapter MUST NOT 持有另一套直接启动 turn 的执行状态机。每个活动 thread 只有一个实际控制 owner 和一个有序输出消费者；应用 MUST 等待实际运行的 owner，而非另一个空闲 facade，才可释放其依赖资源。

宿主现有外部行为 SHALL 保持：一次性模式只提交一个输入且保留输出格式/退出码；TUI 活动期间禁止 composer 提交；stream-json 保持 FIFO、request identity 和独立外部 version。

#### Scenario: Run each host through one owner
- **WHEN** 相同文本分别通过 TUI、一次性 headless 和 stream-json 执行
- **THEN** 每个会话只有一个执行 owner，副作用与 durable terminal 都只发生一次
- **THEN** 各宿主仍产生原有格式的输出，不泄漏其他宿主的控制事件

### Requirement: Host detachment preserves cleanup ownership

宿主退出或输出失败后，事件分发 SHALL 能解除宿主发送背压并继续排空内核输出。正常消费时 MUST 保留事件顺序和 terminal；停止向已退出宿主投递不得省略 Session durable 收口。启动取消、输出分发和运行 owner SHALL 各有唯一清理责任与可等待的最终完成结果。

关闭等待超时 MUST NOT 释放仍被活动 turn 使用的 journal lease、workspace 或 repository。应用 SHALL 保留失败结果，执行明确的活动 stream 取消/强制清理路径，等待 worker、owner 与分发器退出，再按生命周期释放资源。重复关闭 SHALL 观察同一最终结果，不新建无 owner 的等待任务。

#### Scenario: Exit with a full host event buffer
- **WHEN** TUI 停止读取且事件 buffer 已满，随后请求关闭
- **THEN** 分发不再阻塞于已退出宿主，活动 turn 取消并完成唯一 durable terminal
- **THEN** worker、控制 owner 和分发器最终完成后，才关闭 writer 并释放 lease

#### Scenario: Cancel during startup
- **WHEN** 控制 owner 已启动而宿主启动 context 取消
- **THEN** 启动路径取消并等待已创建任务清理后返回，不能遗留孤立 owner

#### Scenario: Escalate a shutdown timeout
- **WHEN** 确定性注入使活动 stream 清理超出首次 shutdown 等待期限
- **THEN** timeout 被保留，应用请求解除真实 stream 阻塞并继续等待固定完成信号
- **THEN** 清理完成前其他进程仍不能取得活动 journal lease，完成后可以取得

#### Scenario: Repeat shutdown while cleanup is pending
- **WHEN** 多次关闭与 terminal 发布并发发生
- **THEN** 通道只由唯一 owner 关闭，不发生重复关闭、send-after-close 或第二次副作用
- **THEN** 所有关闭调用观察一致的最终清理结果
