## Context

动机与交付范围见 [proposal.md](proposal.md)。当前 `internal/tui/model.go` 同时持有 `[]rune` 草稿、transcript、审批和命令详情，`handleKey` 只支持末尾编辑；`View` 输出整个 transcript 和单行输入。`submitTurn` 异步调用现有 `ChatSession.Submit`，成功返回事件 channel，失败返回 `ErrorMsg`。Runtime 已接受合法多行 UTF-8 文本，并通过 `protocol.MaxInputBytes` 限制为 4 MiB；无需为输入编辑新增内核协议。

已读取当前 `tui/basic-chat`、`runtime/input-control`、Roadmap、总体架构、TUI 模型/快照/审批/命令详情测试及 app 启动路径。原有活动期禁止 composer 提交、审批默认拒绝、定向取消和清理后恢复输入继续生效。

### 参考证据与适用边界

参考仓库只读，实际路径为 `/Users/cutejiuge/IdeaProjects/claude-code-sourcemap` 和 `/Users/cutejiuge/IdeaProjects/codex`。以下路径相对各自仓库根，不形成构建依赖。

| 来源与快照 | 阅读位置 | 观察与设计结论 |
|---|---|---|
| Claude sourcemap `a8a678cb6244e6770e1e421767ff0987a1d95549`，工程文档标识 2.1.88 | `restored-src/src/hooks/useTextInput.ts`，`handleEnter`、`upOrHistoryUp`、`downOrHistoryDown`、`mapKey` | Alt/Shift+Enter、反斜杠+Enter 换行；先移动视觉行再进入历史。采用交互顺序，不移植平台原生修饰键探测、双 Esc 或 kill ring。 |
| 同上 | `hooks/useArrowKeyHistory.tsx`，`components/PromptInput/PromptInput.tsx` | 历史浏览保存草稿，异步加载避免快速按键旧状态；PromptInput 在提交前 trimEnd，并用多处焦点守卫阻止双重提交。EasyCode 使用同步纯内存历史与单一输入路由，不复制全局加载状态、磁盘历史和多处 guard。 |
| 同上 | `hooks/usePasteHandler.ts`，`components/PromptInput/inputPaste.ts` | 从统一 parser 的 isPasted 识别粘贴，注释记录第二个 stdin listener 导致丢字符；分块拼接有 pending ref 防 Enter 竞态，大文本使用占位符。采用单 reader 和原子文本插入；不复制异步图片检测、定时拼接和占位符。 |
| 同上 | `utils/Cursor.ts`、`components/BaseTextInput.tsx` | 字素测量、wrap 与 viewport offset 关联；Cursor 文档描述 NFC 规范化。采用统一布局映射，但不做 NFC/NFD 规范化，避免改写代码字节。 |
| Codex 工作区 `6f8bf683cee83ed4d580a2e106b587d8451db936` | `codex-rs/tui/src/bottom_pane/chat_composer.rs`，提交、paste、焦点路由和相关测试 | 提交前处理粘贴，校验抑制后恢复草稿；大粘贴占位符有所有权/展开/历史关联成本。采用输入快照和失败恢复，首版直接保留文本。 |
| 同上 | `bottom_pane/chat_composer_history.rs` | 历史记录折叠相邻重复，区分 replay/local；非空输入通常需匹配 recall 文本且位于边界才进入历史。采用去重和 replay 输入来源，导航触发选择 Claude 风格，不混搭两个规则。 |
| 同上 | `bottom_pane/textarea.rs` | 字素边界移动、显示宽度、preferred column、wrap cache；测试覆盖 emoji 和组合输入。采用这些不变量；避免因 Go rune 索引误当终端列而错位。 |
| 同上 | `bottom_pane/paste_burst.rs`、composer 的 `handle_input_basic_with_time` | 无 bracketed paste 的启发式涉及 ASCII 首字符等待、IME 分流、Enter 抑制、flush 与 clear 次序以及可控时钟。当前切片不复制该启发式，不保证未知终端中的快速逐键粘贴不会触发 Enter。 |
| Codex 文档指定基线 `c248f6d48b` | 用 `git show` 核对 `chat_composer.rs`、`textarea.rs` | 历史、粘贴状态、字素和显示宽度的核心参考在基线已存在；不将当前工作区新增能力冒充文档基线。 |
| 本地 Bubble Tea `v1.3.10` | `key.go`、`key_sequences.go`、`tea.go` | 默认开启 bracketed paste；`Key` 有 Type/Runes/Alt/Paste，无通用 Shift 字段。完整 paste 作为 `KeyRunes{Paste:true}` 发出；在末标记之前积累原始缓冲，且解码时跳过 RuneError。能力承诺必须以已解码输入为界。 |

