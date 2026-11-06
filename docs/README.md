# EasyCode 文档索引

## 当前能力与阅读顺序

先阅读[当前能力清单](roadmap/capability-status.md)了解已实现功能、平台限制和代码/测试证据，再用 [README](../README.md) 查使用方式。Roadmap 的目标与 ADR 的历史决定不自动表示已交付；当前行为契约位于 [OpenSpec 主规格](../openspec/specs/)，进行中的变更位于 [changes](../openspec/changes/)，已归档变更及其验收证据位于 [archive](../openspec/changes/archive/)。本轮核对日期：2026-11-06。

命令与 Skill 首词补全已完成实现、追加验证和归档：交互规则见 [README 补全说明](../README.md#命令与-skill-补全)，行为契约见 [输入补全规格](../openspec/specs/tui/input-completion/spec.md)，测试与文档同步证据见 [归档核对记录](../openspec/changes/archive/2026-11-06-add-command-and-skill-completion/archive.md)。

## 架构

- [总体架构设计](architecture/overall-architecture.md)
- [Tool 分层、批准、Hook 与恢复](architecture/tool-system.md)
- [架构决策记录与模板](architecture/adr/README.md)
- [Provider 原生历史与语义投影](architecture/adr/0002-provider-native-history-and-semantic-projection.md)
- [Session 线程树](architecture/adr/0003-session-thread-tree.md)
- [首版 OpenAI Responses Wire 范围](architecture/adr/0004-openai-responses-first-wire-scope.md)

## 规划与经验

- [产品与工程 Roadmap](roadmap/product-roadmap.md)
- [踩坑与经验记录](roadmap/pitfall-log.md)

## 工程约束

- [根目录 AGENTS.md](../AGENTS.md)
