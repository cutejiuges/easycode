## 1. 当前契约与严格配置

- [x] 1.1 实现三种独立策略的强类型值、不可变摘要和严格 validator；通过零值、未知枚举、复制隔离及默认禁用单测验证，遵循 design D1/D3。
- [x] 1.2 在 config 解码与最终 Provider 校验中接入三个 JSON 字段；通过 V1 默认、null/重复/大小写、跨 family 和 env 覆盖后冲突测试，证明错误前零网络与 Session 副作用。
- [x] 1.3 将 Factory 创建/恢复收敛为显式合法 ThreadID 的唯一契约，更新两家实现、app/session_service 和测试 doubles；通过构造无 I/O、身份非法拒绝、create/resume 顺序及全量编译验证。
- [x] 1.4 实现固定命名空间的 OpenAI thread key 派生与 omit 路径；通过 V4 同 thread 跨样本/恢复一致、不同 thread 分离、secret/path/world-state 不参与及默认 HTTP golden 不变验证。

## 2. Anthropic 请求覆盖与两种 TTL

- [x] 2.1 拆分最终原生请求构建与缓存覆盖，接入两家普通/候选预检/摘要共享入口；通过重复预检不改变状态、摘要后缀只追加一次及关闭策略 golden 验证，覆盖 V2/V5。
- [x] 2.2 实现 Anthropic 请求专属 tool/content 包装，保持 NativeItem/Raw 持久化结构不变；通过 V2/V3 未知扩展保留、冲突标记拒绝、thinking/signature/redacted/opaque 前后原生 bytes 不变验证。
- [x] 2.3 实现工具/项目/历史锚点/尾部的确定性断点计划、去重与上限；通过 V2 的 0–4 标记、空项目、合法目标、19/20-block 边界及重启相同位置 golden 验证。
- [x] 2.4 实现 ephemeral_5m 与 ephemeral_1h 的统一请求 TTL 和禁用省略规则；通过 V2 两种 TTL canonical golden、内容 fingerprint 相同/策略 fingerprint 不同、活动会话不可变测试验证。
- [x] 2.5 覆盖手动与自动摘要的最终断点选择，排除一次性指导后缀；通过 V5 成功/失败/取消/重复压缩、旧窗口保持、下次普通请求及摘要 provenance 无缓存字段验证。

## 3. 请求收据与原生前缀证据

- [x] 3.1 定义不可变、严格校验的请求收据及完整请求/native input fingerprint，接入所有 PreparedSample/PreparedCompaction 当前构造；通过非法 family/purpose、零收据、复制篡改和默认 disabled/full 收据测试验证，无 optional 旧 reader。
- [x] 3.2 分别基于两家原生单位实现完整前缀比较与 first/append/replaced/boundary 关系，保留 opaque 字段；通过 V3/V7 相同语义但 opaque/phase 不同、正常追加、缩短、替换以及 HTTP full 的关系测试验证。
- [x] 3.3 在 Runtime append 前复制验证冻结来源与收据，Sync/finalize 后建立成功普通/摘要观测；通过 V8 Sync barrier、Sync 失败、discard/finalize 失败、后续工具失败及零重复调用断言验证。

## 4. 有界诊断与单 owner 发布

- [x] 4.1 实现 context 纯来源比较、内容/策略分离、history_appended 与 native_context_changed 原因；通过 V12 全来源、revision-only、策略-only、项目来源 added/removed 和 opaque-only 测试验证。
- [x] 4.2 实现最近成功 sample/turn/摘要的三态计量与有界比例；通过 V12 known zero、unknown、N/A、零分母、整数极值/溢出及摘要隔离断言验证，不重新解释 raw usage。
- [x] 4.3 在 worker publication 与 AgentLoop owner 保存不可变 process 快照、恢复基线和失败状态；通过 V13 重复查询不推进统计、活动网络/审批不阻塞查询、实例隔离及大量样本内存结构有界验证。

## 5. 可选 WebSocket transport 与资源所有权

- [x] 5.1 将 coder/websocket 的已审查发布版本与 checksum 固定到 go.mod/go.sum，封装在 transport；通过依赖/license 检查、架构 import guard 与 HTTP/SSE Resty 依赖保持验证，不引入官方模型 SDK。
- [x] 5.2 实现显式连接、URL 前缀/鉴权/禁止重定向/禁用压缩、30 秒握手及写期限、4 MiB 重组事件和 16 MiB 请求上限；通过 V9/V10 本地 ws/wss、TLS/redirect、分片/UTF-8/二进制/超限和 fake deadline 验证。
- [x] 5.3 实现单连接唯一活动 reader/writer、sample 结束保持空闲连接、显式幂等 Close/done/最终错误；通过 V10 阻塞读写、重复关闭、活动/空闲取消、跨 Conversation 隔离和锁内无 I/O 守卫验证。
- [x] 5.4 接入 OpenAI full response.create envelope 与同一 reducer，识别受控 WebSocket error；通过 V6/V10 full golden、HTTP/WS 同事件原生输出等价、identity/顺序、工具 partial/未知/重复参数拒绝及唯一 terminal 验证。
- [x] 5.5 实现写 create 前的 typed 安全回退与会话固定 HTTP；通过 V9 404/405/426、连接不可用只回退一次、401/403/TLS/redirect/用户取消不回退、写后故障不重发和诊断原因验证。
- [x] 5.6 将 Conversation socket owner 接入 app/Provider/Runtime 关闭顺序；通过 V10 shutdown timeout、底层强制关闭、原 done 最终完成、无新 helper goroutine 及 lease 不提前释放集成验证。

## 6. Durable 增量续写与窗口重置

