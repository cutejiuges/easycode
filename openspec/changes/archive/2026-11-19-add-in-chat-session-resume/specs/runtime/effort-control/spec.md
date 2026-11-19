## MODIFIED Requirements

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
