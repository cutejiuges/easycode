## 1. 当前领域契约与严格配置

- [x] 1.1 在 domain 增加 PostToolUseFailure 和 success/failure/none 封闭选择，区分原工具事实、后置选择和检查 disposition，保持 Pre 的 feedback/exit 2 限制；以三事件及三个独立维度的合法/非法组合单测验证，none 不解释为工具未执行。
- [x] 1.2 将 PostHookPlan/terminal 替换为调用归属只存一次的当前结构：外层 invocation_id/capability/branches/allowance/reservation/digest，分支仅 event/hooks/delivery；复用既有引用校验、排序和摘要规则，补齐 typed constructor/strict decoder/validator/clone，验证重复归属、空/重复/乱序分支、跨分支来源 revision、旧 shape 和 digest 篡改均拒绝。
- [x] 1.3 实现独立 typed failure stdin 和安全英文摘要投影，明确 is_interrupt=false；验证三能力输入、512 KiB 边界、重复/缺失/null/错事件及合成 secret 不外泄。
- [x] 1.4 扩展同一 hooks.json loader 的三事件规范排序、总数量/id 与 per-event 匹配限制；以配置键重排 canonical bytes/revision、非法来源失败关闭及 getter 隔离测试验证。
- [x] 1.5 扩展现有输出 parser 和 HookExecution validator，两个 Post 只接受自己的 additionalContext；验证 none/model、exit 0/2、UTF-8/NUL、重复/未知字段、超限与 Pre 行为未放宽。

## 2. 共享后置计划、预算和执行路由

- [x] 2.1 在 tool 层实现仅依赖同调用 durable 执行尝试准入/原结果的 success/failure/none 纯选择，向 runtime/replay/恢复提供同一语义；表驱动验证准入后启动失败、error/conflict、go test/rg 正常 exit=1、原结果 cancelled 及 none，不传 context/当前配置/预算/恢复模式，不从错误文字猜测。
- [x] 2.2 将 runtime 执行骨架返回值及重复编排缓存显式携带执行尝试准入证据，replay 从已验证 started sequence 重建，不新增进程已启动事实；保留候选/批准/读证据规则，验证进程未启动但尝试已准入、准备错误无准入和原工具运行后取消三者不混淆。
- [x] 2.3 在候选前冻结并 Sync 两个可能分支，依次完成精确信任，保留最多 16 项上限和单 invocation owner；验证跨事件同命令不共享信任、默认拒绝/取消/过期响应与 headless 零目标执行。
- [x] 2.4 扩展既有 result budget ticket，任一分支 model 仅 reserve 一次，各分支独立计算 delivery；验证提示槽不相加、A/R/base allowance、只有未选择分支 model 时原预览仍按 A-R 冻结、none/unmatched 只释放并 close 一次且没有伪后缀。
- [x] 2.5 扩展 feedback freezer、renderer 与 ModelOutput，组合边界接收经调用归属校验的选择/计划证据，仍用原包装并绑定 event/plan/ordinal；验证跨分支第 0 项拒绝、none/unmatched 空反馈、完整接收/拒绝、实际编码、4/16 KiB 和重复 close，反馈保持工具数据角色。
- [x] 2.6 在原工具唯一 result Sync 后先选择事件，再按分支存在性、当前取消、输入与剩余期限决定启动，复用原编排/30 秒预算；验证三能力确定失败、准入后启动失败、确认清理的 Exec 超时，以及 error Sync 后取消仍为 failure/cancelled 且零脚本，普通检查错误不重写原结果或递归重试。
- [x] 2.7 收口 none、unmatched、取消、poison 和重复编排路径；以计数器和 Sync 故障注入验证无 durable result 不选择、unmatched 保留 success/failure 且零运行/反馈、poison 不启动、缓存准入不丢失、未 close 不开始后序调用。

## 3. Session 当前格式、回放与恢复

- [x] 3.1 更新原后置 draft/decoder/replay，交叉验证精简分支计划、准入/原结果选择、run identity、连续前缀和唯一 terminal；验证原 error 后检查取消必须保留 failure、原 result cancelled 必须为 none、跨分支第 0 项及错误 digest/预算均失败关闭。
- [x] 3.2 直接替换受影响 testdata/current 固定 bytes，增加准入后启动失败、双分支、none、unmatched、原 error 后取消、关闭披露、预算不足及各崩溃切点；生产 loader/replay 验证，测试运行时不得生成 fixture 输入。
- [x] 3.3 扩展 post_hook_recovery 的确定失败、项间中断、Hook started 未 finished 和终态后缺 outputs 补偿；验证恢复与实时路径选择相同、未配置分支补 unmatched 而非 interrupted、原错误/前缀不变、recovery 后缀无新内容，全部外部调用计数为零。
- [x] 3.4 验证新结构二次崩溃恢复幂等、seq 单调、单 result/terminal/output/预算收口；特别验证已保存的 failure/cancelled 终态原样复用，不被 recovery 原因替换，使用故障注入覆盖各次 append/Sync 失败与 poisoned admission。
- [x] 3.5 增加新失败恢复路径的真实子进程 lease 竞争、占用期间禁止 repair、崩溃释放测试；断言失败路径 journal bytes 不变，corruption 加半行仍先拒绝而非修复。

## 4. 双 Provider 与 app 端到端

