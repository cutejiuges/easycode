## 1. 当前强类型契约

- [x] 1.1 在 `internal/tool` 定义 artifact ID、descriptor、保存状态及 ReadArtifact input/result，补齐纯构造、strict decoder、validator 和深拷贝；单测覆盖状态组合、非法 ID、未知/重复/null 字段、整数范围及可变 getter 隔离。
- [x] 1.2 更新 Exec 冻结候选与 fingerprint，将保存策略和预算纳入审批摘要；测试旧 approval 不匹配新策略、动态使用量不污染 fingerprint，保持现有 sandbox/secret 限制。
- [x] 1.3 定义消费端保存/范围读取小接口及 protocol 的宿主查询值；用架构边界测试证明 builtin 不依赖 session、protocol 不依赖 tool/session/文件系统，New* 不执行 I/O 或创建 goroutine。

## 2. 私有输出存储与配额

- [x] 2.1 新建 `internal/session/artifact` 的显式 Open/Start 生命周期及 descriptor-bound 目录/文件操作；确定性测试 0700/0600、symlink 替换、非普通文件、硬链接和 no-replace 发布，根外 bytes 不变。
- [x] 2.2 实现每流 16 MiB 连续前缀保存、Sync/seal 与状态组合；故障注入验证短写、ENOSPC、file/directory Sync 失败、无输出和 partial，内存与磁盘上限不被突破。
- [x] 2.3 实现 thread/root 预算和每次 32 MiB 原子预留、owner lease 与保守对账；真实子进程测试最后额度竞争、崩溃释放、实存占用保留、损坏 reservation 拒绝及不重复计费。
- [x] 2.4 实现仅在 thread lease 和已验证 replay 后进行的 staging/孤儿回收；测试占用期间禁止清理、损坏 journal 不删文件、另一个 thread bytes 不变、已引用文件永不按年龄回收。
- [x] 2.5 实现同 FD 的长度/digest 校验与有界范围读取；测试文件缺失、同长度篡改、offset 边界、UTF-8 跨页、无效字节、极小 limit、空 EOF 和 partial EOF，返回正文仅来自通过校验的对象。

## 3. Exec 采集与所有权

- [x] 3.0 实现实际目录身份与同一 SandboxPlan 权限范围的存储布局校验，接入启动和命令启动前复核；测试 home workspace、互为祖先、系统读根、路径别名、相似前缀与确定性替换，并以真实 sandbox 验证隔离。

- [x] 3.1 在 `tool/builtin/process_execution.go`、`process_progress.go` 接入首字节双流保存并保持现有头尾采集；集成测试内存省略内容可回读、双流独立计数、磁盘失败仍持续排空，退出码不被保存状态覆盖。
- [x] 3.2 更新 `exec_output.go`、`exec_result.go`、`exec_renderer.go`，一次冻结 preview/保存引用；golden 验证小输出 not_needed、大输出 complete/partial/unavailable、总 preview 不超过 256 KiB，输出引用提示占用同一预算。
- [x] 3.3 将存储资源纳入唯一 process cleanup owner；用可释放屏障注入阻塞 write/Sync，验证 cancel、timeout、shutdown 超时不释放 lease/reservation、不接受新 turn，解除阻塞后唯一 done，无 sleep 概率断言。

## 4. ReadArtifact 与双 Provider

- [x] 4.1 实现当前 thread durable descriptor 授权、ReadArtifact policy/executor/result presenter；测试未知与跨 thread ID 同样拒绝、无 workspace ReadObservation、无递归 offload、错误不回显路径或正文。
- [x] 4.2 为 OpenAI/Anthropic 接入 structured ReadArtifact 的 catalog、request、reducer、native restore 和 output codec；golden 覆盖严格参数、随机 SSE chunk/半包/UTF-8/取消/断线及 call/output pairing，完整链路完成前不暴露 capability。
- [x] 4.3 更新 `context/planning` 与 catalog/cache regression；验证七项稳定排序、两家 schema 估算完整，artifact ID、目录、实时配额、时间与查看动作不改变 stable prefix fingerprint。
- [x] 4.4 接入 parallel_read 与现有 exclusive 屏障；运行异构 ReadArtifact/Exec/Read/Patch/Write 测试，验证并发不超过既有上限、call index 提交顺序、取消及至多一次执行。

