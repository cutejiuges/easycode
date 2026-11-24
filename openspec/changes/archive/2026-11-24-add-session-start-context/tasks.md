## 1. 来源、配置与执行输入输出

- [x] 1.1 实现三个封闭来源、字面 matcher 和真实激活/压缩锚点，接入现有冻结来源、总量、排序及指纹。扩展现有配置用例，验证对象键重排不改变规范摘要，事件披露、命令或 matcher 变化使旧信任失效，非法来源和合计超限拒绝整个快照。
- [x] 1.2 实现严格源分支 stdin、输出规则及有界包装。用同一小组样例验证手动和自动身份字段、获准 `additionalContext`、exit 2、非法字段、限额、未授权与精确空白，并确认原始双流哨兵只留在受限 runner。
- [x] 1.3 扩展现有信任身份和运行结果校验，沿用声明级 trust、只读禁网 runner、时间预算和关闭 owner。用假 runner 验证全部声明获准前零执行，顺序和 30 秒预算正确，旧事件、旧计划和取消后的响应无法派发。

## 2. 原生交付与 Session 回放

- [x] 2.1 增加 `session_start_hook_planned`、`_started`、`_finished`、`_closed` 的当前 typed draft、严格解码和回放。用固定 current bytes 检验激活三记录批次、已安装 checkpoint 后的 compact 单记录、真实序号，以及含合法 checksum 的身份、最长合格前缀、候选摘要、批次或作用域伪造。复用这些固定字节覆盖晚于初始输入或 resume 的 startup、绕过 PostCompact、伪造 singleton 和真正撕裂的合法激活尾批。
- [x] 2.2 为 Anthropic Messages、OpenAI Responses 加入各自原生 `session_start_context` 编码、足迹与先后关系校验。用代表性 native fixtures 验证同批初始输入及真实 checkpoint 原生前缀、工具和 Stop 前驱状态，无额外 sample、usage、InputID 或角色升级。用没有 steering 的自动工具续写断言真实下一次 stream 与 sample commit 的 TurnID 和下标。
- [x] 2.3 复用原生 staged commit owner 支持激活复合交付和 compact suffix 提交，并将验证过的原生上下文与 checkpoint、现有原生项按实际序号合并恢复。用确定性 writer 验证真实 arity 的最大信封预检与 finalize/discard 互斥、连续 lease、Sync 接纳前后取消及持久化成功后误报可重试。
- [x] 2.4 先验证完整 Session 和各 Provider 的真实 native 内容，然后修复合法尾部并本地结算已开始的尝试。用故障注入覆盖追加前、started 后、applied 后及重复恢复，断言拒绝损坏时仍保留 journal bytes，合法恢复期间零脚本和网络。补一个 compact 补充、真实续写、再次 checkpoint 的代表性场景，核对被覆盖的补充仍严格校验，完整恢复和目录 metadata 的原生修订一致。

## 3. 激活与压缩接入

- [x] 3.1 在新建、CLI 恢复和已接管的选择器目标建立一次性触发，首轮在既有自动压缩判断后消费。复用现有会话交接回归，验证预览、取消候选、同线程切换、CLI 继续、恢复后先手动压缩和实际被采用目标的调用次数及来源。加一次输入预检拒绝后再提交合法请求的场景，确认触发机会保留。
- [x] 3.2 将激活产出的有界内容和真实首轮输入一起预检、Sync 和发布。验证预算超限时只尝试一次原输入，首轮模式、effort、Skill 与文件原字节保留，取消或 Sync 不确定时使用正确输入 disposition，结构错误及过期 prepared 不走额度回退。
- [x] 3.3 在共享压缩 owner 的已安装 checkpoint 和 PostCompact 终态之后接入 compact。用边界回归验证无 PostCompact、有 PostCompact、确定观察失败、压缩前拒绝、传输回退、手动完成后立即退出、仅有补充时再次压缩及 Ctrl+C 情形的计数、唯一成功窗口和关闭顺序。用同一 fixture 注入接纳计划前与 started 后运行前取消，确认公开压缩成功不导致 auto 队列自动开始，strict-next 保留原顺序，只在清理后继续。
- [x] 3.4 将已冻结的初始输入、工具后的 steering 或 Stop 反馈候选送入 compact 的纯组合预检。实测缩减、硬预算和实际编码边界，确认超限保留原成功 checkpoint 与候选，只有一次摘要和一次领取，原 Stop with-steering 自身超限仍使用原 feedback-only 回退。
- [x] 3.5 验证 compact 与 startup/resume 同一首轮的确定顺序，及工具输出闭合、Stop 继续的原生顺序、真实 sample 下标和反馈归属。用运行时确定性取消或 Sync 故障检查不确定时停止额外 native/网络副作用，健康取消由现有 Stop 补偿收口。

## 4. 交互与宿主投影

- [x] 4.1 扩展 `/hooks` 的三个来源与模型披露文案，复用信任覆盖层。扩充现有输入路由和窄屏用例，验证默认拒绝、Esc、Ctrl+C、保留草稿、粘贴和迟到响应隔离。覆盖 `*` 首轮信任和后续 compact 复用、独立 PostCompact 审查及 reset 只影响未来触发。
- [x] 4.2 发布 compact 的安装、PostCompact、SessionStart 和公共完成顺序。用现有压缩展示用例验证相同 checkpoint 上独立计划、取消等清理、成功窗口仍可查询，以及已提交后 Context 的较新 revision 和历史 checkpoint revision 各自准确，最近真实 cache observation 无提前变化。模拟安装之后 control stream 关闭，保持 checkpoint 事实和安全的不确定状态。
- [x] 4.3 将已应用输出投影为 Ctrl+O 的有界独立详情，精确映射真实 journal 游标、分段文本及当前线程。验证组合原生序号一次安装、Stop 下标和工具关联不变，正文只有已保存授权内容，普通会话主线和离线预览仍按真实消息计数。验证已打开的 Ctrl+O 分页以真实关闭 seq 更新 Through，保持原阅读锚点并拒绝过期页。
- [x] 4.4 核对一次性 headless 与 `stream-json` v1，验证全部触发均跳过新命令和额外派发前检，先前已批准并保存的上下文可继续参与原生请求，外部 stdout 无新增事件、字段、脚本数据或意外顺序变更。

## 5. 集成验收和文档

- [x] 5.1 为两个 Provider 增补少量 canonical 和 cache regression，比较相同输入与固定历史的持续运行和离线恢复，断言完整请求 bytes、原生顺序、真实预算、稳定前缀及动态历史 fingerprint。覆盖 Anthropic 缓存标记、OpenAI HTTP 及恢复和压缩后 WS 首次全量、后续真实增量与回退。断言 reset 原因优先级、上下文只出现一次。
- [x] 5.2 用代表性 Darwin 真终端 PTY 场景验证新建、接管后的恢复、手动压缩、自动接续、审查和取消，使用本地 mock 服务核对真正请求时序和已保存详情。记录测试执行平台、无法执行的路径及风险。
- [x] 5.3 补齐 README 配置示例、能力清单、Roadmap、架构边界与新增踩坑记录，核对严格当前 vocabulary、实际测试路径和无新增占位。正式同步规格与归档按评审通过后的仓库流程执行。
- [x] 5.4 跑严格 OpenSpec 校验和 `make verify`，明确验证 AGENTS 的依赖方向、凭据边界、唯一 owner 及缓存和恢复约束。仅将简短验收结论留在 change，保留必要 fixture，原始日志用本地或 CI 输出。
