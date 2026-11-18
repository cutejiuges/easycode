## 1. 纯内存契约与引用解析

- [x] 1.1 在 domain 增加封闭 whole/lines 文件引用及 typed 构造器、严格 decoder、validator；用表驱动单测验证相对路径、引号/合法 surrogate 解码、未配对转义拒绝、范围、溢出、零值及 4096-byte 边界。
- [x] 1.2 实现正文引用与编辑目标共用的纯解析规则，覆盖多行、Unicode/字素边界、代码围栏/行内代码、email、转义、@@、引号、范围、首次出现去重及 8/9 项；用属性/fuzz 验证有效目标与提交引用一致，并回归只扫描用户原文、不递归扫描附件/反馈或激活本地命令参数。
- [x] 1.3 实现文件附件、确定性 JSON 数据包装和内容 digest，验证 total_lines/实际范围/ends_mid_line/完整性、CRLF/LF/裸 CR/BOM/末尾换行、空文件、UTF-8/CRLF/长行截断、恶意分隔符与转义后 64 KiB/512 KiB 边界；固定原始字节及包装 golden，测试前缀选择有限复杂度与副本隔离。
- [x] 1.4 增加普通/显式文件交互草稿与有界附件集合，更新 UserInput 的验证、ByteSize 和深拷贝；单测验证原文、Skill 和多文件独立保存及所有元数据计费，无 I/O 或包级可变状态。

## 2. 工作区安全读取与宿主来源

- [x] 2.1 将 Workspace 目录名称读取改为有限批次机制，并分别适配新文件扫描与现有 walker；用注入式枚举测试验证每批至多 256、新扫描单目录/栈上限及溢出整目录省略，现有 Glob/Grep golden 和原遍历契约保持。
- [x] 2.2 从现有 Read 提取语义一致的小型 FD 打开/有限读取机制供文件准备复用，保留 no-follow 与实际类型/权限检查；确定性测试覆盖文件/目录 symlink、路径替换、特殊文件、读前后 fstat 失败/属性变化/大小不一致和 unsupported platform，验证不降级交付正文或伪造 Read invocation。
- [x] 2.3 实现一次准备内同路径分组单次读取、原始 bytes 多范围选择、16 MiB 单源/64 MiB 总源预算和取消计费；单测验证全量引用验证早于任一内容 I/O、源缓冲只保留一份、片段独立、部分失败整条拒绝、EOF/UTF-8/NUL 和包装限额，读取量不被失败或去重重置。
- [x] 2.4 实现候选 session 内的渐进文件名索引和分批匹配，限定 50000 项/16 MiB 索引、16000000 匹配状态转换、50 项/128 KiB 结果；用可控发现/查询 fixture 验证同 sigil 锚点编辑不重启遍历、不重置 2 秒发现 deadline、index revision 单调及部分结果不冒充完整覆盖。
- [x] 2.5 为实际文件来源实现显式 Start/Close、单搜索 worker、最新待处理槽和单内容准备 gate；用屏障测试验证 Start 失败回滚、重复启动/关闭、busy、关门与借用竞争、取消、Done、强制 Close 未退出时保留所有权及锁外 I/O，无按键级长期等待 goroutine。
- [x] 2.6 实现有界 Gitignore 样式纯规则匹配，覆盖锚定/目录规则/否定/注释/转义/尾空格、*、?、字符类、各位置 ** 和嵌套优先级；用固定矩阵及 32000000 状态预算/取消测试验证实际路径纳入与剪枝、字符/模式内层操作计费，无无限回溯或对 Tool.Glob schema 的误用。
- [x] 2.7 接入工作区内 .gitignore/.ignore/.rgignore 的安全读取及过滤，验证 64 KiB 单源、512 KiB/4096 规则、4096-byte 模式和失败子树省略；集成测试证明不读父级/全局/Git index、不受 tracked 状态影响、隐藏文件与手输被过滤路径保持规定语义。

## 3. 输入准备、控制协议与 Runtime

- [x] 3.1 更新现有交互 Append/进程内输入命令的 typed 草稿和附件边界，并将文件准备与 Skill 准备按实际消费者组合；协议与 app 单测验证无关 command payload 拒绝、安装后冻结、纯 admission、无任何新增空 facade 或动态 any。
- [x] 3.2 将文件附件贯穿 admission、queue/claim、用户输入预检与 Sync/finalize，强制提交事件/摘要同样在 writer admission 前构造并验证；回归验证 4 MiB、64 条/16 MiB、native/record 16 MiB、batch 64 MiB 及转义扩张，可预判的 native 或展示错误均不 poison、Sync 不确定正确 poison。
- [x] 3.3 保持 queued 后冻结、FIFO/strict-next、原子取回和停止返还，初始/工具安全点/Stop steering 共用冻结值；用注入文件变动和可控 claim 顺序测试保证取回只返回原文、再次提交才重读。
- [x] 3.4 补齐 pending 准备取消与 active turn 停止的联动，接收前取消零写入、Sync 后取消仍 committed；用取消线性化屏障验证无双 admission、误返还或重复模型请求。
- [x] 3.5 将 pre-admission 文件借用纳入 app cleanup 顺序；用阻塞读取/搜索、shutdown deadline 和最终 Done 测试证明来源退出早于 Workspace/writer/lease 释放，重复 Close 结果稳定且无 helper Wait goroutine。
- [x] 3.6 回归权限与副作用边界：文件附件不产生 ReadObservation、Write grant、Skill 资源激活、Hook 信任或工具 ledger；用现有 Write、Skill、Hook 场景验证附件输入后原有安全检查继续生效。
- [x] 3.7 复用 thread owner 的 operationProfile/TurnInput 捕获，不在文件草稿或准备中保存策略；用文件准备、入队、owner 接收与 finishing 完成屏障验证 latest strict-next、旧策略同轮工具/Stop/自动摘要、独立手动摘要接纳时捕获，以及预检/body/properties/receipt 同源，不能提前启动或清理。