- [x] 6.1 建立与 socket generation 绑定、仅 finalize 后有效的普通响应基线；通过 V7/V8 completed 未 Sync、Sync 失败、prepared discard、外层取消和跨连接失效测试验证。
- [x] 6.2 实现完整非 input 属性比较与 native input 前缀增量派生；通过 V7 model/tools/key/指令/输出约束变化、前缀短缺/替换/opaque 差异和精确 suffix 重组 golden 验证，新增字段必须有比较覆盖。
- [x] 6.3 在工具 outputs durable 与 steering 已提交后发送有序 suffix，保持 Runtime/ledger 不变；通过 V7/V8 本地双工具及追加输入 E2E 验证 native pairing、冻结 preview 和 executor 恰好一次。
- [x] 6.4 实现 resume、取消、错误、断线与摘要前后的重置；通过 V5/V9/V11 第一条 full、摘要独立 full/关闭、摘要失败后普通 full、previous_response_not_found 不重发及下一显式输入可 full 验证。
- [x] 6.5 按已完成实现、显式配置和会话 HTTP 回退状态声明 PromptCacheControl/PromptCacheKey/PreviousResponse；通过 capability 表驱动测试验证禁用/默认/启用/回退，其他延期能力保持 unsupported。

## 7. 强类型缓存查询与 TUI

- [x] 7.1 接入 QueryCache/CacheSnapshot 当前 constructor、validator、ChatSession 和 correlated control item，扩展 /context 必需策略摘要；通过无版本/无 any 架构守卫、零值/非法字段与 owner 响应顺序测试验证。
- [x] 7.2 实现 /cache 精确本地命令和独立面板，区分配置与最近已观测 transport；通过 V14 参数拒绝保稿、未知 slash 正文、无网络/turn/Session record、审批隔离与 Ctrl+C/Esc 输入路由验证。
- [x] 7.3 实现查询 generation/request 关联及关闭后迟到响应丢弃；通过 V14 交错 /context-/cache、关闭/重开、活动 worker publication 和草稿/光标保持测试验证。
- [x] 7.4 添加缓存面板固定尺寸/窄屏/极小屏/无色/恢复/no-observation/unknown/恶意元数据 snapshot；通过 V14 与 UI resize 不改变请求 fingerprint 回归验证，渲染无 I/O。

## 8. Headless 控制与输出隔离

- [x] 8.1 添加严格 cache.query 外部 v1 decoder 与 QueryCache 映射；通过 V15 任意 chunk、UTF-8、尾行、重复/未知/null 字段、非法 request_id 和超限测试验证，input.submit 的 /cache 保持正文。
- [x] 8.2 添加显式 cache.snapshot DTO/validator/golden，接入唯一 stdout writer；通过 V15 response-before-snapshot、活动任务查询、重复 request_id、64 KiB 边界及敏感字段拒绝验证，不直接编码内部结构。
- [x] 8.3 维持一次性 --print/--json 及无查询 stream 输出，覆盖 EOF drain、shutdown 与断管；通过 V15 旧 golden、输出关闭错误及 cleanup owner 等待验证。

## 9. 恢复、缓存矩阵与跨层验收

- [x] 9.1 使用现有不可变 current fixtures 增加双 Provider 启用策略的恢复测试；通过 V11 同 ThreadID/配置下完整 canonical bytes、native 顺序、内容/策略/请求 fingerprints 逐一相等验证，不在测试中用 encoder 生成历史 bytes。
- [x] 9.2 增加 HTTP full、WS full 和 WS incremental 三路径比较；通过 V6/V7/V11 独立 full 字节级恢复比较、各自 wire golden 及原生 prefix/outputs/suffix 精确重组验证，不以语义文本或 hash-only 降低原门槛。
- [x] 9.3 增加双 Provider 本地模拟服务的“读取/修改/执行测试/总结、恢复、手动/自动压缩”缓存 E2E；通过 V3/V5/V8/V13 断言工具不重跑、opaque 保真、缓存计量归属与主线可读。
- [x] 9.4 固定完整失效矩阵及隐私哨兵：工具/指令/source revision 改变、世界状态/UI 不变、正常 append、窗口替换、策略切换与未知 cache usage；通过 V2–V4/V12/V16 断言精确首个变化与 secret/prompt/opaque 不泄漏。
- [x] 9.5 执行确定性故障组合与既有真实子进程 Session 回归；通过 V8–V11 的 Sync/阻塞/取消/关闭 barrier、lease 竞争/崩溃释放/占用期间禁止 repair/seq 单调/journal prefix 不变验证，不以 sleep 或概率循环证明。

## 10. 文档与完整质量门

- [x] 10.1 新增已接受的缓存/瞬态连接 ADR 并更新索引、架构、Roadmap、pitfall 与配置/交互说明；逐项核对 design 参考提交、真实目录、记录词汇和未实现 P4/P6/P7 边界，无失效链接或格式版本机制。
- [x] 10.2 在 change 验收记录中建立 V1–V16 到实际测试路径/命令结果的对应表；通过逐项证据复核验证全部范围已完成，所有未执行的平台 smoke 明示原因和风险，不声称远端命中收益已验证。
- [x] 10.3 执行相关单测/golden 后完成 make verify、openspec validate add-provider-prompt-caching --strict、openspec validate --specs --strict 与 git diff --check；验证全部退出码为零并记录真实结果，禁止删减/跳过/弱化测试掩盖失败。
- [x] 10.4 在全部功能和质量门通过后完成 specs/架构/Roadmap/pitfall/路径/实际契约一致性复核，准备主 specs 同步和独立 archive；通过需求覆盖及任务全勾选复核，任何完整能力未完成则不宣布变更完成或擅自缩范围。
