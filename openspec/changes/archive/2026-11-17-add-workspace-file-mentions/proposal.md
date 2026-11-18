## Why

当前 EasyCode 已具备九项工具、安全工作区读取、多行 composer、命令/Skill 补全及冻结输入附件，但用户仍需手写路径并等待模型调用 Read，才能围绕具体文件展开讨论。文件 `@` 引用可以复用这些已验证接缝，直接改善日常编码交互；相较尚需动态工具契约和服务生命周期的 MCP、线程树和预算调度的 Subagent，它更适合作为当前阶段的完整纵向切片。

## What Changes

- 在交互式正文光标处提供工作区文件 `@` 补全，支持手输引用、带空格的引号路径及 `#L10-20` 行范围；Tab/Enter 选择只填入并把光标移出完整引用，后续独立提交才读取内容。
- 由宿主持有有界、可取消、仅在文件候选打开期间存活的文件名索引；同次候选交互中更新查询不重启目录发现，渐进结果绑定索引 revision、查询代次与编辑目标，旧列表在新查询完成前不可接受。
- 候选发现遵循工作区内分层 `.gitignore`、`.ignore`、`.rgignore` 样式规则，区分候选过滤与读取权限；不读取工作区外/全局规则或 Git index，手输被过滤的合法路径仍可明确引用。
- 提交前冻结强类型文件附件，与用户原文、既有 Skill 附件共同接受输入、编码、上下文及队列预算检查；排队后不重读，取回后重新提交才重新准备。
- 与 main 已实现的 `/effort` 共用操作捕获、七项内置命令判断和 TUI 行数预算：内容冻结不固定未来推理策略，strict-next 在启动时取最新选择，同轮追加/工具/Stop/自动摘要保持当前捕获值；恢复等价须显式重选相同设置。
- 双 Provider 分别保存原文、可选 Skill 和有序文件文本块，保留原始文件换行、严格核验完整引用集合与冻结正文；历史召回保留原文，主线显示有界摘要，长路径通过明确省略维持摘要额度，Ctrl+O 可分页阅读附件。
- **BREAKING**：直接更新唯一当前 `user_inputs.input_metadata`，必需 `files` 数组表示本次文件附件，无附件为 `[]`；替换固定 current fixtures，缺失字段的旧开发 shape 失败关闭，不增加版本门禁、兼容 reader 或迁移。
- 保持普通 headless/stream-json 文本中的 `@` 为原文；已保存的交互式附件可由这些入口正常恢复。首版只处理当前工作区内普通 UTF-8 文件，不包含目录附件、图片/PDF、外部路径、自动 rehydrate 或文件 watcher。

## Capabilities

### New Capabilities

- `context/file-mentions`：显式文件引用语法、工作区权限、保真文件切片、附件准备/预算/确定性包装及宿主读取生命周期。

### Modified Capabilities

- `tui/input-completion`：区分首词命令/Skill 与正文光标处文件目标，增加分层过滤、有界索引、异步路径搜索、选择和焦点契约。
- `runtime/conversational-input`：typed 文件输入草稿与冻结附件进入既有 admission、FIFO、安全点、取回和 durable resolution。
- `provider/native-history-persistence`：唯一当前文件附件元数据、双 Provider 原生文本块核验、零来源读取恢复与续写等价。
- `provider/history-projection`：文件附件独立于原始用户正文投影，支持只读详情、有效窗口和深拷贝。
- `context/planning`：文件附件参与候选预检、有效窗口估算与历史尾部 fingerprint，稳定目录和前缀不受文件搜索影响。

## Impact

涉及 `internal/domain`、`internal/protocol`、`internal/context`、`internal/tool/builtin`、`internal/runtime`、两家 Provider、`internal/app`、`internal/tui`，以及对应固定 fixtures、golden、集成/PTY 和架构测试；Session 外层 record vocabulary、SQLite 索引与外部 headless JSONL 不扩展。实现时同步 README、能力清单、总体架构、Roadmap 与实际发现的踩坑记录。

参考仅限本地只读快照：Claude Code 2.1.88（参考仓库 `a8a678cb6244e6770e1e421767ff0987a1d95549`）的文件建议、引用/行范围和附件交互；Codex `c0cc9fc820f62e4dbdde68214dd6d792d6c8a7ee` 的光标目标、搜索 owner、过期结果和显示/插入路径分离。具体源码、差异和取舍见 design.md；不增加参考工程依赖，不复制全局缓存或宽松文件读取。

2026-11-15 已基于两个相同参考快照进行第二轮方案复审，2026-11-16 在合并 main 至 b0387c4 后复核 effort、恢复、缓存和 TUI 组合边界；用户随后明确确认方案并触发 apply。实现与验证状态以 tasks.md 和 verification.md 为准，发现与处理记录见 design.md。effort 保持纯进程选择，不因文件附件增加设置 record 或恢复推断；本次 apply 完成后保留待归档，不自行提交或发布 PR。