## 4. 双 Provider 当前原生结构

- [x] 4.1 将 InputMetadata 更新为必需 files 数组与明确的 input_id/attachments 结构，完善选择 union、total_lines/ends_mid_line 等必需字段的 strict decoder 与深拷贝；验证完整有序引用集合、[]、缺失/null、重复、空集合、真 digest 下的非法范围/完整性、上限及字面 @ 无附件。
- [x] 4.2 更新 Anthropic user_inputs、clone、codec 和 strict restore，按原文/Skill/文件构造并逐字验证 text blocks；增加固定多文件及 Skill+文件 native/request golden，验证准备不提交、Sync 后单 finalize。
- [x] 4.3 更新 OpenAI 对应 input_text parts、clone、codec 和 strict restore；增加同类固定 golden，验证 function/custom 配对及 reasoning/encrypted content/phase 不变。
- [x] 4.4 为两家补充直接续写、恢复、离线预览后恢复的等价测试；固定包含 opaque reasoning、工具、steering、Stop 和 checkpoint 的事实，恢复后显式重选与直接路径相同设置，允许选择来源/序号不同，比较完整请求 bytes、原生次序、footprint/revision 和 fingerprints；不重选时验证启动值恢复及历史 bytes 不变，来源删除后读取为零且不增加策略 record。

## 5. 历史投影、上下文与缓存

- [x] 5.1 扩展 SemanticMessage/TranscriptItem 的独立文件集合及所有 clone/getter，两家投影只从 native facts 派生；测试 recall 原文、离线预览、并发提交和副本修改不影响后续原生请求。
- [x] 5.2 在已有 committed_history 增加实际消费的 file_attachment 内容 kind，完善候选/有效窗口计量和 canonical 内容；用固定 cache golden 验证内容只计一次、身份/digest/绝对根不进入 fingerprint、无附件请求 bytes 保持。
- [x] 5.3 验证手动/自动压缩与 Stop 候选 discard/rebuild 保留已准备文件而不重读；使用故障注入证明新窗口只计摘要和后续增量、旧文件仍可查且不自动 rehydrate。
- [x] 5.4 补充 Anthropic disabled/5m/1h、OpenAI omit/thread key 及 WebSocket full/incremental 的文件回归；分别比较完整逻辑请求和各自 wire golden，验证查询/候选不改变任何稳定前缀。
- [x] 5.5 为 UserInputCommitted 增加独立 total/items/omitted 文件摘要，按编码后 4 KiB 保留完整项前缀，由已提交 transcript owner 提供正文分页；测试首项或多项超限、路径转义、原文/列表不带完整文件、Ctrl+O 全部来源/部分行语义、四页缓存及零来源重读。
- [x] 5.6 实现摘要的 typed 构造器/严格 decoder，与同一冻结输入纯派生并预检最终提交事件；协议/Runtime 故障注入验证计数、顺序及 byte size 一致，确定展示编码错误在 Sync 前拒绝，live/恢复投影相同且不会把摘要当成完整附件。
- [x] 5.7 固定同一文件 native 窗口/候选输入隔离 effort 与附件的身份变化：验证策略变化仅按现有契约失效 provider_profile/properties、同 wire 不同来源/序号等价；增加 WS 无中间操作选回旧值与实际失败/取消/摘要后选回旧值的不同 baseline 回归，文件 digest 不能恢复失效连接。

## 6. TUI 文件补全与阅读

