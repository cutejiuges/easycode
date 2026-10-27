## 1. 固定 codec 并统一严格解码

- [x] 1.1 在 `internal/codec` 用显式固定配置消除对第三方全局 codec 的读取；增加隔离子进程替换默认 codec 的回归，验证 canonical bytes、checksum 与 fingerprint 不变。
- [x] 1.2 将重复键、字段存在性/null、大小写、尾随内容及无损 Unicode 基础检查集中到 codec；以嵌套对象、合法代理项对、孤立代理项和 optional/default/null 表驱动测试验证。
- [x] 1.3 迁移 Read/Glob/Grep/Patch/Write/Exec 参数 decoder 到统一入口，保留各自大小上限和业务 validator；验证六项工具合法默认值不变、显式非法 null 和歧义字段均失败且 executor 零调用。
- [x] 1.4 迁移 config、protocol、headless 和 Session 的自有 JSON 解码，列明 required/optional/nullable 字段并覆盖嵌套对象；运行对应包测试，验证 current fixture 固定 bytes、外部 JSONL golden 和非法 journal 不被改写。
- [x] 1.5 迁移双 Provider 的自有 native envelope 严格解码及 JSON 基础调用，保持外部 wire 的未知扩展策略；运行 commit/native/reducer/restore 测试，验证 opaque bytes、usage 三态和 tool pairing 不变。

## 2. 收敛 prepared commit 当前入口

- [x] 2.1 将 sample/tool-output 的当前构造器改为显式要求 finalize/discard，删除 WithDiscard 别名及旧简化签名，同步所有生产与测试调用；通过引用检索、编译和 `go test ./internal/provider/... ./internal/runtime` 验证无旧入口残留。
- [x] 2.2 补充零值、缺失回调、重复及竞争 finalize/discard、durable 失败释放 staging 的回归；断言回调计数、history revision、完成时机及失败后下一请求内容正确。

## 3. 统一宿主会话控制与关闭所有权

- [x] 3.1 移除 `runtime/session.go` 中独立启动 turn 的会话状态机，使一次性 headless、TUI 和 stream-json 共用 AgentLoop 控制生命周期；以三入口集成测试验证单 owner、单次执行、FIFO 与原退出码/输出格式。
- [x] 3.2 为 app 宿主 adapter 建立显式启动、停止、dispatcher done 与最终结果，统一事件流关闭责任；通过启动取消、admission 拒绝与 terminal 并发回归验证无重复关闭或 send-after-close。
- [x] 3.3 修复 TUI 已退出且事件 buffer 满时的分发阻塞，停止投递后继续排空内核；用 barrier 控制消费者退出，验证 worker/loop/dispatcher 都完成且 journal terminal 恰好一次。
- [x] 3.4 将 `chatResources.close` 绑定到实际启动的控制 owner，传播 runTUI 的关闭错误并保留重复关闭最终结果；以故障注入验证 owner 完成先于 writer/workspace/repository 释放。
- [x] 3.5 完成 shutdown timeout 的活动 stream 取消与升级路径，必要时在 transport 边界提供可解除实际阻塞的显式能力；用阻塞 stream 与真实竞争子进程验证清理完成前 lease 不释放、完成后可获取，不用 sleep 证明顺序。
- [x] 3.6 回归一次性 headless 断管、stream-json EOF/显式 shutdown、TUI interrupt/approval 和 resume；运行 `internal/app`、`internal/headless`、`internal/runtime`、`internal/tui` 的相应用例并比较既有 golden。

## 4. 按职责收敛工具生命周期

- [x] 4.1 从 Runtime 提取有序调度、实例级审批状态和副作用执行骨架，采用骨架持有强类型策略接口；通过依赖守卫及混合 Read/Patch/Write/Exec 测试验证只读有序并行、exclusive 屏障和 call/output 顺序。
- [x] 4.2 统一 Patch/Write grant 的 Sync 后安装、deny 优先级及真实 decision source，保持 Exec 单次 ask；新增批准 Sync 失败、已有 grant 遇 deny、跨 capability grant 隔离和来源事实断言。
- [x] 4.3 统一执行 started 的取消线性化，去除 Patch 接收前绕过取消的路径；用可控 writer admission 验证接收前 executor 零调用、接收后确定结果、健康取消不 poison。
- [x] 4.4 对三种副作用工具统一验证 invocation/provider call/capability/wire kind 和候选相关 metadata，非法执行结果转为 outcome_uncertain/invalid_tool_result；用注入错配结果验证后续副作用零调用、Runtime poison 和健康 journal 的有序配对收口。
- [x] 4.5 保留工具特有安全策略与 UI 事件时序；运行 Write Read-observation/generation、Patch conflict/symlink/Sync、Exec sandbox/cleanup/progress 与批准 TUI snapshot 回归，验证无权限扩大或重复副作用。

## 5. 架构守卫与文档收敛

- [x] 5.1 扩充核心 API 动态类型守卫至 tool，增加 JSON 调用越界、可变全局 codec 及旧构造入口负例；以 `internal/architecture` 测试验证负例失败、真实 canary/content revision/opaque 正例通过。
- [x] 5.2 将 New* 副作用守卫扩展到全部生产包与项目内可解析调用链，并为无法静态证明的资源边界补生命周期测试；增加直接和间接副作用负例，验证纯内存构造不打开资源、不启动隐藏 goroutine。
- [x] 5.3 同步总体架构、Tool 专题、Roadmap、pitfall 和受影响代码注释，纠正 Tool 占位、工具数量与失效 v1 描述；逐条核对实际 record vocabulary、当前 canary、真实测试路径和未完成阶段，保留现有批准占位边界。

## 6. 联合验收与归档准备

- [x] 6.1 双 Provider 验证 uninterrupted/restored 下一请求 canonical bytes、原生 item 顺序、opaque bytes 和 fingerprint 等价；执行 request/restore/usage/cache golden 回归并确认动态配置未污染稳定前缀。
- [x] 6.2 执行 Session 真实子进程 lease 竞争、崩溃释放、占用禁止 repair、seq 单调及失败 journal bytes 不变回归；结合宿主 shutdown 用例验证没有提前释放所有权。
- [x] 6.3 执行 `make verify` 与 `openspec validate unify-current-contract-lifecycle --strict`，建立本 change 场景到测试的验收对照；明确记录不可运行的平台检查及风险，不以交叉编译替代运行测试。
- [x] 6.4 核对 proposal/design/specs/tasks 与实际实现一致，六类发现全部关闭、无旧入口/双 reader/无期限兼容分支后同步主 specs；验证 OpenSpec 严格校验和文档引用，满足条件后再按归档流程归档本 change。
