## 1. 双 Provider 离线校验与投影

- [x] 1.1 在 `internal/provider` 定义有实际预览消费者的强类型离线来源/投影接口及压缩用量校验接缝；以非法来源、取消、无凭据构造与架构边界测试验证纯内存、无全局 registry、无 `any` 核心协议。
- [x] 1.2 抽取 OpenAI 私有 native restore 与 source 计算纯逻辑，供正式恢复和离线投影共用；用现有 restore/compaction/input/tool fixture 验证 role、InputID、pairing、重复 checkpoint、source/model 错配均失败关闭。
- [x] 1.3 抽取 Anthropic 对应逻辑，保持 Messages 原生 block 语义；用 fixture 验证 thinking/signature/redacted 无损恢复且离线投影不暴露这些内容。
- [x] 1.4 将 app 压缩 usage/provenance 校验依赖收窄到真实 validator，两家离线 reader 与联网 Provider 共享纯验证；通过错误 usage、failed-compaction provenance 与 checkpoint 关联回归证明拒绝规则一致。
- [x] 1.5 增加双 Provider 离线 golden、返回视图独立性与逐条取消测试；比较完整原生恢复后的 ProjectHistory，确认同轮输入身份、顺序、空历史和压缩前历史全部保留。

## 2. 只读会话预览

- [x] 2.1 在 app 实现独立 Preview 路径，复用 Repository/Loader/ReplayPlanner/orderedRestoreRecords 和离线校验；用两家 fixture 断言未调用 resume、writer、Repair、Provider stream、工具或 artifact 清理，且成功发布前 lease 已释放。
- [x] 2.2 实现前 128 条非空消息、32768 bytes 原文、196608 bytes 安全展示预算及 incomplete/truncated 状态；用多字节、边界恰好相等/超一字节、空消息和恶意控制符测试验证 UTF-8、身份和有序前缀。
- [x] 2.3 增加半行、不完整 batch、input-only、ready/started/result-only 工具尾部与手动/自动压缩预览回归；逐字节比较预览前后 journal，验证不生成补偿项及未知/损坏记录整体验证失败。
- [x] 2.4 增加真实子进程 lease 竞争和崩溃释放测试，以及确定性 symlink/路径替换、取消与 Close 失败注入；验证读取不越界、占用期间不 repair、失败不返回成功正文且 owner 最终完成。

## 3. 启动宿主与资源所有权

- [x] 3.1 从现有列表装配提取必要的显式发现生命周期，保留一次性 `sessions list` 输出语义，新增 picker 串行查询 owner；以初始协调取消、切范围、刷新失败、busy/invalid 与全部旧列表回归验证无语义退化。
- [x] 3.2 实现请求 ID、scope generation、preview thread 绑定与仅保留最新待处理意图；使用受控 barrier 证明旧成功/错误不能覆盖新状态，最多一个存储操作，无无界任务排队。
- [x] 3.3 实现 owner admission、取消、幂等关闭与唯一 done；注入输出失败、父 context 取消和关闭超时，验证等待清理、锁外 I/O/等待/发送及没有每次 Wait 的 helper goroutine。
- [x] 3.4 在既有恢复装配中增加 app 私有强类型失败阶段与显式资源接收结果；注入 repair 前拒绝、repair/补偿后装配失败、Sync 不确定和 cleanup 失败，验证返回列表/结束启动分类及合法事实保留。
- [x] 3.5 实现恢复成功与取消竞争下的单次资源交接；通过 admission 前取消、恢复后未交接取消、已交接正常退出测试，断言无 writer/Provider/workspace 泄漏及无重复清理。

## 4. 选择器与单终端交互

- [x] 4.1 实现独立 picker 模型、200 条元数据搜索与字素编辑，包含 256 code point 上限和 decoded paste 规则；单测覆盖 ID 稳定选择、大小写匹配、超限整体拒绝、空结果和 HasMore 提示。
- [x] 4.2 实现上下键/翻屏、Ctrl+A、Ctrl+R、Ctrl+V、Enter、分层 Esc/Ctrl+C；状态测试覆盖加载失败保留旧范围并禁用误恢复、重复确认抑制、预览返回和正文丢弃。
- [x] 4.3 实现安全元数据、兼容性详情、跨目录默认返回确认与有界预览布局；固定时间/时区和尺寸 snapshot 覆盖正常、窄屏、极小屏、无色、中文/emoji、控制字符及完整 ID 可查看。
- [x] 4.4 提取现有 Chat 模型装配接缝并接入单一 Bubble Tea 启动壳；以真实 Program/受控 reader 验证只有一个输入 owner，恢复期间输入不提交、换屏后既有 composer/approval/context/cache/transcript 行为继续工作。
- [x] 4.5 实现加载/预览取消、Resuming 取消及终端退出后的清理结果呈现；通过集成测试验证 Esc 0、Ctrl+C 130、清理失败 1，迟到资源成功由宿主回收。

## 5. CLI 与端到端恢复

- [x] 5.1 新增 `cmd/easycode/resume.go` 路由及帮助，校验参数和 stdin/stdout 终端条件，冻结配置/cwd；CLI 测试验证帮助零资源、非法调用 2、配置失败 1、`--print "resume"` 不被接管。
- [x] 5.2 将 picker 选中 ID 接入既有连续 lease 恢复并阻止不兼容提交，跨目录仅经确认继续；集成测试验证 cwd/当前项目指令、相同 thread identity、零自动模型/工具调用及已有 `--resume`/`--continue`/headless 回归。
- [x] 5.3 增加预览后更新、删除、损坏或替换 journal 的恢复竞态测试；用真实子进程证明 busy 失败 bytes 不变、释放后续写 seq 单调，绝不使用预览缓存作为恢复事实。
- [x] 5.4 完成双 Provider uninterrupted/direct-resume/preview-then-resume 矩阵；比较下一请求 canonical bytes、native item 顺序、内容/策略 fingerprint 与 opaque 数据，覆盖工具和压缩，并保留既有瞬态连接重置语义。

## 6. 文档、质量门与验收

- [x] 6.1 更新 README、架构与 Roadmap 的入口、预览边界、搜索预算和未实现范围，修正 `sessions` 帮助的 repair 误述并补对应帮助回归；检查文档链接和实际 record vocabulary 一致，不宣称整个 P5 完成。
- [x] 6.2 记录本次 owner/预览/失败阶段踩坑与适用的长期架构决策，核对已有 schema-version 过时措辞与单契约基线冲突；验收记录明确本 change 已修正内容及仍需独立收敛的条目，不增加格式迁移或兼容 reader。
- [x] 6.3 执行 `openspec validate add-interactive-session-picker --strict` 和完整 `make verify`；记录格式、vet、Staticcheck、架构守卫、全量与 race 测试结果，任何失败必须修复或明确列出阻塞原因。
- [x] 6.4 在支持平台真实终端完成选择、搜索、预览、跨目录确认、resize、无色、取消与正常退出 smoke；验证终端模式恢复，记录实际平台/终端，无法执行项不得标记通过。
- [x] 6.5 在 change 中形成最终验收记录，逐项映射四份规格场景到真实测试文件/命令，检查参考 revision、scope、依赖方向、无秘密泄漏和无 dead code；确认实现与文档一致后才准备同步 specs 和归档，不提前勾选未验收任务。