- [x] 6.1 在已有 composer 中接入路径区目标和往返一致的 serializer，保持 /、$ 首词行为；单测/fuzz 覆盖前导 @、空格/#/双引号/反引号、路径与 #L 分区、接受后越过完整引用及分隔字素、多行/Unicode/长路径/不受支持名称、超限原子拒绝及非目标文字保持。
- [x] 6.2 接入 typed 宿主文件查询、单个结果等待路径和 progressive index revision，核对 workspace/session/composer/token/request 代次；用乱序/ABA/取消 fixture 验证旧结果不能复活，最新结果按路径身份保选择且不会每按键重新遍历。
- [x] 6.3 复用选择/滚动/抑制与焦点机制，文件 Tab/Enter 只填入、第二次独立提交才准备内容；回归 paste、Esc 两步、Ctrl+C、换行、modal 与流式事件，验证第一次接受零附件正文读取/零 Session/零 Provider 请求，光标不导致再次接受。
- [x] 6.4 增加正常、28x10、20x5、1x1 和 NO_COLOR 的固定 snapshot，验证最多五行、loading/error/incomplete 提示、控制字符转义、完整路径插入与 resize 光标保持；覆盖 effort 通知/帮助、多行 composer 和 64 条排队输入共存，有限视区保留输入及 FIFO 队首、隐藏候选不可接受且不删除队列事实。
- [x] 6.5 在主线和 Ctrl+O 显示文件摘要、范围/截断及分页详情；用 live/恢复同事实回放测试验证摘要一致、未 Sync 的附件不可冒充已保存、详情不改变 composer/recall。
- [x] 6.6 为 pending 新查询立即停用已显示旧行，明确 loading/empty/error/partial 的接受规则与无可接受项的普通 Enter；测试 A 列表显示期间输入 B 后立刻 Tab/Enter、旧索引回包、关闭重开新文件/规则，断言不写错路径且旧 session 最终清理。
- [x] 6.7 复用 domain 保留名纯 switch 和既有 chat_space/composer 编辑失效路径，保持七项内置及完整 256 Skill 容量；验证 /effort、/compact 等精确内置命令参数不开文件搜索，而 $effort Skill 可带文件；文件接受/取回/召回后迟到 effort 回执不得清新草稿、抢焦点或重开候选，选择权威水位与文件查询代次独立。

## 7. 集成、当前 fixture 与真实进程验收

- [x] 7.1 增加双 Provider app 集成：手输/补全文件、Skill+多文件、工具后追加、取回重提和准备失败后继续；验证实际 mock request 内容/顺序、调用次数、冻结文件变更及工具权限保持。
- [x] 7.2 逐字节替换受影响的 current JSONL/native fixtures，并增加缺 files、部分引用集合、错块/关联/digest、真 digest 下非法 total_lines/范围/部分行/完整性及带半行损坏的拒绝 fixtures；测试预期来自固定 bytes，语义拒绝发生在 repair 前且原 journal bytes 不变。
- [x] 7.3 增加真实子进程 input-only 崩溃和重复恢复、lease 竞争/崩溃释放/占用期间禁止 repair、续写 seq 单调测试；删除全部来源后仍离线恢复，且没有自动 Provider 请求或文件再读。
- [x] 7.4 回归一次性 headless 和 stream-json：含 @ 的普通输入无文件 I/O、原文保持，已保存交互附件可被正常 resume；断言外部 JSONL 字段没有新增内部附件摘要。
- [x] 7.5 增加 Darwin 真实 PTY smoke，覆盖两家 Provider、引号/行范围/接受后光标、旧 loading 行不可接受、忽略文件与手输、中途补全/两次 Enter、paste/modal/取回/停止、resize/NO_COLOR/Ctrl+O 及终端恢复；执行 `python3 scripts/file-mentions-smoke.py` 验证全部断言，使用事件/窗口握手而不以 sleep 证明正确。
- [x] 7.6 增加大目录/大规则集/长行与快速查询的有界性集成与 benchmark，断言发现次数、原始枚举/规则/索引/状态转换/包装/结果额度及关闭完成；记录性能观测，正确性通过计数和确定屏障判定，不以机器时长或随机竞态替代断言。
- [x] 7.7 增加双 Provider 文件+effort 组合 app 集成并扩展文件 PTY：准备中/queued 后选择、strict-next 与自动追加、工具/Stop/压缩/finishing 及恢复后重选；断言冻结内容不重读、实际请求及收据策略正确、选择不写 Session、原工具/Hook 执行次数不增加，同时重跑 `scripts/effort-smoke.py` 与 `scripts/completion-smoke.py`。

## 8. 文档、契约复核与最终质量门

- [x] 8.1 同步 README、capability-status、overall-architecture、product-roadmap，以及实现中实际确认的 pitfall；核对语法、实际额度、唯一 record vocabulary、平台边界与所有文件引用，未验收能力不写成已完成。
- [x] 8.2 为新边界补充有业务断言的架构测试并执行针对性 suite，验证 domain/protocol 无 I/O、依赖单向、无包级可变 registry、无 any/版本 reader/未消费 schema，既有九工具与 Hook/Plugin 回归保持。
- [x] 8.3 执行完整 `make verify`、本 change 与全部 OpenSpec 严格校验，将命令/平台/结果及未运行项目写入本 change 的 verification.md；仅通过基线或单测不能勾选本项。
- [x] 8.4 验收通过后同步本 change 的 delta 到主规格并再次严格校验，复核 proposal/design/tasks/验证证据/实际代码一致；保持 change 待归档，不在 apply 中擅自归档、提交或发布 PR。
