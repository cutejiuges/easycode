## 1. 当前协议、查询与 prepared 值

- [x] 1.1 扩充 domain Transcript 的 reasoning kind、source/availability 与不可变 parts 正文，提供 typed 构造、strict decoder/validator 和虚拟范围读取；`go test ./internal/domain` 覆盖重复/null/未知/跨 kind、零字节段、空白和双 LF 的精确 UTF-8 偏移。
- [x] 1.2 实现内部推理 Started/Delta/Completed 当前事件与 sample/block 状态校验；`go test ./internal/protocol` 证明非法引用、偏移、重复关闭及正常 sample 未关闭块拒绝，失败解析允许尚未 Completed 的当前块收口。
- [x] 1.3 增加 PreparedSample 的纯内存 typed block/native ordinal 绑定表并更新全部当前构造者；`go test ./internal/provider` 证明空表、唯一/边界/来源校验、深拷贝、Finalize/Discard 单次消费，不出现旧构造兼容分支。
- [x] 1.4 将 TranscriptCommitted 改为明确 input/sample 来源，并加入 sample index/绑定；给内部 failure 增加 typed not_committed/durable/uncertain 当前 sample resolution，Session 失败 record 不变；协议测试覆盖字段组合、编码预算、无 sample 时禁止伪造以及旧内部 shape 拒绝。
- [x] 1.5 扩充 TranscriptListQuery 的必需 main/detail 模式、缓存键与全部当前调用者；`go test ./internal/protocol` 证明缺模式/null/非法模式拒绝，两种模式按各自筛选后分页，不保留旧查询 reader。
- [x] 1.6 将 reducer 推理结果改成最多两项的 typed 逻辑 draft，并由两家发布器惰性分片构造实际事件，更新 StreamEvent 白名单；Provider 测试证明 item-only 的 Started/Completed/native 顺序、大 delta 不物化分片数组、逐片取消、terminal 唯一及 turn/tool 生命周期伪装拒绝。

## 2. Provider 白名单与交付状态机

- [x] 2.1 分别实现两家共用的白名单提取/快照核对纯函数，供 reducer 与 TranscriptProjector 使用；golden 保留原空白、part 顺序/分隔，并逐种检查 signature/redacted/encrypted/raw/未知字段哨兵零泄漏。
- [x] 2.2 接入 Anthropic start/delta/stop 和 redacted 无正文生命周期；`go test ./internal/provider/anthropic` 证明 start 初值忽略、重复保护、signature 保真、空 thinking、错误 block 和 EOF/取消仍遵守原生累计契约。
- [x] 2.3 实现 OpenAI item/part 独立关联与有序前缀发布；`go test ./internal/provider/openai` 覆盖后段先到、前段空/未完成、非连续 index、初始 summary、文本/工具交错，最终原生完成顺序不被 UI 排序改变。
- [x] 2.4 按 design 第 4 节逐行实现段 done/part.done、item-only、缺省段补齐、相同重复幂等、冲突和错误引用；每行固定 fixture 验证一次正文、已知关闭项 late done 的 Provider 边界丢弃、关闭后新 delta 拒绝。
- [x] 2.5 固定默认交付范围，确保请求不新增 sequential_cutoff/summary/thinking/effort/budget，澄清 ReasoningSummary 为主动请求能力且仍 false；request golden/能力测试证明被动展示不改变任何请求策略或假称未协商模式支持。

## 3. 全链路资源界限

- [x] 3.1 在推理累计追加/替换/克隆前实施 source 16 MiB、scratch 16 MiB、item/part 合计 4096、index 小于 4096 和元数据 1 MiB 检查；多小 frame 与边界值测试证明不等最终 commit 编码才检查，不重复计费 done，不保留无限关闭身份。
- [x] 3.2 将合法大 delta 拆为最多 16 KiB UTF-8 的事件，完成快照最多 64 KiB，serialized 推理/映射/失败解析 payload 最多 512 KiB；escaping 与大输入测试证明预检早于复制/发布，绑定预算失败早于提交 admission。
- [x] 3.3 保持 Provider 与 AgentLoop 各最多 16 项的输出容量，核对新增排队 payload 合计 16 MiB、处理中临时副本 4 MiB 及所有转发的预算/所有权；用阻塞消费者与取消门闩证明背压有界、owner 最终释放、没有无界聚合或 per-delta goroutine。

## 4. Runtime、取消结果与性能