- [x] 4.1 扩展两家 typed ModelOutput 接收和 preflight，仅放行与原结果关联的后置反馈；golden 验证 OpenAI function/custom apply_patch 与 Anthropic tool_result 的原身份、status/is_error、顺序和唯一配对。
- [x] 4.2 新增本地 fixture server 的失败检查后继续采样场景，覆盖成功/失败互斥、none/model、无匹配、拒绝与普通检查错误；断言模型实际收到预期冻结反馈且不重复执行工具。
- [x] 4.3 对两家完成反馈、项间中断、启动失败及原 error 后取消分别比较同一冻结/补偿历史的 uninterrupted/restored canonical request、native 顺序、opaque bytes、footprint 与 cache fingerprint；配置/脚本/artifact 删除后仍离线等价，不宣称与无 model 预留的整个请求相同。
- [x] 4.4 扩展 hook cache golden/regression 与现有 OpenAI WebSocket 路径；验证声明/信任/活动身份不污染稳定前缀，无匹配 schema/catalog bytes 不变且原 fallback 行为保持。

## 5. TUI 与宿主投影

- [x] 5.1 将 PostHookCommittedPayload 当前字段收敛为 invocation_id/plan_digest/detail_sequence/phase，删除固定 event，不增加可空事件或新 kind；验证旧 event shape 严格拒绝、原结果未知时仅中性 planned 投影，实际事件从同调用/计划/序列快照读取，错分支及过期状态不覆盖详情。
- [x] 5.2 扩展 /hooks 和信任 overlay 的第三事件及执行时机/披露文案，说明执行尝试准入不证明进程启动、正常非零退出仍走成功事件；固定尺寸及窄屏 snapshot 验证完整命令审查、默认拒绝、Esc/Ctrl+C/paste 隔离。
- [x] 5.3 接入失败后的 checking/finished/terminal 和 Ctrl+O 反馈详情，保留原 Error/Conflict、草稿与锚点；验证原 error 后取消保持 failure、none 不表示原工具未执行、检查通过不冒充修复，cleanup 完成前不 idle。
- [x] 5.4 增加或扩展真实 PTY smoke，演示执行尝试确定失败、诊断反馈、原 error 保存后取消、结果未知的中性计划及详情/过期消息隔离；保存实际输出与退出状态，不把渲染 snapshot 当作完整交互验收。

## 6. 进程与安全专项验证

- [x] 6.1 复用原 HookRunner/process owner 完成 Darwin 真实失败后脚本用例；验证只读工作区/禁网、私有 Session/home 根不可读写、无敏感环境继承，原工具错误保持。
- [x] 6.2 使用假时钟和通道屏障注入超时、阻塞 stdin、输出超限、I/O 故障、cleanup 不确定及 shutdown timeout；验证最终完成信号、强制清理路径和 lease 所有权，不用 sleep 或概率竞态。
- [x] 6.3 在原始输出、错误和被拒正文中放入合成 secret 哨兵，检查 Session、模型、错误、snapshot、diagnostic 与 artifact 均无未授权泄漏；只允许明确 model 授权的完整字段持久化。
- [x] 6.4 运行新增路径相关的 race 与支持平台检查，确认非 Darwin 仍失败关闭；验收报告分别列出真实执行、交叉编译与不可运行项，不把编译成功宣称为平台运行支持。

## 7. 质量门、文档与验收收口

- [x] 7.1 同步 README、capability-status、总体架构、Roadmap 与 pitfall，明确本能力为执行尝试失败诊断、不含命令语义解释或测试失败自动修复；说明 success/failure/none、取消门禁、正常非零退出、双分支信任/预算和中性计划通知，核对链接与实际 vocabulary。
- [x] 7.2 执行 make verify，确认 gofmt、vet、Staticcheck、架构边界、全量和 race 全部通过；记录命令/退出状态，对无法执行的检查写明原因与风险。
- [x] 7.3 执行 openspec validate add-post-tool-use-failure-hooks --strict 并逐项核对 specs、design、tasks 与实际功能/测试，确认无占位、兼容 reader、重复副作用或无用依赖；验收记录区分本次基线和新增功能验证。
- [x] 7.4 汇总真实 PTY、双 Provider canonical/cache、故障注入和子进程 lease 的验收证据，只有业务断言与质量门通过才勾选完成；再同步需要沉淀的主规格，并按获授权的 archive 流程收口，不代为归档其他 change。


## 8. 2026-11-11 独立回归与审查消息修复

- [x] 8.1 扩充双 Provider 三能力失败端到端矩阵至 66 例，并补充 6 例双分支真实 Exec 正常退出回归，覆盖普通拒绝、空正文、错事件、4 KiB 超限和编码后预算拒绝；保留并记录修复前失败证据。
- [x] 8.2 按 8 KiB 原始命令与最坏 JSON 编码预算修复信任审查消息边界；三事件最大命令完整往返，未知字段和原始超限继续失败关闭；原工具结果和唯一配对不变。
- [x] 8.3 执行不使用测试结果缓存的 make verify，固定种子乱序 race 重复、输出解析 fuzz、双 Provider 三事件真实 PTY 与平台编译检查。
- [x] 8.4 同步主规格、架构/踩坑和验收报告，分别保存修复前失败与修复后通过的实际命令、退出状态和原始证据；列出平台不可执行项，不代为归档。


## 9. Windows 全量测试编译修复与追加回归

- [x] 9.1 将 listCatalogStub/listWriterFunc 移入无平台标签的共用测试文件，保持 helper 实现、平台专属测试标签及全部业务断言；验证 Windows 全量测试编译。
- [x] 9.2 重新执行不使用测试结果缓存的 make verify、双 Provider 72 例端到端 race，以及 Linux/Windows amd64/arm64 的全库构建和测试编译；保留原始输出及退出状态，区分交叉编译与原生执行。
- [x] 9.3 追加验收报告与平台标签踩坑记录，保留前次失败证据，标明后续修复结果；不修改能力契约，不归档。