这些是本地快照证据，不声明对应产品最新发行版都具有完全相同行为。参考工程的注释用于定位原因，实际处理分支与测试用于核对行为；没有构建或执行参考工程。

## Goals / Non-Goals

**Goals:** 在现有宿主边界内建立可测的文本编辑、布局、历史和提交事务；保持正文、显示副本与内核事实分离；以明确输入事件和固定尺寸验证交互。

**Non-Goals:** 不为未来附件、补全或 Vim 预建 facade；不改全局终端输入解析、不新增 stdin reader/goroutine、不升级键盘协议。沿用普通终端模式与现有 transcript 输出，不引入全屏历史浏览。完整范围见 proposal。

## Decisions

### 1. 组件边界与数据流

在 `internal/tui` 内使用不导出的具体类型，按职责拆为 `composer.go`、`composer_edit.go`、`composer_layout.go`、`composer_history.go` 和 `composer_input.go`（实现可合并同职责小文件）。不引入通用 editor 包或策略 registry。

```text
Bubble Tea KeyMsg / WindowSizeMsg
               |
         Model focus router
          /      |       \
   approval    detail   composer
                          |
                  typed edit / submit intent
                          |
                   Model pending snapshot
                          |
                  tea.Cmd -> ChatSession
                          |
                   AgentLoop -> Runtime

SemanticHistoryView -> local recall seed
RuntimeEvent        -> transcript projection
```

composer 拥有正文、字节光标、目标显示列、视口起点、正文/宽度缓存键与布局结果；history 拥有有限条目和浏览快照。Model 拥有活动/等待提交状态与唯一 pending snapshot；composer 不持有 Session、Provider、context 或 I/O。输入动作使用封闭枚举/具体值，不用字符串动作分发或 `any` 返回业务结果。Bubble Tea 的框架消息接口仅留在宿主边界。

备选：引入 Bubbles textarea。当前模块未依赖 Bubbles，且本切片需要明确字素、精确文本、历史和提交快照规则，因此不为一个输入控件增加整套依赖，也不将其默认按键行为隐式变成契约。

### 2. 正文、光标与编辑规则

正文使用合法 UTF-8 string，光标为字节偏移且始终位于扩展字素边界；使用已锁定的 uniseg 计算边界与宽度。不修改 uniseg 包级选项。文本编辑产生新值，snapshot 不共享可变切片/map；Model 值复制不得让旧快照被后续输入修改。

左右、Backspace、Delete 分别移动/删除一个字素；换行也是可删除边界。插入组合字符可能与前后字素合并，插入后重新定位到插入结束所在字素的末边界，不保留字素中间光标。Home/End 与 Ctrl+A/E 到逻辑行首/尾；Ctrl+B/F 对应左右。上下移动依据软折行后的视觉行，保持 preferred display column；短行取不越界的最近左侧字素边界，后续长行恢复目标列。水平移动、编辑、历史切换和 resize 清除 preferred column。

插入统一将 CRLF 和独立 CR 规范化为 LF；除此以外不做 trim、Unicode 规范化或内容过滤。全空白校验只决定是否允许提交，不修改正文。手动 Tab 保持现有无操作语义；粘贴中的 Tab 作为正文保留。

### 3. 按键表与优先级

| 场景 | 操作 | 行为 |
|---|---|---|
| 空闲 composer | Enter | 有效非空白输入发起一次提交 |
| 空闲 composer | Alt+Enter / Ctrl+J | 插入 LF |
| 空闲 composer | 普通 Enter 且光标前一个字素是反斜杠 | 删除该反斜杠并插入 LF，不提交；只处理光标前一个反斜杠，不做奇偶转义 |
| 空闲 composer | Left/Right、Ctrl+B/F | 字素移动 |
| 空闲 composer | Up/Down | 先视觉行移动，到顶/底才尝试历史 |
| 空闲 composer | Home/End、Ctrl+A/E | 逻辑行首/尾 |
| 空闲 composer | Backspace/Delete | 删除前/后字素 |
| 空闲 composer | Esc；非空草稿 Ctrl+C | 保持当前无操作行为，不引入双击清空 |
| 空闲空草稿 | Ctrl+C | 退出并沿用 app 清理 |
| 等待提交/活动 turn | Esc/Ctrl+C | 保留既有取消意图/定向中断，等待清理；普通文本、粘贴和 Enter 不修改草稿或排队 |
| 审批/详情 | 各自原有按键 | 只影响聚焦控件；粘贴不是审批快捷键或聊天提交 |

