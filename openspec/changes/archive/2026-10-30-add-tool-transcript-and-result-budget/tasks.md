## 1. 强类型契约与纯逻辑

- [x] 1.1 在 domain 定义封闭 transcript 文本/调用锚点及稳定位置值，保持与 SemanticHistoryView 分离；通过构造、复制隔离、无 opaque 数据和依赖边界单测。
- [x] 1.2 在 protocol 定义七工具统一活动与有界 transcript/detail 查询值，Exec progress 仅作为合法变体，保留独立 approval；通过每个 kind 的 constructor/strict decoder/validator 正反例及无下层依赖检查。
- [x] 1.3 在 tool 定义实例持有的 ResultBudget 与必需预算元数据，实现 256/64 KiB 单调用、512 KiB sample、2 MiB turn 和 1 KiB 后续调用预留；通过顺序分配、边界算术、64 call、安全错误及完整 preview bytes 计费测试。
- [x] 1.4 为只读执行定义有界 typed material 与按工具 finalization 边界，定义 stdout/stderr/search_result 来源和独立命令/文本 capture 小接口；通过非法能力/来源、无 any、无 Session 依赖及生命周期契约测试。

## 2. 双 Provider 的有序展示投影

- [x] 2.1 在 OpenAI provider 实现 native entry/item 有序 transcript 投影，复用私有文本提取逻辑；通过 text/call/text、custom/function、多输入、phase/reasoning 排除和返回副本隔离 golden。
- [x] 2.2 在 Anthropic provider 实现 block 有序投影；通过 text/tool_use/text、thinking/signature/redacted 排除及同轮追加 golden。
- [x] 2.3 接入真实宿主投影消费者需要的 Provider 小接口，保证 capabilities 只声明完成能力；通过投影前后 canonical request/native item 顺序/fingerprint 等价和现有 SemanticHistoryView 回归。

## 3. 私有保存机制复用

- [x] 3.1 在 session/artifact 提取包内共享对象发布/配额骨架，保持命令双流和新增单文本 capture 各自 owner；通过旧命令存储回归与 search_result 增量写入、UTF-8 边界、complete/partial/unavailable/not_needed 测试。
- [x] 3.2 替换当前 reservation 严格格式，固定 Exec 32 MiB、搜索 16 MiB、thread 256 MiB、root 1 GiB；通过真实子进程混合配额竞争、崩溃对账、拒绝旧 shape/非法额度且既有 bytes 不变测试。
- [x] 3.3 扩展 descriptor 授权与读取支持 search_result，沿用安全 FD、digest、no-replace 和 Sync 发布；通过跨 thread/能力/来源拒绝、symlink/TOCTOU 故障注入、同长度损坏检测测试。
- [x] 3.4 将文本 capture 与 app 生命周期接线，补齐取消/Sync 失败/阻塞 shutdown 的唯一 cleanup；通过 done、reservation、store 与 thread lease 在完成前不释放的确定性故障测试。

## 4. 七工具预算处理

- [x] 4.1 改造 Read 有界执行材料及 finalization，保持既有范围/长行规则，超实际额度返回可操作预算错误并失效观察；通过完整读取、空文件、长行、Read 预算拒绝后 Write 不得覆盖的回归。
- [x] 4.2 改造 ReadArtifact 在有界 raw window 上按实际额度渲染，页头/转义计费并计算真实游标；通过搜索对象、非法 UTF-8 偏移、转义膨胀、连续分页无重复/跳字节/零进度及不生成观察/递归 artifact 测试。
- [x] 4.3 改造 Glob/Grep 将既有扫描上限内选中结果增量写入文本 capture，保留确定性顺序与取消语义；通过长行裁剪前保存、limit 覆盖与保存完整性分离、无无界累积测试。
- [x] 4.4 实现搜索按实际 allowance 的稳定首尾预览与有界 metadata，只保留模型所见 matches、统计和保存引用；通过存储不可用、极小额度、实际 JSONL 大小及完整提示计费测试。
- [x] 4.5 将 Exec 实际 allowance 在审批前加入候选 fingerprint，按最终额度决定 needed 与双流预览；通过小于 256 KiB 但需保存的命令、拒绝旧批准、非零 exit 与保存失败相互独立测试。
- [x] 4.6 为 Patch/Write 及全部错误/拒绝/冲突/取消/不确定结果提供有界 finalization，安全 fallback 不超过 1 KiB；通过长路径、保持真实副作用终态、完整审批候选不变测试。

## 5. Runtime 与 Session 提交

