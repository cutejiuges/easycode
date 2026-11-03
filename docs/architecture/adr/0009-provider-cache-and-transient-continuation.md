# ADR-0009：缓存策略与原生瞬态续写分离

- 状态：accepted
- 日期：2026-11-03
- Roadmap 阶段：P4
- OpenSpec：[add-provider-prompt-caching](../../../openspec/changes/archive/2026-11-03-add-provider-prompt-caching/design.md)

## 背景与选择

原生恢复、七工具与 durable checkpoint 已具备纵向闭环，稳定分段可以驱动实际缓存；此时推进缓存可复用既有请求、计量和提交边界。Claude Code 2.1.88（`a8a678cb6244e6770e1e421767ff0987a1d95549`）提供显式断点、稳定 TTL 与本地诊断交互参考；Codex（`6f8bf683cee83ed4d580a2e106b587d8451db936`）提供原生 Responses、持久连接和精确前缀增量参考。两工程只读，具体定位与取舍见 design 的参考证据。

三种配置互相独立：Anthropic disabled/ephemeral_5m/ephemeral_1h，OpenAI omit/thread key，HTTP/SSE 或 websocket_auto。默认 HTTP 请求 bytes 保持既有 golden。Factory 必须显式绑定合法 ThreadID；thread key 仅由固定用途命名空间与该身份派生，不进入 Session 或诊断。

Anthropic 在最终请求副本上覆盖断点，最多四个：最后工具、非空项目、满足 20-block 回看距离的历史边界和最后合法内容。摘要指导后缀不标记。thinking、signature、redacted 和未知字段保持原生字节；请求包装不进入事实源。5m 不发 ttl，1h 显式发 ttl=1h，会话内策略不可变。

## 原生事实与传输

完整逻辑请求始终由原生历史编译，内容、策略、完整请求与原生输入 digest 分别比较。HTTP/SSE 仍由 Resty 实现；coder/websocket v1.8.14（ISC，go.sum 固定 checksum）仅在生产 transport 包使用，不引入模型 SDK、私有 Codex header 或登录体系。

WebSocket 保持 store=false，response.create 省略 HTTP stream 字段。只有同一活动连接、上一普通响应完成 durable Sync/finalize、全部非 input 属性相同，且完整原生输入精确等于此前 input + outputs 的前缀时，才发送 previous_response_id 与有序后缀。opaque reasoning 与 message phase 参与字节比较。恢复、取消、错误、断线或摘要切窗使资格失效；摘要使用独立 full 连接，成功与失败均重置下一普通请求。写 create 前仅对 404/405/426 或明确连接不可用回退，回退后会话固定 HTTP；鉴权、TLS、重定向、用户取消及写入后故障均不自动重发生成请求。

连接登记在显式 Open 前获得 owner。唯一活动 reader/writer 持有原 done；空闲没有隐藏 reader。Close 取消正在握手、读或写的原 owner，关闭底层资源并等待原完成信号。正常关闭先 join Session，再关闭 Provider，最后释放 writer lease 与仓库；超时也保持此资源所有权。

## 观测与产品边界

PreparedSample/PreparedCompaction 必须携带唯一当前、严格校验的请求收据。Runtime 冻结同一采样计划的无正文来源，在 append 前验证；Sync/finalize 后才更新有界观测。普通 sample、成功 turn 和成功摘要各保存最近一项；失败不伪造成功 turn。publication 将值交给 AgentLoop 单 owner，活动请求和审批期间查询不读取 worker 可变状态。

/cache 与 /context 不进入模型历史；stream-json v1 的 cache.query 明确接收 control.response 后输出 cache.snapshot。TUI 在提交前分配 request/generation，关闭后的迟到响应无效。快照最多五种来源、64 KiB，明确 process/restored_baseline，排除正文、真实 key、URL、凭据、路径和原生 reasoning。所有适用输入指标已知且无溢出、分母非零时才计算 cache_read 比例；相同 fingerprint 不证明实际命中，读取降低也不能确定归因 TTL。

不增加 Session/native/checkpoint 字段、版本、迁移或兼容 reader。跨 Provider compacted fork、扩展目录、精确费用、远端 tokenizer、telemetry 和后台进程仍需独立变更。

## 验证

见[验收矩阵](../../../openspec/changes/archive/2026-11-03-add-provider-prompt-caching/verification.md)：固定 request/stream golden、完整 bytes 恢复、原生前缀重组、真实工具/steering、Sync 故障、握手/读写取消、真实子进程 lease、严格宿主查询与固定尺寸 TUI，以及 make verify。没有远端真实凭据测试，不据本地模拟声称实际命中率、延迟或费用收益。
