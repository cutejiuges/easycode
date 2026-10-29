## MODIFIED Requirements

### Requirement: Command progress and result use a compact Claude-style timeline

命令排队时TUI SHALL显示紧凑命令与`Waiting`；执行后显示`Running`、相对workdir、elapsed time、累计输出规模和有界近期输出；完成后显示exit code或timeout/cancelled/failed状态以及truncated提示。普通视图的命令摘要最多两行和160个Unicode code points。Ctrl+O SHALL 打开历史命令列表并默认选中最新项，↑/↓ 选择、Enter 进入详情，复用完整命令和冻结 preview；运行中只显示有界 recent 输出。

详情 SHALL 支持 Tab 切换 stdout/stderr、PgUp/PgDn 有界分页与 resize；Esc 从详情返回列表，再次 Esc 关闭，Ctrl+O 直接关闭覆盖层。命令活动时 Ctrl+C 仍定向中断，普通字符和 Enter 不得修改 composer 或提交并发 turn。显示副本 SHALL清理终端控制字符，不改变候选和冻结结果。

#### Scenario: Inspect a completed command
- **WHEN** 用户在命令终态后按 Ctrl+O 并选择该命令进入详情
- **THEN** 详情立即展示相同完整命令和冻结有界结果，存在保存引用时通过 typed 宿主查询异步获取首个输出页
- **THEN** View/resize 不读取外部资源，分页 I/O 只在异步查询执行，不改变候选或冻结模型输出预算

TUI 只能消费 typed RuntimeEvent 和宿主 typed 命令查询结果，不得直接读取 executor、Session、文件系统、PID 或 Provider native item。活动Exec期间composer不得提交并发turn，定向interrupt保持可用；只有Runtime已经完成进程cleanup并发布terminal后，TUI才可恢复idle输入状态。

#### Scenario: Render deterministic command progress
- **WHEN** 固定进度序列包含command started、两个output progress和completed
- **THEN** 固定尺寸snapshot依次展示Waiting、Running、近期输出、elapsed/bytes和最终exit code

#### Scenario: Show a truncated result safely
- **WHEN** 命令结果报告omitted bytes
- **THEN** TUI显示稳定省略提示与保留头尾，不尝试读取未授权artifact或绝对临时路径

#### Scenario: Wait for cleanup after interrupt
- **WHEN** 用户中断一个有子进程的活动命令
- **THEN** TUI保持取消中状态直到Runtime terminal，并且下一turn不会与旧进程cleanup重叠

列表每页 SHALL 不超过 50 项，保存内容单页请求不超过 64 KiB 原始 bytes，最多缓存 4 页、每页展示文本不超过 256 KiB。查询 MUST 绑定当前 thread 与选择 generation，切换或关闭后丢弃旧回复。保存完整性与预览省略 SHALL 分别显示；missing/corrupt 仍保留原冻结 preview，用户查看 MUST NOT 触发模型请求或工具 ledger。

#### Scenario: Ignore a late page
- **WHEN** 用户从命令 A 切换到 B 后 A 的页回复到达
- **THEN** B 的内容与游标不受影响，旧请求被取消并由 owner 收尾

#### Scenario: Browse restored commands
- **WHEN** 恢复后打开列表并查看较早命令 stderr
- **THEN** 按 seq 找到历史命令、独立切流翻页，零 Provider 请求、零 tool ledger 写入

#### Scenario: Show partial storage
- **WHEN** artifact 只保存前段而预览包含末尾
- **THEN** 详情明确标注保存范围，stored EOF 不被显示为完整输出结束
