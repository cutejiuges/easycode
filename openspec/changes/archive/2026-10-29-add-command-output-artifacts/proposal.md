## Why

当前前台 Exec 只保留有界头尾和模型预览，长测试日志中间的错误无法追回；重启后的文本投影也不能定位历史命令详情。已有双 Provider、durable 工具循环和命令详情提供了落地基础，本变更让用户与模型按需查看保存输出，同时保持 Claude Code 风格交互和 Provider 原生能力。

## What Changes

- 为前台 Exec 增加私有、受控、按 stdout/stderr 分离的输出保存；区分执行结果、内存截断、保存完整性与显示折叠。
- 增加双 Provider structured `ReadArtifact` / `artifact.read`，只读取当前 thread 已提交引用，不扩大 workspace Read 权限，不产生 Write 观察证据。
- 采用候选默认预算：每流保存 16 MiB、每 thread 256 MiB、每存储根 1 GiB、单次回读原始范围最多 64 KiB；保留现有内存和模型预览上限。数值为设计默认值，待评审，并非性能实测结论。
- 已引用输出默认随会话保留；启动取得 thread lease 后回收该 thread 的暂存与孤儿项。配额耗尽明确报告，不隐式删除历史输出。
- 现有 Ctrl+O 详情支持选择历史命令、按页查看保存输出；用户查看不调用模型、不产生工具调用。恢复后从已验证 Session facts 建立独立命令投影。
- **BREAKING**：扩展唯一当前 Tool input/result、Session payload、命令终态和宿主查询契约；更新当前 Session canary 与固定 fixture，旧开发 shape 失败关闭，不保留兼容 reader。
- 不包含后台进程、PTY/stdin、通用 artifact 浏览器、日志搜索/导出、自动过期、所有工具统一预算或跨 Provider fork；headless 外部 JSONL v1 保持现有输出边界。

## Capabilities

### New Capabilities
- `session/output-artifacts`：安全保存、跨进程配额、提交顺序、完整性、清理与缺失处理。
- `tool/read-artifact`：双 Provider 严格输入、当前 thread 授权、有界回读及恢复。
- `session/command-history`：从已验证 records 投影可分页的历史命令及保存引用。

### Modified Capabilities
- `tool/command-execution`：保存策略进入候选，输出采集与保存状态分离，终态携带只读引用。
- `tool/invocation-loop`：七项可执行 catalog、ReadArtifact 封闭输入/结果、只读调度及冻结输出。
- `session/jsonl-store`：当前 result payload 保存 artifact descriptor，严格校验且不记录输出 delta。
- `session/resume`：恢复命令投影与授权引用，缺失文件不改变 native history。
- `tui/basic-chat`：现有详情支持历史选择、异步分页与准确保存状态。
- `context/planning`：当前完整 catalog 与估算纳入 ReadArtifact，动态 artifact 状态不进入稳定前缀。

## Impact

经评审补充：启动及每次 Exec 启动前必须根据实际目录身份与 sandbox 读写范围检查存储隔离。存储根与 workspace 相同或互为祖先、存储根位于其他 sandbox 可访问目录内或无法验证身份时，以 `unsafe_storage_layout` 拒绝；不得静默禁用保存。

影响 `internal/tool`、`tool/builtin`、`session` 与新增 `session/artifact`、`runtime`、双 Provider、`protocol`、`app`、`tui`、context golden 和架构守卫。存储实现由 app 装配给消费端小接口，禁止 executor 反向依赖 Session；新增构造器纯内存。沿用现有 Sonic codec、Unix descriptor-bound 安全方式和 journal lease；不新增服务、数据库事实源或参考工程构建依赖。

实现时同步总体架构、Tool 专题、Roadmap、pitfall 和 ADR；将本切片置于后台进程控制前，但不宣称 P3 完成。参考 Claude Code 2.1.88 的按需披露与结果保存、Codex `6f8bf683c` 的有界采集和生命周期分层。
