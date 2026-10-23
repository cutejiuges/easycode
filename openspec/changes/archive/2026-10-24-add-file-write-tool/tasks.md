## 1. 固定唯一当前接入边界

- [x] 1.1 对照实施目标分支上的 Tool catalog、approval presentation、exclusive 分派及 Session canary，保留实际已合并的 Exec/会话列表能力；将本 change 的相关 delta 调整为最终唯一当前联合，验证无 with/without-Exec 双分支、无对 `17ad2` 或参考工程的写入。
- [x] 1.2 在 `internal/tool` 增加 WriteInput、invocation/ready 分支及 strict decoder；测试创建、空内容、缺字段/null/重复字段、非法 Unicode、未知字段/尾随 JSON、1 MiB 输入和路径长度/层级边界。
- [x] 1.3 增加 Write operation、candidate、result metadata 与冻结摘要的强类型构造/验证；测试零值、身份/operation 不匹配、getter 深拷贝、已知/不可确定 operation、精确 byte/line count，成功结果不回显正文。
- [x] 1.4 将可共享的文件证据定义集中为唯一当前强类型，调整 Patch 调用点而不改其行为；验证原 Patch input、fingerprint、结果及全部 golden 回归通过，不保留旧 alias/Legacy 类型。

## 2. 完整 Read 观察与 Runtime 生命周期

- [x] 2.1 将 Read executor 的唯一返回契约改为 typed ReadExecution，在同一安全 fd 读取前后采集一致性证据；测试完整/空文件、CRLF 原始摘要、同大小内容变化、inode/权限变化及读失败，确认 preview bytes 与旧 golden 相同。
- [x] 2.2 验证 Read observation 必须匹配 invocation、规范路径与完整 preview；测试 offset、未到 EOF、长行/总预算截断及多片段读取都不产生可用证据。
- [x] 2.3 在 Runtime 专属文件实现固定 call slot 暂存与 outputs Sync/finalize 后激活，表上限 1024 且按提交顺序淘汰；使用可控 journal/finalizer 验证 result durable 但 output 失败时不激活，同 sample Read->Write 不获得新授权。
- [x] 2.4 实现路径 generation 与既有/待生效观察失效；测试 Read->Patch、Patch->Read、Write started、Write conflict、失败/截断的新 Read、跨 turn 和 resume 清空，若 Exec 已集成则测试其 started 清空全部观察。

## 3. 候选与精确审批展示

- [x] 3.1 实现只读 WritePreparer，区分 create/overwrite、绑定已有祖先与目标 fd evidence；测试不存在、空文件、权限错误、目录/FIFO/设备、symlink、缺失与过期 Read，断言 prepare 零 mkdir/临时文件/写入。
- [x] 3.2 生成确定性目录清单与权限计划，限制总路径 64 组件及缺失目录 16 层；测试规范路径、祖先 identity 变化、create/overwrite 不转换和相同候选 fingerprint 稳定性。
- [x] 3.3 复用或提取纯内存文本 diff 逻辑，提供 create 完整内容/空文件说明和 overwrite 完整 diff；测试 CRLF、BOM、无末尾换行、相同内容 write_no_changes、256 KiB 展示边界，不用 Patch replacement 伪装 Write。
- [x] 3.4 实现审批专用可见转义和转义后预算检查；固定 golden 覆盖 ESC、CR、双向控制符、长行与中文，断言实际 final bytes 不变且原 Patch 展示回归不退化。

## 4. 平台提交与唯一暂存 owner

- [x] 4.1 实现同目录私有暂存文件 owner、完整写入、基本 mode 保留、文件 Sync 和 fd 清理；故障注入覆盖短写、Chmod/Sync/Close 失败及入口取消，验证无半文件覆盖或无主 goroutine。
- [x] 4.2 实现 Darwin 禁止覆盖发布 adapter；真实文件系统测试普通目标与目录的 no-replace、已存在空文件/空目录/symlink 冲突及 unsupported 注入，断言绝无普通 rename/直接写入降级。
- [x] 4.3 实现 Linux 禁止覆盖发布及 unsupported 平台 adapter；在真实 Linux 环境运行等价发布测试，验证其他平台编译和失败关闭；无法运行的检查记录原因，不以交叉编译冒充运行验证。
- [x] 4.4 实现缺失目录链的私有暂存树，逐级安全创建、叶到根 Sync 和首个缺失组件的一次发布；测试 src/new/pkg/a.go 成功、发布前正式链不存在、首个组件被抢占时保留竞争者内容。
- [x] 4.5 实现 overwrite 重开及最终基线复验、同目录原子替换；确定性测试审批后内容/inode/mode 变化、目标删除、leaf/ancestor symlink 替换、0755 保留及外部目标 bytes 不变。
- [x] 4.6 实现发布前后取消与错误分类：确定未发布、成功发布、无法确认三分；测试发布前取消、发布后取消但 Sync 成功、目录 Sync 失败及模拟“实际提交但返回 I/O 错误”，验证结果不谎报未修改且不自动重试。
- [x] 4.7 实现按 owned entry 清单的句柄相对清理，不递归删除未知子项；注入临时身份替换、未知项和 unlink/rmdir/Close 失败，验证结果 uncertain、资源最终关闭、未知对象保留。

## 5. Catalog 与双 Provider 原生闭环

