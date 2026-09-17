# EasyCode 踩坑与经验记录

> 本文记录实际开发中已经发生的问题、根因和防回归措施。  
> Roadmap 中的“已知踩坑与规避”是预防清单；本文是事实日志。

## 1. 记录规则

遇到以下情况必须记录：

- Provider 兼容行为与官方协议或预期不同。
- thinking/reasoning/tool item 出现数据丢失、乱序或无法恢复。
- prompt cache 命中异常下降或发生无法解释的失效。
- stream 重试造成重复输出、重复工具调用或状态不一致。
- session resume/fork/compact 出现历史破坏。
- 权限、sandbox、hook/plugin trust 出现安全问题。
- TUI 在终端恢复、输入路由或流式渲染上出现系统性问题。
- 架构边界导致循环依赖、重复实现或上帝模块倾向。

每一项必须关联自动化回归测试；如果暂时无法自动化，需要说明原因和替代验证方法。

## 2. 记录模板

```text
### [P?][YYYY-MM-DD] 简短标题

- 状态：发现 / 修复中 / 已解决 / 接受风险
- 影响版本或提交：
- 现象：
- 触发条件：
- 根因：
- 架构影响：
- 缓存影响：
- 修复方案：
- 未采用方案及原因：
- 回归测试：
- 关联 ADR/Issue/PR：
- 后续行动：
```

## 3. P0 工程与协议基线

### [P0][2026-09-18] Sonic 默认配置不能直接作为缓存 canonical JSON

- 状态：已解决
- 影响版本或提交：初始脚手架
- 现象：Sonic 的默认高性能配置没有启用 map key 排序，相同语义的 map 可能产生不同请求字节。
- 触发条件：Tool Schema、扩展 catalog 或请求结构包含 map，且插入顺序不同。
- 根因：性能优先配置与缓存所需的确定性目标不同。
- 架构影响：所有缓存敏感 JSON 必须通过统一 codec，业务包不得直接调用 `sonic.Marshal`。
- 缓存影响：可能造成 Anthropic/OpenAI 稳定前缀无意义失效。
- 修复方案：统一使用 `sonic.ConfigStd`，并通过 `codec.MarshalStable` 暴露。
- 未采用方案及原因：没有使用标准库 `encoding/json`，因为项目已经选定 Sonic，且 Sonic 提供稳定排序配置。
- 回归测试：`internal/codec/json_test.go`、`internal/context/cache_plan_test.go`。
- 关联 ADR/Issue/PR：ADR-0001。
- 后续行动：所有 Provider request golden test 必须从统一 codec 生成。

## 4. P1 双 Provider Kernel

### [P1][2026-09-18] Resty v3 当前仍是 RC 版本

- 状态：接受风险
- 影响版本或提交：`resty.dev/v3 v3.0.0-rc.4`
- 现象：Go 模块代理当前没有 Resty v3 稳定版，最新为 RC。
- 触发条件：初始化 Go module 并解析 `resty.dev/v3`。
- 根因：Resty v3 尚未发布稳定标签。
- 架构影响：Resty 被限制在 `internal/provider/transport`，避免 RC API 泄漏到 Provider Kernel 和 Runtime。
- 缓存影响：无直接影响；升级可能改变 request/SSE 行为，因此需要重新检查最终 wire 字节。
- 修复方案：固定 `v3.0.0-rc.4`，通过 transport 契约测试保护；稳定版升级单独处理。
- 未采用方案及原因：未回退 Resty v2，因为项目明确要求使用 Resty v3。
- 回归测试：`internal/provider/transport/client_test.go`。
- 关联 ADR/Issue/PR：ADR-0001。
- 后续行动：每次升级 Resty 都运行双 Provider fixture、SSE chunk、重试和缓存 golden suite。

重点关注：SSE chunk 边界、未知 event、opaque 字段、usage 缺失和兼容服务能力降级。

## 5. P2 Session 与 Headless Agent Loop

暂无实际记录。

重点关注：JSONL 尾部损坏、事件顺序、取消时 flush、SQLite 重建和 native history 恢复。

## 6. P3 Coding Tools 与安全执行

暂无实际记录。

重点关注：重复副作用、路径穿越、symlink、并发结果顺序、patch 增量解析和输出截断。

## 7. P4 缓存与上下文强化

暂无实际记录。

重点关注：非确定排序、动态状态污染稳定前缀、TTL 漂移、cache key 变化和 compaction rehydrate 膨胀。

## 8. P5 Claude 风格 TUI

暂无实际记录。

重点关注：高频重绘、终端恢复、按键歧义、overlay 输入泄漏和 snapshot 非确定性。

## 9. P6 扩展系统

暂无实际记录。

重点关注：插件信任、watcher 半文件、skill authority、MCP 故障隔离和 catalog 缓存失效。

## 10. P7 Subagent

暂无实际记录。

重点关注：共享可变 history、并发 slot 泄漏、取消传播、后台 completion pairing 和跨 provider fork。

## 11. P8 发布强化

暂无实际记录。

重点关注：跨平台路径/PTY 差异、迁移、诊断脱敏、磁盘故障和隐式全局缓存。