- [x] 5.1 在 Runtime turn owner 接入账本与 ordered finalizer，只读 worker 不直接发布终态，搜索预留在 started durable 后按 call index 执行；通过八 worker 乱序完成预算一致、最后配额稳定分配和 exclusive 屏障回归。
- [x] 5.2 实现正文耗尽后的 admission 停止、accepted 调用完成与未 accepted 配对，outputs 后 durable 失败且停止 steer/sample；通过耗尽前后准入、取消竞态、sample 局部不足重试和同轮追加不重置测试。
- [x] 5.3 在 result Sync 前一次冻结预算/保存/preview，Sync 后授权引用，native outputs finalize 后激活完整 Read 观察；通过结果/outputs/观察各边界故障注入与无重复副作用测试。
- [x] 5.4 替换 Session 当前 canary 5 为 6 与当前 result 严格 payload，保持 native 2、headless v1 和 record vocabulary；更新唯一 testdata/current 固定 bytes，通过缺字段/错误预算/错误来源/旧 shape fail-closed 及 checksum 回归。
- [x] 5.5 扩展 replay 的冻结结果与安全补偿，禁止 artifact I/O 和重新预算；通过异构调用组、部分 results、results 后 outputs 前崩溃、保存对象删除后的 canonical request/native 顺序/fingerprint 等价测试。
- [x] 5.6 将 Runtime 事实通知统一为七工具活动，Exec 保留有界瞬态进度并迁移旧消费者；通过终态仅在 durable 后出现、partial args 不出现活动、高频 progress 不入 journal 及阻塞 stream/cleanup 测试。

## 6. 宿主记录与分页查询

- [x] 6.1 将 session/command_history.go 演进为七工具已验证事实投影，app 用 NativeCommitRecord.Sequence 连接 entry ordinal 与 ready identity；通过 text/call/text、多 turn、steering、重复/缺失锚点失败关闭测试。
- [x] 6.2 将 app/command_queries.go 演进为唯一 transcript/detail owner，支持 50 条逻辑页、64 KiB 正文页和原 artifact 范围页；通过跨页长文本、取消完成信号、错误隔离和无全历史渲染副本测试。
- [x] 6.3 实现 provisional sample 文本到 committed 投影的恰好一次替换，失败文本标注未完成；通过 live/restored 顺序一致、无重复正文和未提交内容不伪装持久化测试。
- [x] 6.4 删除迁移后的命令专用 observer/facade/查询状态，保持 headless 分流；通过旧消费者引用扫描、架构边界、--print 最终文本和外部 JSONL v1/stream-control golden。

## 7. Claude 风格主线与详细记录

- [x] 7.1 拆分纯 transcript state、grouping 与 tool presenter，展示七工具、Shell 名称和明确异常，移除持续 Status 行；通过双 Provider 相同展示、正常/拒绝/冲突/取消/不确定/非零退出固定尺寸 snapshots。
- [x] 7.2 实现相邻 Read/Search 去重计数与分组边界，ReadArtifact 独立，Shell 默认最多三行结果；通过重复文件、组内失败、assistant 文本/输入/审批/修改打断及展开全部调用 snapshots。
- [x] 7.3 将 Ctrl+O 替换为完整 transcript，Enter 打开详情、Esc 逐层返回，支持逻辑页与正文偏移定位；通过恢复旧活动、草稿/光标/滚动位置不变、PgUp/PgDn 与 resize 测试。
- [x] 7.4 接入四页缓存、generation 取消、双流 Tab 与搜索保存详情，区分展示/模型/采集/保存省略；通过迟到回复、missing/corrupt、partial stored EOF、只保存摘要的历史修改及无额外模型/ledger 副作用测试。
- [x] 7.5 完成审批优先焦点、浏览输入隔离、Ctrl+C 定向中断与 Esc 返回规则，保留窄屏/无色/控制字符安全；通过按键/paste/resize snapshots、View 零 I/O 和退出后查询 owner 最终清理测试。

## 8. 合并验收与交付

- [x] 8.1 增加双 Provider 的七工具混合任务纵向 fixtures，覆盖实时与恢复、审批拒绝、非零 exit、搜索回读和 Write 证据；比较 uninterrupted/restored canonical bytes、item 顺序、fingerprint，浏览前后 journal/request 不变。
- [x] 8.2 增加脚本化 Provider 固定工作负载：定点 Read、八工具并行搜索、大命令日志、64 call/预算耗尽与同轮追加；记录 preview/request/saved bytes、分页次数与追加读取次数，以有界材料/页缓存断言验收默认策略，失败时先更新 artifacts 不静默放宽。
- [x] 8.3 在支持的 macOS/Linux 实际运行存储/lease/进程与故障矩阵，覆盖真实子进程竞争、崩溃释放、占用期间禁止 repair 和 seq 单调；缺少 runner 时明确未验收项，不以交叉构建替代运行证据。
- [x] 8.4 同步总体架构、tool-system、Roadmap、pitfall 及必要 ADR，记录参考来源与复用边界、当前 canary 和真实 record vocabulary；逐项核对文档路径、实现状态及本 change 全部规格，不把未完成项写为已交付。
- [x] 8.5 执行 strict OpenSpec 校验、AGENTS/依赖/无死代码审查及 make verify，确认 gofmt、vet、Staticcheck、全量测试、架构测试与 race 全部通过；交付证据必须覆盖 UI 与预算两部分，同一 change 不部分完成或部分归档。
