## 1. 纯决策与配置

- [x] 1.1 在 internal/context 实现自动压缩值对象、阈值及封闭决策，单测覆盖阈值边界、unknown、极值溢出、摘要预留大于普通预留和小窗口不可用。
- [x] 1.2 在 internal/config 接入 auto_compact 严格布尔解析及 app 装配，测试默认、禁用、无窗口 true、null、重复、错误类型与原配置启动不退化。

## 2. 当前契约与恢复

- [x] 2.1 增加 typed manual/auto trigger 与自动 TurnID/sample association，完善构造器、strict decoder、validator 和架构守卫，协议非法组合测试通过且无 any/包级可变 registry。
- [x] 2.2 扩展 Session replay 在初始输入提交前与完整工具 outputs 后允许自动压缩，验证目标 sample，拒绝 pending tools/非法关联及压缩区间普通事实；固定 bytes fixture 覆盖 completed、failed、活动 turn 和旧 shape fail closed。
- [x] 2.3 实现崩溃时悬空压缩与 turn 的确定收口顺序，故障 fixture 验证零网络/零 executor、seq 单调、journal prefix 不变及同 lease 下 repair，以及 completed 后未保存输入不被伪造为已提交。

## 3. Runtime 执行骨架与输入

- [x] 3.1 将摘要拆为手动独占入口与已持有 owner 的内部骨架，现有手动压缩全套回归保持且验证并行 compact 被 busy 拒绝。
- [x] 3.2 实现统一的下一 sample 准备协调及每目标 sample 一次额度，在输入候选预检接入自动摘要与重新 Prepare，双 Provider 测试验证摘要不含候选、正文/InputID 保真、只提交一次、无历史走普通预检、纯编码大小错误不触发、初始/steering 拒绝差异和仍超限返还。
- [x] 3.3 在工具 outputs durable/finalize 后接入自动决策，测试审批/执行中不切窗、工具结果与 steering 同时满足只摘要一次、正常继续同 turn、sample/call/result 预算不重置与副作用不重跑。
- [x] 3.4 实现可恢复失败后普通请求预检与来源抑制，测试旧窗口可继续/不可继续、确定收口与内部故障区分、相同来源不重试及已保存输入不返还重跑。
- [x] 3.5 经 turn worker publication 交付自动事件及 coherent snapshot，受控屏障测试验证 query/interrupt/shutdown 响应、取消与 Sync 竞争后已成功窗口不回滚且零下一请求、过期 ID 隔离和 cleanup/lease 所有权。

- [x] 3.6 实现进程会话连续三次自动失败熔断，测试跨 revision 累积、摘要预算预检拒绝计数、skip 不计数、手动不受限且成功重置，调度状态不写 Session。
- [x] 3.7 实现明确区分手动/自动的确定性摘要包装，双 Provider golden/恢复测试验证自动直接接续、最新纠正进入摘要请求、非授权语义不变且不承诺完整 Session 读取能力。

## 4. 宿主交互

- [x] 4.1 TUI 增加活动 turn 内压缩状态与历史边界，固定尺寸 snapshot 和按键测试覆盖 Ctrl+C、Esc、Ctrl+O、草稿、queued/returned、无色及窄屏，摘要完成不完成 turn。
- [x] 4.2 stream-json 编码自动 trigger 与原 submit/turn/sample 关联，严格解码/golden 验证无虚构 response、可恢复 failed 后同 turn 继续且仅一个最终 turn 终态、事件顺序、EOF drain 与断管清理。
- [x] 4.3 一次性 text/JSON 过滤自动压缩事件，双 Provider app 集成测试证明最终输出保持原词汇，摘要正文与 secret 不外泄。

## 5. 集成验收与文档

- [x] 5.1 双 Provider mock SSE 集成覆盖触发、未触发、多次不同窗口、无缩减、refusal/incomplete、工具调用、取消、受控 timeout、Sync 失败，断言工具/网络次数及旧窗口是否保持。
- [x] 5.2 提交不可变当前 fixture 与恢复等价测试，在相同后续输入下对比 uninterrupted/restored 下一请求 canonical bytes、原生顺序及 fingerprint；cache 回归验证稳定前缀与未触发路径不变。
- [x] 5.3 真实子进程测试覆盖自动摘要持有 lease 期间禁止 repair、崩溃释放及续写 seq，失败路径 journal bytes 不变；所有竞争使用确定屏障不靠 sleep。
- [x] 5.4 同步 README 配置/交互说明、架构、Roadmap 和 pitfall，交叉核对主 specs、实际 vocabulary、测试路径；不宣称 P4 整体完成。
- [x] 5.5 执行 openspec validate add-threshold-auto-compaction --strict 与 make verify，记录命令、结果及平台范围；通过后同步 specs，不保留旧 shape reader；归档作为后续 archive 工作流执行。

## 6. 用户修正：移除 Session 版本机制

- [x] 6.1 删除 Session 版本字段、常量、构造赋值与 decoder 门禁；验证无替代标记且 typed 当前记录仍严格校验。
- [x] 6.2 更新唯一固定 fixture/checksum 与回归测试，验证版本字段作为未知字段拒绝、缺字段/非法 kind/篡改/恢复失败时 bytes 不变，以及双 Provider 恢复等价。
- [x] 6.3 更新 AGENTS、架构、Roadmap、README 与 OpenSpec，明确未经用户明确指示不得引入多版本兼容、迁移或预留内部版本门禁；核对不存在 Session canary 声明。
- [x] 6.4 执行完整 make verify 与 OpenSpec 严格校验，更新验收记录。

## 7. 全工程审计与剩余门禁清理

- [x] 7.1 删除 native/checkpoint payload 版本字段、常量、构造参数与校验，更新 fixture 和结构错误回归，验证双 Provider 恢复请求、顺序与 fingerprint 等价。
- [x] 7.2 删除压缩来源固定 prompt revision 字段及门禁，保留实际摘要内容校验和来源链，验证损坏/错配来源拒绝。
- [x] 7.3 删除 Catalog user_version/schemaVersion 门禁，保留实际结构与完整性验证，测试无业务意义版本元数据不触发重建而错误结构仍正确重建。
- [x] 7.4 同步规范、文档和审计结论，增加防回流架构守卫；执行 make verify 与严格 OpenSpec 校验。