路由顺序：审批 > Ctrl+O/详情 > 等待提交或活动 turn > 空闲 composer。一次事件只被一个分支消费，不向下冒泡。审批解决和详情关闭后恢复相同草稿、字节光标与历史浏览状态；resize 可重新计算视口。审批反馈仍沿用现有单行契约，不在本次改为多行。

Shift+Enter 只有终端映射为已支持换行序列时才生效。帮助固定宣传 Alt+Enter / Ctrl+J，而不显示未经支持检测的 Shift+Enter。当前 KeyMsg 不区分 press/repeat/release，不能照搬 Codex 的 fresh-key 过滤；单 pending/active 状态负责防并发重复提交。

### 4. 粘贴与限额

在聚焦 composer 中先检查 `Paste`，再处理快捷键。paste 的 Runes 整体转为文本并规范化换行，一次插入到光标，不将内含 Enter、Esc、Ctrl+C、Tab 或反斜杠解释为动作。粘贴完成后的独立 Enter 正常提交；无需基于时间猜测它属于哪次粘贴。

草稿限制复用 `protocol.MaxInputBytes`（4 MiB UTF-8 bytes）。以规范化后的完整替换结果校验，超过则整次拒绝，原正文/光标/历史保持；显示不包含正文的英文错误，例如 `invalid_input: input exceeds 4194304 bytes`。连续输入和历史恢复同样遵守上限，禁止静默截取半个字符。完整 paste 为空时无操作。

该限制只覆盖 TUI 接收到的 decoded event 和持有的正文。Bubble Tea 在 paste 末标记前的缓冲不受此限制，且 RuneError（包括字面 U+FFFD）可能在框架层被跳过；本次既不承诺原始终端字节往返，也不提供恶意未闭合 paste 的内存上限。spec、README 与测试应明确区分“到达 composer 的文本保真”和“终端原始输入保真”。无 bracketed paste 的输入按普通按键处理，不启动毫秒计时启发式，不添加假安全保证。若将原始解码保真/预解码上限纳入范围，必须先更新本 change 的方案与契约，不能实施时偷偷替换 reader。

### 5. 安全显示与布局同源

正文和显示副本分离。LF 划分逻辑行，Tab 按 4 列 tab stop 显示为空格；C0（除 LF/Tab）、DEL、C1 与双向控制字符（U+061C、U+200E/F、U+202A..E、U+2066..9）显示为可见 ASCII 转义。原始控制字符不得进入终端输出；提交保留 decoded 正文原值。ZWJ、变体选择符作为字素内部内容保留，不粗暴清除所有格式字符。孤立零宽字素增加仅显示的占位基底；无法容纳的宽字素用一列替代标记展示，底层文本不变。

用同一布局结果计算视觉行、字节范围、显示列和光标位置，禁止 View、移动和测量各自切一次文本。不在转义显示中间放置正文光标。软折行边界光标归属下一行，行尾提供一个可绘制的虚拟空格，以区分满宽末尾和不可见光标。窄于正常提示符时缩减提示符，保证内容区至少一列；未收到尺寸时使用 80x24。

正文区域最多 8 行且不超过 `max(1, height-4)`；光标移动和编辑自动调整 viewport。顶部/底部省略提示及状态提示不得挤掉唯一光标行；极小窗口优先显示输入和光标，按空间依次隐藏帮助、边框/省略提示。不是完整屏幕重排，原有 transcript 仍使用现有终端回滚方式。

首版统一采用无色轻量边框和可见 ASCII 插入位置标记，不增加颜色探测、主题或跨包构造器参数。布局为标记固定预留一列，避免每次移动光标改变正文折行；极端单列窗口只显示光标所在位置。软件光标仅在可编辑 composer 显示，不启动 blink goroutine，也不在 View 调用终端 cursor I/O。

