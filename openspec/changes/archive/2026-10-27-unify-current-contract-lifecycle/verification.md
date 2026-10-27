# 联合验收对照

本变更保留 Session canary 3、双 Provider native revision 1、外部 headless JSONL version 1 和 SQLite 当前索引版本；没有新增迁移、历史 reader、版本化 fixture 或依赖。合法当前 fixture 的固定字节由既有 golden 验证。

## 契约与测试

| 场景 | 自动化依据 |
| --- | --- |
| 替换第三方全局 codec，canonical/checksum/fingerprint 不变 | `internal/codec/global_isolation_test.go` 的隔离子进程测试 |
| 嵌套重复键、大小写、null、代理项、尾随值与缺省语义 | `internal/codec/validation_test.go`、`internal/tool/input_contract_test.go` |
| 非法嵌套 Session payload 在 repair 前失败且 journal bytes 不变 | `internal/session/current_shape_test.go` |
| Provider 自有 usage 严格解码、整体 restore 失败、不改写 payload | 两家 `restore_test.go` 的 `TestRestoreConversationRejectsEntireHistoryOnOneBadCommit` |
| finalize/discard 缺失、零值、重复、竞争与回调完成时机 | `internal/provider/prepared_lifecycle_test.go`、既有 `provider_test.go` |
| durable 失败释放 staging、不推进 revision、不污染下一请求 | Runtime 的 `TestRunTurnToolDurabilityFailuresPreventLaterSideEffects`；两家 `footprint_test.go` 的 `TestDiscardedPreparedSamplePreservesNextRequest` |
| restored/uninterrupted canonical bytes、opaque 顺序与 fingerprint 等价 | 两家 `restore_test.go`、`footprint_test.go` 及 request/cache golden |
| 三宿主共用内核，原输出、退出码与 stream FIFO 保持 | `internal/app`、`internal/headless`、`internal/runtime/agent_loop_test.go`、`internal/tui` 全量回归 |
| 宿主 buffer 满时 detach、唯一 terminal、所有 owner 完成 | `internal/runtime/session_owner_test.go` 的 barrier 回归 |
| Interrupt 不等待背压 admission，待交付中断保留最新目标且不误改 turn | `session_owner_test.go` 的 `TestHostInterruptDoesNotWaitForBlockedAdmission` 与 `TestHostInterruptKeepsNewestTargetWithoutRetargetingOldRequest` |
| 启动取消、admission 拒绝、中断与重复 shutdown | `session_owner_test.go`、`runtime_test.go` 的 ChatSession 生命周期测试及 `agent_loop_test.go` |
| 真实活动 body 阻塞被解除、关闭后不再发请求 | `internal/provider/transport/close_test.go` |
| timeout 保留、真实进程 lease 竞争、terminal 清理后才释放 | `internal/app/host_lease_test.go`、`app_test.go` 的关闭顺序与 escalation 回归 |
| lease 跨实例/跨进程、崩溃释放、占用禁止 repair、seq 与失败字节 | `internal/session/lease_test.go`、`writer_test.go`、`exec_fixture_test.go` |
| Patch grant Sync 后安装、deny 优先、capability 隔离与来源 | `internal/runtime/current_contract_test.go`、`write_test.go`、`patch_runtime_test.go` |
| 三工具 started 接收前零调用、接收后确定执行、健康取消不 poison | `current_contract_test.go`、`write_test.go`、`exec_runtime_test.go` 的 admission 故障注入 |
| 非法执行结果归属原调用、uncertain、poison、剩余调用取消 | `current_contract_test.go`；健康 native pairing 由 `write_test.go` 和 Runtime tool-loop 回归验证 |
| 有序只读并发、独占屏障、Read evidence、文件安全与命令 cleanup | `runtime_test.go`、`write_schedule_test.go`、`internal/tool/builtin`、批准 TUI snapshots |
| Tool 核心 any、JSON 越界、可变全局、旧入口与构造副作用守卫 | `internal/architecture/current_contract_test.go`、`architecture_test.go` |

## 字段策略

- Tool JSON：Read 必需 `file_path`；Glob/Grep 必需 `pattern`；Write 必需 `file_path/content`；Exec 必需 `command`；Anthropic Edit 必需 `file_path/old_string/new_string`。其余已声明参数可省略以采用原默认值，所有显式 null 均失败。OpenAI apply_patch 保留原 freeform 语法。
- Config：七个已声明键均可省略，由现有配置优先级及组合校验决定有效性；空对象、未知键与显式 null 仍失败。
- 外部 stream command：必需 `version/type/request_id`；submit 的 `text` 和 interrupt 的 `expected_turn_id` 由对应强类型 validator 强制有效；没有 nullable 字段。
- Runtime payload 和 Session record：非 omitempty 字段必须存在；标量、对象及集合元素不接受 null。当前 nil slice/map 的编码为 null，按现有契约保留；自定义联合 decoder 明确验证 required 分支字段。
- Provider 自有 commit：非 omitempty 字段必需，kind 决定 input/outputs/assistant/metadata/tool_outputs 分支；usage 的 known/value 关联继续由 Provider validator 验证。原生 item 的自定义 decoder 保留 opaque 扩展，不把 wire 强转成共享 DTO。

## 验证范围

2026-10-27 最终代码的 `make verify` 退出码为 0，完整普通测试与 race 均通过；四份增量规范已逐段同步主 specs，29 份主 specs 严格校验全部通过，本 change 严格校验通过。归档核对完成，全部 25 项任务已完成；四份增量规范的 8 条需求与主 specs 一致。经用户确认，本变更于 2026-10-27 归档。

质量门：`make verify`，包括格式、Go vet、Staticcheck、架构检查、全量测试和 race。规范检查：`openspec validate unify-current-contract-lifecycle --strict` 与 `openspec validate --specs --strict`。

运行平台为 macOS；Linux/Windows 原生运行环境本次不可用，未将交叉编译计作运行验证。共享逻辑、真实子进程 lease 和 Darwin sandbox 路径已纳入本机质量门，其他平台的系统调用路径仍需其原生 CI 验证。构造器 AST 守卫检查项目内可解析函数、显式接收者和字段调用；动态接口与第三方内部行为不声称由静态分析完整证明，由资源边界测试和本地依赖源码复核补充。
