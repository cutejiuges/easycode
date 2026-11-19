## ADDED Requirements

### Requirement: In-chat resume opens the existing picker or selects an exact thread

交互聊天 SHALL 提供 `/resume` 与 `/resume <thread-id>`。无参数时打开同一终端内的选择器，默认按规范化的启动工作目录列出候选，复用有界元数据搜索、刷新、Ctrl+A 范围切换和显式只读预览。候选 MUST 排除当前线程。显式参数 SHALL 只接受一个完整、规范的 UUIDv7 thread ID，按该 ID 定位，适用于未出现在已加载列表中的线程。当前线程 ID、非法 ID 和多余参数 SHALL 在开始目标恢复前以安全英文诊断拒绝。命令本身不得成为对话输入、Skill 调用、文件引用或 Provider 请求。

当前目录没有其他候选时，选择器 SHALL 明示该范围没有其他会话，并继续支持 Ctrl+A 切换范围及 Ctrl+R 刷新。发现、刷新和预览可恢复失败时 SHALL 保持选择器可用，显示安全英文诊断并保留查询和原选中身份。已显示结果的可操作性仍遵守范围与刷新契约。

只有当前会话真正空闲，并且没有接纳前准备、待提交、排队、取回中或尚未处理的返还输入时，系统才 SHALL 接受该命令。遇到忙碌状态 SHALL 提示处理当前操作或输入后重试。系统 MUST NOT 自动排队执行恢复、自动中断当前工作或丢弃未处理的输入。

#### Scenario: Find another session in the current workspace
- **WHEN** 空闲聊天提交无参数的 `/resume`
- **THEN** 同一终端打开选择器，按启动工作目录列出其他线程，用户可以搜索、刷新和预览

#### Scenario: Explore another scope when the local list is empty
- **WHEN** 当前目录只包含活动线程，用户打开 `/resume`
- **THEN** 选择器说明当前范围没有其他会话，保留 Ctrl+A 与 Ctrl+R。扩大范围后仍不显示活动线程

#### Scenario: Keep picker state after a recoverable browsing error
- **WHEN** 已设置查询并选中线程的用户遇到可恢复的刷新或预览错误
- **THEN** 选择器保留查询和源选中身份，标明错误与已显示结果的状态，可返回列表、刷新或取消

#### Scenario: Resume by an ID outside the loaded list
- **WHEN** 用户提供未出现在已加载列表中的同目录兼容线程的完整 ID
- **THEN** 只尝试恢复该线程。提交当前线程 ID、部分 ID 或多个参数时均显示本地错误，不启动目标恢复

#### Scenario: Preserve pending work on a busy resume attempt
- **WHEN** 用户在当前操作尚未收口或仍有排队、取回中或未处理的返还输入时尝试 `/resume`
- **THEN** 恢复不进入后台队列，当前操作与输入仍按原路径处理，界面提示待空闲后显式重试

### Requirement: In-chat resume isolates directory and provider selection

聊天内只允许接管 creation cwd 与启动工作目录一致、Provider family、wire 与 model 完全匹配的 root thread。全目录范围中的其他目录线程 SHALL 可浏览和只读预览。只有在选择器列表中按普通 Enter 才能发起所选线程的恢复。预览不接收恢复操作，Esc 关闭预览并返回原列表与选项。

列表拒绝跨目录选择时 SHALL 保留原查询与选中身份，并显示安全转义的创建目录和 `easycode --resume <thread-id>`，提示从其创建目录以兼容配置启动。提示中的命令与完整 UUID SHALL 可通过换行或滚动读取，窄屏也不得丢失完整 ID。列表中的三元组不匹配 SHALL 显示差异并阻止接管，目录与配置同时不匹配时 SHALL 允许查看两方面信息。直接指定 ID 遇到目录不匹配时可使用带完整恢复命令的通用目录提示，遇到配置不匹配可使用安全的配置不兼容诊断。提示不得展示配置文件路径、自动切换目录、生成可执行的目录切换命令或操作剪贴板。实际恢复仍重新核验目录与配置。

#### Scenario: Browse another workspace without entering it
- **WHEN** 用户在窄屏通过 Ctrl+A 找到其他目录的线程，在预览中按 Enter，按 Esc 返回列表后再按 Enter
- **THEN** 预览不启动恢复。列表拒绝接管并保留查询和选择，用户能从可滚动或换行的详情读取创建目录、完整 UUID 和独立恢复命令，当前聊天和工作目录保持原状

#### Scenario: Keep incompatible sessions discoverable
- **WHEN** 列表中某线程同时来自其他工作目录且 Provider family、wire 或 model 不匹配
- **THEN** 仍可查看其已支持的只读预览，选择器提供目录提示和配置差异并拒绝接管

### Requirement: In-chat resume owns focus through cancellation and handoff

选择器及直接恢复进度 SHALL 独占终端输入，显示加载、恢复或清理状态。列表中的 Esc 沿用关闭详情或预览、清空搜索的返回层级，最外层 Esc 取消并返回聊天。接管前，Ctrl+C 取消聊天内恢复，正式恢复中的 Esc 也 SHALL 请求取消。终端事件循环在选择器或交接等待期间收到的按键由当前焦点消费。重复 Enter、文字或粘贴不得产生第二次恢复或缓存为后续聊天输入。

接管前取消，或已受理的正式恢复发生可恢复失败时，系统 SHALL 在确认清理后返回原会话并显示安全通知，按当前终端尺寸还原原 composer 的准确正文和光标。发现、预览及列表拒绝仍留在选择器。目标的有界初始历史页及身份 SHALL 在接管前验证，已验证的空历史以明确空状态处理。初始页缺失、无效或与目标身份不符时，正式恢复 SHALL 在接管前失败。

接管获准后，界面 SHALL 等待旧会话清理完成，显示目标身份与已验证的初始历史或空状态，再启用目标输入。清理期间的 Esc SHALL 被消费，保持进度可见且继续交接。Ctrl+C 或终端退出信号 SHALL 由宿主接手退出和清理，不能回滚至源会话。旧草稿不得成为目标输入。两次进入同一线程也 SHALL 有独立的界面操作身份，迟到的列表、预览、控制流或详情结果不得替换当前状态。启动选择器的按键、退出和跨目录确认仍遵循启动入口的契约。

#### Scenario: Cancel a pending switch and continue the original session
- **WHEN** 用户在目标接管前取消正在加载或恢复的会话，期间调整终端尺寸
- **THEN** 等待取消清理完成后返回原聊天，保留原正文和光标且采用新尺寸，目标不会因迟到结果成为活动线程

#### Scenario: Return from a failed formal recovery
- **WHEN** 列表确认或直接 ID 恢复已受理，但目标忙碌，或其初始历史缺失、无效或身份不匹配
- **THEN** 在清理确认后返回原聊天，显示可安全展示的失败原因，原正文及光标准确复原

#### Scenario: Ignore escape while completing an accepted handoff
- **WHEN** 已接受接管但旧会话尚在关闭，用户按 Esc
- **THEN** 继续显示清理进度，不退出或回滚。清理成功后，显示目标身份及已验证历史，再接收第一条输入

#### Scenario: Ignore replies from an earlier activation of the same thread
- **WHEN** 用户从 A 切换至 B，再切回 A，第一次打开 A 时发出的异步结果迟到
- **THEN** 第二次打开 A 仅接受本次激活的结果，旧界面投影和草稿不能进入当前会话