用户提交后的 transcript 显示也使用安全显示函数，避免正文中的 ESC 在提交前安全、提交后却被原样输出。不在本切片重写 assistant Markdown 或通用工具渲染。安全显示输出绝不回流模型输入、Session 或 fingerprint。

布局在文本 revision/宽度/显示模式变化时更新，纯 View 只读取结果；光标移动复用布局。优先按逻辑行保存边界/宽度结果，避免每个字素一个 heap 对象。首版允许有界 O(n) 编辑/重排，n<=4 MiB；不提前引入 rope。用接近上限的 ASCII/CJK/长单行样本记录分配和耗时，若实测无法响应再在 apply 内优化同一契约。

### 6. 历史语义与预算

历史包含当前 root thread 在启动时已给 TUI 的 SemanticHistoryView.UserText，以及本进程接收成功的新输入；不读 Session 文件或全局历史。新会话为空；resume 只从现有语义投影种子填充，不能从 transcript 反推 native 请求。种子同样应用换行规范化与上限；过大条目不入 recall，不改 transcript。

最多 100 条且正文合计最多 8 MiB；加入时折叠与最近条目完全相同的连续重复，按最旧优先淘汰至满足两个约束。每条至多 4 MiB，淘汰不改 Session。内存还允许一个当前草稿、一个浏览前快照和一个 pending 快照；这些不得累积成无限 undo 栈。

第一次在首视觉行按 Up 保存完整当前草稿/光标，召回最近输入，光标放在首视觉行最后一个可停靠位置（按软折行边界归属规则），便于连续向上浏览；Down 召回较新条目时光标放在末尾。多行条目内继续优先移动视觉行；到最后一项后 Down 恢复浏览前原文/光标并退出浏览。空白草稿也完整保存。

浏览中仅移动光标不结束浏览；第一次修改召回文本时结束浏览，该文本成为新工作草稿，旧历史条目不变，原浏览前快照释放。此为明确取舍：编辑召回内容表示采用它，不承诺多份草稿的撤销恢复。无历史、已到最旧、非浏览时底部 Down 均无操作。接收成功才加入历史；接收失败不加入；接收后的 provider 失败仍保留该输入供用户主动召回。

### 7. 提交接收与 turn 生命周期分离

```text
idle -- valid Enter --> submitting -- Submit accepted --> streaming
 ^                          |                               |
 +---- Submit rejected -----+                               |
 +---------------- terminal + cleanup ----------------------+
```

图中拒绝路径直接返回 idle；accepted 后只允许通过内核事件收口，不能因本地恢复草稿而重提。

Enter 时完成空白/字节检查，将草稿（包括光标、浏览状态）存为 immutable pending snapshot，立即进入 submitting，禁用第二次提交。此时不增加已接收的 transcript/history，也不丢弃输入。

tea.Cmd 成功返回 streamReady 后：追加一条用户 transcript、记录 recall、清空当前草稿并释放 pending，再开始按序消费 channel。成功返回表示进程内 admission，不表示 `turn_started` 已 Sync；显示不能声称已经 durable。现有 RuntimeEventTurnStarted 仍是内核事实边界。

Submit 返回错误：使用专门的内部提交失败消息恢复 pending 状态，返回 idle 并显示安全错误。不得用所有 ErrorMsg/turn_failed 统一恢复 pending。已接收后的 stream 错误、终态失败和取消保留既有 transcript，不自动恢复为待提交输入或重试；用户可主动从 recall 选择。

等待 Submit 时 Esc/Ctrl+C 设置已有 interruptNext 意图；成功后通过现有 session Interrupt 路径发送一次取消，接收失败则清除此意图。终态前保持输入冻结，资源 owner 和 shutdown 保持既有实现。Model 的单 pending 不允许异步结果跨越下一次提交，单测覆盖重复 Enter、失败与取消交错。

### 8. Provider、缓存和 Session

经用户确认补充：TUI 与双 Provider 的用户消息构造都移除 TrimSpace。Protocol、headless 输入只做空白判断，合法正文原样进入 Provider；TUI 仍在编辑阶段将 CRLF/CR 规范化为 LF，headless 不新增换行规范化。headless 发给模型的外围空白开始保留，这是明确批准的行为变化；输入验证、输出格式与协议版本不变。双 Provider 原生 user item/message 构造复用现有类型，只删除内容裁剪，不增加按宿主分流或兼容分支。

