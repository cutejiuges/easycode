## 1. 声明、输入和执行契约

- [x] 1.1 新增 PostCompact 声明、共享压缩匹配与严格冻结配置。扩展现有来源测试，验证字面触发、user/project 顺序、*、共享 ID/数量、键顺序不影响 revision，以及不接受 `feedback:model`。
- [x] 1.2 增加绑定真实 compaction scope 的 typed summary stdin 和 run identity。核对手动与自动的严格字段、Unicode 原字节、未包装语义摘要和转义后编码上限，验证前后置事件不能交叉使用 identity 或输入。
- [x] 1.3 接入现有 runner、受限进程 owner、字节限额与事件专属校验。定向检查 exit 0 输出无控制或异步效果、exit 2 的退出失败分类、清理优先级，以及拒绝超双流限额的伪造成功和控制/反馈结果。

## 2. 持久化与回放

- [x] 2.1 在 `compaction_completed` 冻结必需计划联合并校验完整单记录编码预算。验证仅计划超限时保存同一 checkpoint 的安全跳过。接入 started/finished/closed 构造与严格解码，替换受影响的固定 `testdata/current` bytes 与 checksum。检查缺字段及校验和合法的多记录成功行后接半行能在 repair 前拒绝。
- [x] 2.2 扩展 compaction replay，保存确定 checkpoint 序号和独立后置前缀。用固定 fixture 和定向负例验证 checkpoint 内容摘要绑定、事件、ordinal、有序前缀及唯一关闭，覆盖恢复与元数据回放。验证 planned 尾部未关闭时拒绝后续记录、每项自动事实推进真实 turn 尾序号。
- [x] 2.3 在现有恢复与同一 exclusive lease 下先关闭后置尾部，再进入 Stop/turn 收口。验证无 started 为中断、孤立 started 为未知、全部确定 finished 按真实事实结算及已闭合不重写；注入关闭 Sync 失败，验证离线预览只读、原 bytes 不改写、零信任/命令/网络和重复恢复幂等。

## 3. 压缩 owner 与宿主

- [x] 3.1 在共享 `runCompactionOwned` 候选校验后冻结计划，使用可观测屏障验证 checkpoint Sync、finalize、摘要投影、已安装阶段以及全部授权后才执行。复用信任 wait 与 `runTimedHook`，在一次握手回退中验证只为最终安装的 checkpoint 审查一次；预期拒绝与宿主安装失败均零启动。
- [x] 3.2 分开 checkpoint 提交状态、后置关闭和 worker 错误。确定性注入 Sync 前取消、started 已接收后取消、最后一个 finished 与取消竞争、预算耗尽、后置写入及清理不确定。验证真实 finished 不改写，未接纳后缀零运行，已安装成功仍只发一次 completed，owner 与 lease 保持到清理收口。
- [x] 3.3 复用 AgentLoop 关联和定向取消。授权等待时用宿主 typed query 验证新 Context revision、`LastSummary` 与原最近采样基线，旧事件不能安装或授权。普通检查失败后从新窗口继续；分别验证自动初始输入、工具续轮和 Stop 待交付遇到后置 poison 时为非空 Session 失败，零后续普通提交、请求、Stop live 交付、`compaction_failed` 与失败计数。
- [x] 3.4 接入 /hooks 与 TUI 本地相位，核对拒绝默认项、窄屏与 NO_COLOR、过期事件、无 plan 的交互跳过、取消时接纳真正关闭并合入完成状态。复用现有 modal 与代表性 TUI/PTY 用例验证草稿、焦点和摘要锚点。
- [x] 3.5 对 headless stream-json v1 与一次性模式复用原输出，验证匹配的 PostCompact 仍成功安装，只有原有相关压缩帧/普通任务投影，零 PostCompact 信任、执行、正文及新增字段。

## 4. 纵向验收与交付

- [x] 4.1 复用双 Provider 压缩 mock 和 cache 测试，以同 checkpoint、冻结输入和有效模型配置比较无匹配、匹配执行及恢复后的第一普通 canonical 请求、原生顺序与稳定 fingerprint。用独立摘要、任务包装及 opaque 哨兵验证 Hook stdin 只取摘要、原生证据和摘要计量保持；脚本 stdout/stderr 哨兵不进入 Provider/日志/Session/界面。
- [x] 4.2 在 Darwin 用真实只读禁网 runner 和 PTY 烟测手动路径、授权/默认拒绝和检查失败或取消，复用现有 PreCompact 启动工具；用已有平台规则验证 Linux/Windows 的 owner 构建、测试编译及 unsupported 路径，明确真实运行范围。
- [x] 4.3 同步 README、能力大纲、路线图和架构。配置示例展示脚本读取完整 JSON stdin 并用退出状态报结果。对照实际 Session 词汇、源码路径、固定 fixture 与平台范围，确认文档及主规格的完成状态真实。
- [x] 4.4 运行 `make verify` 与 OpenSpec strict 验证。仅在存在新增高风险覆盖缺口时定向扩展 `scripts/race_policy.json`，记录独立检出的故障和实测耗时。验收记录仅保留结果、平台范围和未验证项。