- [x] 4.1 在现有采样循环绑定真实 sample scope，核对 prepared 绑定并在成功提交后发布精确 sample commit，输入 commit 单独发布；Runtime 测试覆盖无工具最终 sample、多 sample、同轮追加、无 TUI 查询消费者以及错配绑定零 journal 写入。
- [x] 4.2 接入实际提交结果的 failure resolution，明确已选中 completed 进入提交路径后沿 WithoutCancel 等待结果；确定性门闩覆盖提交前取消、提交中取消成功、Sync uncertain、Sync 后 finalize/投影失败，断言保存状态与真实 journal/恢复事实相符。
- [x] 4.3 在失败/取消一次收口所有当前活动块与校验状态，允许缺 Completed，不重标先前 committed sample；Runtime/headless 测试覆盖多个活动块、工具阶段取消和 late delta，以及取消后只继续验证/转发生命周期边界、抑制 Delta，保证 completed 胜出时宿主不残留活动块；唯一 terminal/cleanup，不重跑工具。
- [x] 4.4 统一 publication 生产和 AgentLoop 消费的高频事件分类，推理 Delta/Started/Completed 复用 committed context/cache；fake planner/projector/footprint 对 1 与 10000 次 delta 的调用计数相同，只有真实提交刷新 revision。
- [x] 4.5 用确定性调度在输出期间插入 interrupt、审批、输入及 shutdown，证明控制在最终输出前被处理；手动/自动压缩辅助推理不进入普通活动或 transcript，不新增 owner/timer/每 delta tea.Cmd。

## 5. 宿主投影、查询和 TUI

- [x] 5.1 扩充两家唯一 TranscriptProjector 和 app 连接/正文范围读取，推理位置与 call 配对独立；`go test ./internal/provider/... ./internal/app -run 'Transcript|Reasoning'` 证明虚拟正文页不拼接整篇、unavailable 禁正文、原语义历史隐私断言继续通过。
- [x] 5.2 实现 main 排除推理后取 50 项、detail 包含全部项的分页与共享缓存；大历史测试证明隐藏推理不挤掉主线普通项、detail 可翻到所有真实 cursor，模式缓存不串用。
- [x] 5.3 拆出小型活动推理组件，默认主线只保留当前提示/等待保存，成功后撤下不累积永久行；固定 golden 覆盖无推理、opaque-only、工具/三类 modal/取消状态优先和历史发现入口。
- [x] 5.4 将临时 typed 键接入单一 Ctrl+O 浏览器，commit 立即按绑定升级 cursor 并保留源偏移；延迟查询、相同正文、连续 commit、目标离开最新 50 项的测试证明恰好替换一次、不按文字去重且旧回复不重开页面。
- [x] 5.5 复用 Markdown/安全原文、UTF-8 页、净化、源锚点和 NO_COLOR；固定正常/窄屏 golden 覆盖中文/emoji、OSC/bidi、长围栏、r、原文片段和 unavailable，View/resize 不执行 I/O 或请求。
- [x] 5.6 实施全 sample 64 KiB/最近 50 个临时条目的显示预算、选中项优先与明确尾片段，活动详情不复制另一份正文、所有页共用四页；多块/超长/完成快照测试覆盖实际分配及淘汰，不影响模型成功性。
- [x] 5.7 接入未保存/已保存刷新失败/保存不确定状态，至多保留一个失败 sample 快照并在下一 turn/退出释放提示；按键、paste、草稿/光标/回忆/队列与问卷/Hook/审批互斥回归证明无焦点泄漏或旧 sample 复活。

## 6. headless、原生恢复与缓存

- [x] 6.1 在三种 headless 共用 projector 中严格消费推理、提交来源/绑定和 failure resolution；`go test ./internal/headless` 证明合法 open-block 失败收口一次、未知/非法/终态后事件仍报错，外部词汇/字段不变。
- [x] 6.2 增加双 Provider × print/json/stream-json 的本地集成矩阵，含工具、推理、普通回答、取消、EOF drain 与断管；stdout/stderr 哨兵断言零推理/opaque/内部映射泄漏，owner/退出码符合既有契约。
- [x] 6.3 比较两家 uninterrupted/restored 的完整 canonical bytes、native item 顺序、signature/encrypted 和 fingerprint；展示/协议 golden 使用固定 bytes，现有 Session/native fixture 不变，不在运行时由 encoder 生成恢复 fixture。
- [x] 6.4 验证未浏览/活动阅读/原文/resize/提交交接/恢复的请求和缓存等价；Anthropic marker/TTL 与 OpenAI full/incremental 分别核验，稳定来源、工具目录和请求次数无展示引入的变化。

## 7. 交互验收、文档与质量门

- [x] 7.1 增加本地 app fixture 与 `scripts/reasoning-smoke.py`，Darwin 双 Provider 实际 PTY 验证活动阅读、完成后主线撤下、精确提交交接、取消/uncertain 提示、分页、modal 抢焦点及终端恢复；不访问公网模型。
- [x] 7.2 同步 README、能力清单、P5 Roadmap、总体架构与实际词汇/路径，明确被动展示、默认交付范围和平台限制；链接检查通过，只有实际发现问题写入 pitfall。
- [x] 7.3 对照 design 第 11 节九项缺口矩阵及七份 delta 逐场景审核，核实所有测试路径和事实词汇；执行本 change strict 校验、主规格校验、git diff --check 和最终 make verify，完整记录命令/退出码，不弱化门禁。
- [x] 7.4 在 `verification.md` 记录实际证据及基线 Hook race 失败的核实结果；不能以单项复跑覆盖完整门失败，若需改变 Hook 契约先确认其独立范围；验收后同步主规格，独立归档流程待后续授权。

归档说明：用户于 2026-11-08 后续明确请求归档，本 change 已按独立归档流程完成；原任务 7.4 的授权前置条件已满足，检查结果见 verification.md。