多行文字自然进入各自原生请求和 Session，保留双 Provider 的签名、encrypted reasoning、工具 facade 与调用顺序。终端宽高、草稿游标、历史索引和展示转义均不进入请求。不同正文可以改变完整请求 fingerprint，不能错误要求所有 fingerprint 不变；要求同一正文只改变 UI 操作时 canonical request 不变、稳定前缀不变，并验证 uninterrupted/restored 请求等价。

### 9. 验证矩阵

| 层 | 必须验证 |
|---|---|
| 编辑纯逻辑 | 行中插入；前后删除；Home/End；上下保持列；CJK、组合重音、ZWJ emoji、空行、尾换行、软折行边界；不共享可变快照 |
| 输入/限额 | Alt+Enter/Ctrl+J/反斜杠换行；paste 含 LF/CRLF/CR、Tab、控制字符及独立尾随 Enter；4 MiB 边界、超限原子拒绝；空 paste |
| 历史 | 100 条/8 MiB、相邻去重、淘汰、原草稿和光标恢复、编辑召回后脱离、resume seed、不产生磁盘 I/O |
| 提交/焦点 | rejected 恢复 vs accepted 失败不恢复；重复 Enter；pending interrupt；审批/详情隔离；活动期粘贴无排队；等待 cleanup |
| 固定 snapshot | 80x24、32x10、极小窗口，长单行/多行/控制字符/emoji/无色/首尾滚动和 resize；显示不含原始控制序列 |
| 输入适配集成 | 通过真实 Bubble Tea Program 和受控 reader 喂入完整/分块 bracketed paste、Alt+Enter、Ctrl+J；用同步屏障而非 sleep 断言调用次数/正文；退出后 reader/Program 清理 |
| 双 Provider | 同一多行文本经 TUI 提交后的 native user item 精确匹配；窗口/光标操作不改 canonical bytes；fixture/mock 验证恢复后的下一请求、原生顺序和稳定前缀等价 |
| 性能与工程 | 近上限布局/编辑 benchmark 记录分配与耗时，View 不反复重建布局；`make verify`；实际终端手动 smoke 并记录终端/版本/结果，未运行项如实列出 |

不以真实 API 调用验收；不复制参考工程测试，只针对 EasyCode 可观察行为编写断言。

## Risks / Trade-offs

- 终端 Shift+Enter、IME 与字宽存在差异 -> 支持明确的编码与 fallback；字素算法一致不等于所有终端字体绝对同宽，手工验证记录范围。
- 框架 raw paste 缓冲无上限、RuneError 被跳过 -> 明确 decoded-text 边界，不将 composer 限额宣传为完整终端输入防护；修复需要独立终端边界设计。
- 无 bracketed paste 时 Enter 无法可靠区分 -> 不引入会误判快速打字/IME 的时序算法，文档建议支持 bracketed paste 的终端。
- 4 MiB 编辑/布局可能占用较长主循环时间 -> 复用布局、减少元数据分配、预算内 benchmark；不在 View 做重复分词或重排。
- 去掉 TrimSpace 改变已有请求正文 -> 标识行为变化，明确仅空白验证，添加首行缩进和尾部空白的双 Provider 回归。
- 历史是在内存保留用户文本 -> 条数/字节双界限，不写日志或另一份磁盘记录；结束实例后释放引用。
- 当前主聊天 transcript 不限高 -> composer 独立限高提升输入可用性，但不声称解决完整 transcript 滚动和内存预算。

## Migration Plan

经后续明确 apply 请求后，先实现纯内存编辑/布局/历史，再替换 Model 草稿与提交分支，补集成和快照。直接删除旧单行分支，不保留 feature flag 或双 reader。无需 Session/native/headless revision 或数据迁移。

实现完成后同步 README、总体架构、Roadmap、pitfall；纠正 Write 状态矛盾并记录上述终端限制。执行 `make verify` 与实际终端验证；通过后再按流程同步 delta spec 并归档。若回滚，回退本次 TUI 代码/文档即可，现有 Session 文本记录仍由同一 reader 读取，不改写日志。
