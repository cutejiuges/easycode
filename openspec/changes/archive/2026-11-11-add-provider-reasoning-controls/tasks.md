## 1. 冻结设置与配置校验

- [x] 1.1 apply 启动时核对九份 delta、design 与当前 checkout，执行 strict 校验并记录默认 request/profile golden 及代码质量门基线；确认计量与 native 当前形状的变更范围明确后再实现。
- [x] 1.2 实现不可变 InferenceSettings、thinking/display 判别联合及 typed 构造/validator；只在真实 JSON 边界实现 strict decoder，单测覆盖零值、分支、getter 隔离、纯内存构造和无 any/空策略。
- [x] 1.3 在现有五个顶层配置字段中接入 display；表驱动测试覆盖 adaptive/enabled 的 summarized/omitted/省略、disabled 禁止 display、updates/null/重复/大小写/跨分支拒绝及全部 effort 枚举。
- [x] 1.4 验证 manual 普通 budget、普通输出范围、环境覆盖后的 family 和窗口预留；测试 ordinary 8192/16384 与 summary 4096 的普通启动成功、原小窗口行为，以及输出 0/1/65536/65537 与溢出拒绝。
- [x] 1.5 扩展无副作用启动测试，断言非法 display/family/预算零网络、零 Session 创建、无后台启动且不泄漏 secret；合法启动后改配置文件不影响冻结选择或实际发送值。

## 2. 原生编译与 transport

- [x] 2.1 将同一冻结值接入两家 Provider Config，替换 Anthropic 独立输出真源并更新全部调用点；构造器/架构测试验证没有双配置优先级、旧 API facade、隐藏 I/O 或 goroutine。
- [x] 2.2 编译 Anthropic thinking/display/output_config 和普通上限；新增固定请求 golden 覆盖所有分支及独立 effort，旧默认请求 bytes 保持，空正文仍按真实活动处理。
- [x] 2.3 在 Anthropic disabled/5m/1h 缓存派生保留完整 thinking/display；测试 marker/TTL、原生 opaque 与工具顺序，缓存覆盖不写入原生 content 或 usage。
- [x] 2.4 编译 OpenAI effort-only/summary-only/both/省略及输出上限；固定 golden 验证独立省略、无空 reasoning、include、store=false 和原生输入/工具不变。
- [x] 2.5 在 OpenAI WS full/incremental 与允许的 HTTP fallback 保留全部逻辑设置；本地 fixture 断言 transport 专属差异、配对和完整逻辑 fingerprint，一旦属性不匹配不得沿旧 response ID 增量。
- [x] 2.6 更新已有 ReasoningSummary 声明并在真实 app 装配校验；测试显式 summary 与本地已启用能力一致、默认/Anthropic summary false、RawReasoning false，阅读或空正文不改变声明。

## 3. 同源请求计划、证据与缓存

- [x] 3.1 在各 Provider 内用现有 wire 类型组合不可变用途计划，供预检及执行分别纯内存构造；测试相同来源/设置/用途计划等价、来源变化在网络前拒绝，不能读取配置或反向用共享历史构造请求。
- [x] 3.2 从同一计划导出真实 body、properties、输入 fingerprint 和 receipt；直接断言摘要 tool choice none、独立上限、保留重点及推理字段一致，删除摘要开始前/terminal 后重编译普通 evidence 的路径。
- [x] 3.3 扩展请求属性及 WS baseline 回归；验证 marker 不入逻辑输入 hash、摘要指导入该 hash、CompactionSource digest 单独校验，summary 准备/失败不安装普通 baseline，成功后按 compacted boundary full 重建。
- [x] 3.4 扩展 profile/cache regression，验证 display/effort 改变定位 provider_profile、默认 bytes/revision 及显式 Anthropic 4096 等价、其他 segments 不变、usage/配置路径/显示来源不污染稳定指纹。

## 4. 摘要策略与独立预算

- [x] 4.1 为两家 summary 计划沿用冻结 thinking/display/effort/summary，只替换用途、tool choice、输出上限及摘要指导；固定 golden 验证手动/自动两入口和缓存路径均不删参数、不改变普通上限。
- [x] 4.2 在 Provider preflight 增加 CompactionBudgetExceeded typed 分类并映射到既有 Runtime 预算拒绝；测试 manual 8192 对 summary 4096/8192 拒绝、8193 在其他约束满足时允许，零 started/网络且不夹取，不将普通启动判为无效。
- [x] 4.3 复用自动压缩预算失败抑制路径，测试同来源不重复尝试、无 started/failed 记录、不误报协议错误；旧窗口可请求则同任务继续，否则按既有失败/返还输入收口。
- [x] 4.4 覆盖 thinking+工具调用+durable outputs 后、最终回答前触发摘要；测试完整 native 来源、冻结策略和工具执行一次，不能把本地工具安全点当作 Provider 模式切换许可。
- [x] 4.5 验证独立窗口/输出预检、普通未知估算与自动阈值公式不回归；摘要成功、refusal/incomplete/空白、取消及 Sync uncertain 分别维持现有切窗/poisoned/cleanup 语义，普通策略和失败前 baseline 不变。

## 5. Anthropic 推理计量与当前格式