## 5. Session 提交、恢复与历史投影

- [x] 5.1 更新 `session/types.go`、codec/draft/replay 的唯一当前 result/input 联合与 canary；strict 测试错误 thread/invocation/stream、长度/digest、保存状态组合和旧开发 shape 在 repair/append 前失败关闭。
- [x] 5.2 在 runtime result 提交边界落实文件/目录 Sync → result Sync → native output Sync，成功提交后才授予引用；逐个边界注入崩溃/Sync 失败，验证无未提交读取、无重复执行、poisoned 资源仍由 owner 清理。
- [x] 5.3 实现从全部有效命令 facts 建立的纯历史投影，不复用尾部 ToolRecovery 充当全历史；单测多 turn 成功/拒绝/取消/不确定结果排序，metadata 有界且不加载对象正文。
- [x] 5.4 扩展 `app/session_service.go` 的恢复装配与授权集合；集成测试持 lease 验证/修复/清理顺序，文件缺失不阻断 native 恢复，ReadArtifact 中断不自动重读。
- [x] 5.5 直接更新仓库 `testdata/current` 固定 bytes，覆盖七工具和 artifact 状态/中断；使用生产 loader/replay 验证且测试运行时不得生成输入 fixture，旧 canary 拒绝与失败 journal bytes 不变有回归断言。
- [x] 5.6 为两家 Provider 对比 uninterrupted/restored 请求 canonical bytes、原生 item 顺序与 fingerprint；覆盖 result 后/native output 前崩溃及对象被删除/篡改，证明恢复不访问 artifact 且原生 reasoning 数据不损失。

## 6. 宿主查询与 Claude 风格详情

- [x] 6.1 在 app 装配 typed 命令列表与范围查询服务，按当前 thread 授权、列表最多 50 项；测试稳定游标、无 Provider 调用/ledger 写入、取消后 owner 可等待完成，避免 render/getter I/O。
- [x] 6.2 更新 `tui/command_detail.go` 与消息处理，Ctrl+O 打开历史列表、↑/↓ 选择、Enter 详情、Tab 双流切换、PgUp/PgDn 分页、Esc 返回/关闭；固定尺寸 snapshot 和按键测试覆盖审批优先级、Ctrl+C 定向取消及 composer 无输入泄漏。
- [x] 6.3 实现 selection generation、独立流游标及最多 4 页缓存；确定性迟到回复测试覆盖切换命令/流、关闭、换 session，resize/View 零 I/O，长行与终端控制序列安全展示。
- [x] 6.4 增加恢复后历史命令端到端测试：查看早期 stdout/stderr、complete/partial/unavailable/corrupt、预览省略与保存缺失分离；验证主线紧凑、内存有界、查询不改变原生请求和 journal。
- [x] 6.5 回归现有 `--print` 与外部 headless JSONL v1；验证无新增内部 descriptor/progress 输出、无交互 Exec 拒绝规则保持。

## 7. 文档与整体质量门

- [x] 7.1 同步总体架构、Tool 专题、Roadmap、pitfall 与存储/投影 ADR，标注本切片完成范围、默认预算、兼容策略及参考实现取舍；核对文档中的真实包/测试路径、当前 canary 和 record vocabulary，不宣称后台或全工具预算已实现。
- [x] 7.2 对照九份 delta specs 逐场景检查实现和测试证据，执行 `openspec validate add-command-output-artifacts --strict`；检查无空 facade、无 any 公共边界、无可变全局和失效兼容代码。
- [x] 7.3 执行 `make verify` 完整质量门（gofmt、vet、Staticcheck、架构检查、全量及 race），确认不访问真实 API/凭据，记录任何平台限制；所有任务验证完成后交付等待独立归档请求。