- [x] 5.1 将完整实现的 structured Write 纳入两家唯一当前 catalog，保持 custom apply_patch grammar；测试定义排序、schema strictness、缺真实组件时拒绝构造，以及目标状态/观察/grant 不污染 catalog fingerprint。
- [x] 5.2 接入 Anthropic Write tool_use 参数归并、strict decode、native commit 和 tool_result；新增 request/continuation golden 及随机 chunk、半包、UTF-8、取消/EOF、无效/超限输入零副作用测试。
- [x] 5.3 接入 OpenAI Write function call/output，与 custom apply_patch 共存；新增同等 request/stream/golden 测试，并覆盖 function/custom 名称、wire kind、call ID 错配拒绝。
- [x] 5.4 补齐两家 Write native persistence/restore 和混合调用回归；逐字节比较不中断/恢复的 canonical request、native item 顺序与 fingerprint，并验证 thinking/signature/encrypted content/phase 无损。
- [x] 5.5 更新 context/tool-catalog 估算与 cache regression；验证仅真实 Write 定义改变稳定前缀，修改文件内容、cwd、目录 evidence、批准状态或 Read observation 不改变稳定 catalog segment。

## 6. 审批、调度与 Session 事实

- [x] 6.1 扩展 Tool Policy 和独立 Write session grant；测试默认 ask、Patch/Write 互不授权、allow fact 未 durable 不安装 grant、headless 拒绝、过期/重复 response 和拒绝反馈上限。
- [x] 6.2 在 Runtime 独立文件接入 Write candidate/approval/started/executor/result；测试所有 durability 边界故障、accepted started 恰好调用一次、发布成功后的取消保留 success、uncertain 阻止后续 sibling。
- [x] 6.3 将 Write 纳入实际当前 exclusive 调度；测试 Read/Write/Patch/Grep 混合顺序、有界只读并行、拒绝/冲突不取消独立 sibling、结果按 call index 提交，已集成 Exec 时增加混合屏障场景。
- [x] 6.4 扩展当前 Session ready/result 联合、strict decoder 和 replay 状态机；测试完整 Write 参数保存、结果不含正文、错误 metadata/operation/wire 拒绝、无批准 started 拒绝、准备 conflict 合法及 no-op error 零 started。
- [x] 6.5 更新唯一当前 canary 和仓库固定 fixture，覆盖 create/overwrite/denied、ready/allow/started/result 尾部及混合顺序；测试直接读取固定 bytes、旧 shape 失败关闭、非法状态 journal bytes 不变，不在测试运行时生成 fixture。
- [x] 6.6 接入恢复补偿并验证 workspace/Provider 零调用：ready 无批准、allow 无 started、started 无 result、result 无 output 均产生预期配对输出；测试不恢复 grant/Read 证据、不扫描旧暂存项。
- [x] 6.7 覆盖单输入及多调用聚合 record/request 预算，验证超限在候选读取/目录创建前失败且现有 16 MiB 上限不被放宽；沿用真实子进程 Session lease、崩溃释放、seq 和 repair 禁止测试。

## 7. 协议、TUI 与应用装配

- [x] 7.1 为 Patch/Write-create/Write-overwrite presentation 提供专属 typed constructor、strict decoder 与 validator，并由 Runtime 映射；测试错误 capability、混合/缺失字段、空文件 presentation 和 decisions，若已有 command 分支则完整保留。
- [x] 7.2 实现 Create file / Overwrite file overlay、完整内容/diff、目录清单及 Write 专属 session allow 标签；固定尺寸 snapshot 测试覆盖正常/窄屏/无色、中文、空文件、控制字符与滚动，渲染禁止 I/O。
- [x] 7.3 验证输入路由：默认 No、deny feedback、重复提交、Esc/interrupt、resize、草稿保留和 overlay 关闭；测试 approval resolved 不被显示成写入成功。
- [x] 7.4 在 app 用同一 Workspace 显式装配 Read/Write 组件与 owner；测试纯 New 零 I/O、配置失败零资源泄漏、shutdown 不提前释放仍被使用的 Workspace/Session。

## 8. 纵向验收、文档与质量门

- [x] 8.1 双 Provider 本地 mock E2E 验证创建新文件/新目录链、完整 Read 后覆盖、再 Read 验证内容及最终总结；同时覆盖 no-op、被拒绝、截断 Read 和审批后外部修改，不使用真实 API key/外网。
- [x] 8.2 新增 headless E2E 测试覆盖 --print、--json 和 stream-json 的 Write approval_required 及新目录零副作用；保留既有输出版本、call/output 配对和正常最终文本行为。
- [x] 8.3 补齐文件发布边界的可控进程崩溃验收，分别验证发布前/后 target 状态、started 补偿与 resume 零重放；允许明确记载的私有暂存残留，不用 sleep/概率竞态证明正确性。
- [x] 8.4 更新 README、Tool/总体架构、Roadmap 和 pitfall：记录 Read 预算约束、同 sample 不能预授权、no-replace 创建、权限范围与崩溃残留；逐项核对实际测试路径、record vocabulary 和在途能力状态。
- [x] 8.5 执行 `openspec validate add-file-write-tool --strict --no-interactive`、`git diff --check` 与 `make verify`，核对 AGENTS 的依赖/生命周期/无兼容分支约束；平台检查无法执行时列出原因和剩余风险，不声称全部通过。
- [x] 8.6 实现验收后核对最终 delta 与主 specs 的一致性，再按归档流程同步并归档；确认没有未完成任务、失效引用或将本轮规划误记成已实现能力。
