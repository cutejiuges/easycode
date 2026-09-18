# Architecture Decision Records

本目录记录已经作出的重要架构决策。ADR 一经接受不直接改写结论；后续发生变化时新增 ADR，并将旧记录标记为 superseded。

## 命名

```text
NNNN-short-decision-title.md
```

例如：`0001-use-go-resty-sonic-and-bubble-tea.md`。

## 已接受记录

- [ADR-0001：使用 Go、Resty v3、Sonic 和 Bubble Tea](0001-use-go-resty-sonic-and-bubble-tea.md)
- [ADR-0002：保留 Provider 原生历史并提供单向语义投影](0002-provider-native-history-and-semantic-projection.md)
- [ADR-0003：使用 Session 线程树组织会话](0003-session-thread-tree.md)
- [ADR-0004：首版 OpenAI Wire 仅支持 Responses](0004-openai-responses-first-wire-scope.md)

## 模板

```text
# ADR-NNNN：决策标题

- 状态：proposed / accepted / deprecated / superseded
- 日期：YYYY-MM-DD
- Roadmap 阶段：P0-P8
- 替代：可选，填写被替代或替代本记录的 ADR

## 背景

## 约束

## 候选方案

## 最终决策

## 代价与风险

## Provider 与缓存影响

## Session/协议迁移影响

## 验证与回归测试

## 回滚或替代方案
```

## 必须创建 ADR 的场景

- 改变 Provider Kernel、native history 或 RuntimeEvent 边界。
- 改变 cache segment、canonical serialization 或失效策略。
- 改变 session/protocol schema 或事实源。
- 引入新的顶层 package、跨进程服务或反转现有依赖方向。
- 改变工具安全、sandbox、hook/plugin trust 模型。
- 改变首版支持的 provider wire 或跨 provider 恢复策略。

ADR 只记录已经接受且需要长期保留理由的架构决策，不承担需求、任务和实施进度管理。所有契约变更必须先通过 OpenSpec 的 `explore -> propose -> review/confirm -> apply -> verify -> archive` 流程；相关 OpenSpec change 应在 proposal/design 中引用 ADR，ADR 也应回链对应 change。流程细则见仓库根目录 `AGENTS.md`。