- [x] 5.1 从 message_delta 归并 output_tokens_details.thinking_tokens；单测覆盖正值、明确零、缺失/不覆盖已观测值、负数/非整数/溢出/损坏，以及最终 thinking 大于 output 拒绝，原 input/cache 占位零规则保持。
- [x] 5.2 扩展 Anthropic rawUsage 和 rawUsageCommit 的必需 thinking_tokens 状态；codec 单测验证 known/value 组合、零值存在、unknown 无 value、字段缺失/null/重复/未知失败，只有唯一当前 shape。
- [x] 5.3 枚举所有 sample、checkpoint generation、failed provenance 及 Session current fixture 中的内嵌 Anthropic usage，替换固定 bytes/normalized 状态/相关校验值；测试不得运行时 encoder 生成 fixture，旧四字段 shape 在 repair/追加前失败且 journal 不变。
- [x] 5.4 验证恢复 raw/normalized 交叉核对：raw known:30 与 normalized 0/unknown/NA 均拒绝；正常正值/零/缺失 fixture 恢复等价，output 未知而 thinking 已知时保留独立状态，不制造总量。
- [x] 5.5 扩展普通多 sample turn 与独立摘要聚合测试，验证 known 相加、unknown 传播、output 不与 thinking 重复相加，摘要成功/候选拒绝分别归属 CompactionID，不混入普通 turn。
- [x] 5.6 在已有 prepared/Sync/finalize 和失败故障注入中加入新计量断言；验证 raw/native/normalized 一起 durable、未知终态为 unavailable、后续失败不抹除先前 sample usage，查询计量不改变请求或 fingerprint。

## 6. 完整循环、失败与恢复

- [x] 6.1 扩展双 Provider app 本地流式 fixture，生成推理、Read/Question/受批准副作用及最终回答；验证每 sample 设置一致、display 与真实正文分开、结果配对有序且副作用恰好一次。
- [x] 6.2 使用更新后的固定当前 bytes 验证同 ThreadID/model/设置/来源恢复：完整请求、native 顺序及 request/native/profile fingerprint 等价，thinking/signature/redacted/encrypted/phase 保真，工具零重跑。
- [x] 6.3 验证压缩前后同配置恢复、摘要 provenance 的 thinking usage 保真与普通重建 full；改配置 resume 不改旧 journal 前缀、不隐式采样，不宣称不同设置或 full/incremental wire bytes 相同。
- [x] 6.4 覆盖参数 400、鉴权/限流、无可见回答的 output incomplete；请求次数和状态断言证明不删 display/summary、不降级、不加预算或重发，Anthropic 普通 terminal 和摘要截断分别保持。
- [x] 6.5 扩展随机 chunk/半包/UTF-8、部分工具输入取消、阻塞 stream/shutdown；用确定性门闩验证零提前副作用、唯一 terminal/cleanup owner 和资源最终释放。
- [x] 6.6 覆盖生成前安全 WS 回退及禁止回退分支；404 等允许路径保留同一计划，参数拒绝/生成写入后错误不回退、不重发，receipt/evidence 与发送策略一致。

## 7. 只读展示与宿主边界

- [x] 7.1 app 从同一 resolved 设置和选择来源装配非敏感 TUI 值；集成断言 /context 模式/budget/display/effort/summary/上限与真实请求一致，不能从 resolved 值倒推显式来源或读取 Provider/config。
- [x] 7.2 扩展有界布局及必要滚动，固定 snapshot/按键测试覆盖普通/窄/极小尺寸、NO_COLOR、hostile metadata、关键值与 Esc，现有焦点/取消优先级保持。
- [x] 7.3 验证 display omitted 但正 thinking usage、summarized 但空正文、未知 usage 等组合；配置、实际返回和计量分开，Ctrl+O/原文切换/resize/查询不改变请求或触发补取正文。
- [x] 7.4 扩展 text/json/stream-json 回归；外部字段/版本保持，已有 Anthropic reasoning_output 从 NA 更新为正确 known/unknown，不能因此删断言，正文/签名/密文/未知字段哨兵不泄漏。
- [x] 7.5 新增 scripts/reasoning-controls-smoke.py 的 Darwin 双 Provider 本地 PTY 场景；验证 display/usage、/context、工具后摘要或预算拒绝、Ctrl+O、取消及退出终端恢复，记录实际平台范围。

## 8. 文档与完整验收

- [x] 8.1 同步 README、总体架构、能力清单、Roadmap 和必要 pitfall；核对五字段及 display、摘要沿用策略/预算拒绝、计量三态、当前 shape/固定 fixture 和平台边界，文档链接有效且不宣称延期能力已实现。
- [x] 8.2 最终执行 make verify 并保留完整结果，验证格式、vet、Staticcheck、架构、全量和 race；不以删除/跳过/弱化断言或一次隔离重跑覆盖全门失败。
- [x] 8.3 对九份 delta 做实现对照和 owner/secret/dead code/依赖自检，记录实际 golden/固定 bytes、PTY、证据一致性与缓存恢复结果；strict/diff 通过后再按独立 sync/archive 流程推进。

## 9. 全面回归与归档核对（2026-11-11）

- [x] 9.1 复现并修复双 Provider 重复 JSON 成员覆盖计量/响应身份的问题；在改变归并状态前复用完整帧校验，保留合法未知扩展、连续 thinking 正值/明确零和缺失观测语义。
- [x] 9.2 新增计量回归与独立整数判定 fuzz；应用异常流验证零成功 sample、零工具执行、无重试、无私有正文泄露；24 组真实 CLI 验证三种宿主、跨进程恢复、请求 bytes 和 native/normalized 一致。
- [x] 9.3 重新执行八套 32 个 PTY 场景、5 轮乱序 race、421,051 次 fuzz 和清理测试缓存后的完整 make verify，保留修复前失败与修复后成功证据，更新 verification 与全面回归报告。
- [x] 9.4 按 AGENTS.md 核对九份 delta、实现、测试路径、实际 record vocabulary、架构、README、能力清单、Roadmap 与 pitfall，补齐本轮修复及正确归档链接。
- [x] 9.5 同步九个 capability，验证完整 requirement/scenario 合并、未涉及内容与 Purpose 保留、重复同步幂等；执行主规格及全库严格校验、完整质量门与文档链接检查，通过归档准入核对。
