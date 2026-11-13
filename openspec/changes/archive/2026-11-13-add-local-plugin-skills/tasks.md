## 1. 当前引用与贡献值

- [x] 1.1 在 domain 增加区分裸名与 plugin:leaf 的纯内存引用解析，保留文件 leaf 校验；用边界单测证明 64/129 bytes、一层冒号、非法字符和 namespace 归属规则。
- [x] 1.2 扩展 SkillSource、SkillMetadata、SkillAttachment 与 SkillInput/Result 的当前校验及严格解码；断言非法 scope/kind/name/revision 组合在构造、嵌套解码和结果校验中全部拒绝，原 local 固定值继续合法。
- [x] 1.3 增加纯内存插件展示值、状态和安全诊断；用单测证明 getter/clone 不共享 slice、宿主路径不进入投影、非法状态/计数不能构造。
- [x] 1.4 将 extension/plugin 占位替换为本轮真实消费的 typed 技能贡献和受控来源 handle，移除无消费者未来字段/旧 Importer；以编译和架构测试确认无 any、新全局 registry、裸 FD 生命周期旁路、隐藏 I/O 构造或反向依赖。

## 2. 描述符读取与私有存储隔离

- [x] 2.1 在 extension/localfile 增加相对于已打开目录 FD 的安全打开与有界读取，复用现有 no-follow/type/Unicode 策略；用每段替换注入证明没有检查后再按未绑定路径打开的窗口，所有错误/取消路径关闭 owned FD。
- [x] 2.2 在 artifact 存储边界提供针对实际来源目录 FD 的身份/祖先隔离检查，由 app adapter 注入插件；以同目录、互相包含、相似字符串路径和确定性替换测试证明私有存储不能作为技能读取根。
- [x] 2.3 为上述来源读取提供等价的 Darwin/Linux adapter 和明确的不支持分支；用平台原生来源测试或可执行的编译检查验证失败关闭，报告未原生执行的平台，禁止普通 ReadFile fallback。
- [x] 2.4 实现最多 32 个 pinned 根与 16 个在途借用的来源关闭 owner、受控 handle、首次 Close 唯一 cleanup worker、cancel/强制关闭与固定 drained/Done；确定性阻塞 Prepare/模型读取/文件关闭，验证等待超时不丢所有权、重复 Close 不新建 worker、超限无读取、真实释放后才完成，锁内无 I/O 或等待。

## 3. 受限 manifest 导入

- [x] 3.1 实现共用严格 metadata 校验和四个小格式 adapter；用固定 JSON 验证 name/metadata 类型与长度、深度 16、64 KiB、重复键/null/Unicode/未知字段和未支持贡献错误，未产生网络或进程访问。
- [x] 3.2 实现固定 manifest 优先级、无歧义 ID 提取与根 Agent Plugins schema 识别；以同时存在、全部缺失、根 schema 不相关、高优先损坏/不安全及合法 ID 带未支持字段的 fixture 断言不回退、不猜名，并保留完整冲突判定资格。
- [x] 3.3 实现默认/显式技能根规范化、排序、去重与最多 8 项限制；断言显式值替代默认根，绝对路径、..、反斜杠、非规范路径和空数组均拒绝。
- [x] 3.4 增加 EasyCode、Claude、Codex JSON 和 Agent Plugins 四份固定最小包、各自支持的布局示例与 Agent Plugins 单技能根拒绝 fixture；由导入测试实际读取并比较规范化贡献，禁止在测试运行时生成期望 manifest bytes。
- [x] 3.5 实现 codex/agentplugins 的 agents/openai.yaml 严格子集及更严格策略合并；固定 fixture 测试 false 隐藏/拒绝模型主文档、true 不覆盖已有禁止、interface 注解不执行、dependencies/products/非法 YAML 失败关闭及 64 KiB/16 层限制。

## 4. 统一 Skill 发现与读取

- [x] 4.1 让 skill.Local.Load 消费本地根与 typed 插件贡献，共用 frontmatter/parser/展开逻辑；以四个 importer 的布局/命名对照证明 Claude 目录调用名、Codex frontmatter 调用名、Agent Plugins DirectChildren 及严格不回退规则。
- [x] 4.2 实现实际目录/主文档共同去重、重复插件 ID 全禁用和限定名冲突隔离；交换 CLI/枚举顺序及提供不同资源根的硬链接文档，断言输出一致且不误合并资源来源，本地裸名不被遮蔽。
- [x] 4.3 实现受控来源 opener、原根/manifest/主文档/sidecar 存在性与 digest 检查及复合附件 revision，分离原始 document digest；以增加/删除/修改副文件、路径替换、资源变化和搬迁测试证明新读取正确拒绝或保持等价，queued 附件不重读。
- [x] 4.4 原子发布合并快照并执行 root/插件/全局原始枚举、技能候选、主文档加副文件读取、诊断及 256 项/64 KiB 额度；临界值/超限测试证明最多读取枚举上限加一，普通文件/无效项也计费、插件整组拒绝、本地项保留、取消不发布部分结果。
- [x] 4.5 统一限定显式语法、模型 Skill 与同来源资源读取，并始终使用冻结有效调用策略；以 manual-only/model-only、sidecar false、主文档重解析、未 durable/旧 turn/跨插件/复合 revision 变化回归证明不会重新启用模型调用或扩大资源资格。

## 5. Runtime 与 app 生命周期装配

- [x] 5.1 增加启动前恰好一次的纯内存 Skill 快照安装，替换当前 Config 中 catalog/preparer 的旧初始化入口并更新调用者；单测证明重复、已启动、活动和 poisoned 状态不能安装，构造及安装无 I/O。
- [x] 5.2 将 local/plugin 来源加载移至旧 turn reconciliation 后、首次工作启动前，注入同一来源 owner 和存储边界；以顺序检查点证明恢复零旧来源读取，正常/失败/超时关闭都在来源 Done 后释放 artifact.Layout，包括未进入 admission 的显式准备。
- [x] 5.3 从同一发布结果向模型、/skills、/plugins 与 completion 装配冻结值；集成测试断言每来源只发现一次，修改磁盘后各投影仍一致，来源取消/失败均完成清理。
- [x] 5.4 在所有 manifest ID/冲突确定后有序扫描，最终 Skill 纳入结束再生成插件状态/计数及 namespace 可用性；测试失败插件即使没有成功贡献也返回 skill_unavailable，合法 empty/未知项保持 not_found，合法 manifest 被 sidecar、冲突或目录额度拒绝时，/plugins、/skills、补全和模型快照保持一致，原始输入顺序不改变结果。

## 6. CLI 与 headless 入口

- [x] 6.1 在两个 CLI 启动入口共用有界 plugin-dir 参数收集并经 app.Options 传递；CLI 测试覆盖重复参数、空值、32 项/4096 bytes、非法 UTF-8、flag 帮助和资源创建前的 exit 2。
- [x] 6.2 接通普通 TUI/print/json/stream-json、--resume/--continue 的明确插件目录；以本地 fixture server 验证显式/模型调用都消费同一来源，stdout 不混入新诊断帧或插件控制协议。
- [x] 6.3 接通 resume picker 的参数传递并固定 launch cwd 解析，保护 version/help/sessions list/预览/取消；通过读取计数和跨目录 picker 测试证明只在选择接收且旧事实收口后 Load，未创建安装状态。
- [x] 6.4 避免 flag.Value.Set 的标准错误回显私有参数，确保 String/帮助不泄漏已配置路径，应用入口同样预检；捕获超长绝对路径、非法 UTF-8 与重复参数错误输出，断言仅有安全 code 且 Session/Provider 零访问。

## 7. 插件浏览与补全

- [x] 7.1 增加 /plugins 冻结列表和详情，显示 Skills only、真实状态/计数及安全诊断；以空/合法/partial/conflict/invalid 固定快照证明导航和 render 无文件、Provider 或 Session 操作。
- [x] 7.2 接通插件详情选择并复用 composer 行为，明确手动命令与补全各自的打开前快照；测试 Enter 只填入限定调用、manual-only 可选、禁用项不可选，Esc 恢复真实入口的命令正文/光标且保留 resize 后布局。
- [x] 7.3 扩展首词补全为第六个 builtin 和限定名称/编辑前缀，保持总额及现有排序/接受规则；测试 qa: 前缀、完整 129 bytes、第二个冒号、命令 plugins 冲突、参数保留和 4 MiB 原子拒绝。
- [x] 7.4 统一 Plugins panel 的焦点、paste、活动 turn/Ctrl+C、resize 和 NO_COLOR；用 80x24、28x10、极小尺寸与恶意 metadata 的固定 golden 证明没有不可见候选抢键或终端控制序列。
- [x] 7.5 让 /skills 与插件详情共用保留名调用文字 helper，并把 /plugins 加入现有补全浏览器草稿恢复路径；回归 plugins/cache 等裸名填 $name、/plug Enter 后取消恢复原文本、当前布局不回滚及参数错误不提交。

## 8. 双 Provider、Session 与缓存回归

- [x] 8.1 扩展两家现有 Skill native/request golden 和当前嵌套来源校验，不导入插件 wire 或增加模型工具；断言原生 call/output kind、输入关联、opaque thinking/encrypted content 和顺序保持。
- [x] 8.2 新增仓库内固定 plugin current JSONL/native fixtures，覆盖显式附件、模型结果和资源结果；通过当前 decoder/replay 测试断言直接读取固定 bytes、拒绝伪造 source/name/revision 关联，不引入版本或历史 reader。
- [x] 8.3 覆盖 ready/started/result/output 各提交前缀与删除来源恢复；本地集成断言零旧插件读取/网络/执行，按原顺序补齐 output，失败 journal bytes 不变，并保留现有真实子进程 lease 竞争/崩溃/repair 质量门。
- [x] 8.4 验证同目录快照的 uninterrupted/restored 请求、footprint 和各级 fingerprint；另测移除插件仅改变未来目录前缀而历史附件 bytes 不变，OpenAI full/incremental wire 分别验证，不混淆实际帧与完整逻辑请求。
- [x] 8.5 增加 source 搬迁、参数/枚举乱序、正文/sidecar 注解/有效策略分别变化及只浏览/补全的缓存回归和 Sync 失败/poisoned 故障用例；断言有效 metadata 精确失效、未调用正文/digest 不污染前缀、未 durable 输入不提交。
- [x] 8.6 用固定 current bytes 验证有效副文件策略只通过冻结附件/结果体现，恢复不读取副文件或重算来源摘要；覆盖副文件增加/删除/修改后的历史保真，以及明确资源读取副文件的普通结果不生成新记录或授权。

## 9. 系统验收与架构门禁

- [x] 9.1 用四种最小包完成真实 CLI 与本地 SSE fixture server 的两家端到端验收，覆盖显式调用、模型调用、辅助资源、无效来源隔离和关闭后 resume；验证模型实际收到正确指令/结果并输出预期终态，保存请求/安全输出证据，禁止真实 key 或外网。
- [x] 9.2 增加并运行 scripts/plugins-smoke.py，覆盖 /plugins -> 详情 -> 选择 -> 参数提交、/skills/补全、Esc/paste/Ctrl+C、NO_COLOR、窄屏及终端模式恢复；以事件/窗口握手驱动，不用固定 sleep 证明正确。
- [x] 9.3 增加插件职责/依赖/构造纯度检查，并运行相关架构及已有 Skills/Hooks/Question 回归；证明无 Provider/Session/TUI 反向依赖、隐藏 goroutine、全局 registry、空贡献 schema 或无用依赖。
- [x] 9.4 对全部新增源码和测试完成 AGENTS/OpenSpec 契约核对；用依赖、record vocabulary、唯一当前 shape、来源与权限分离检查结果证明实现和五份 delta 一致，发现设计不成立先修订 artifacts。
- [x] 9.5 逐项执行 design 二次自检 R1–R9 的确定性反例回归，尤其关闭先于显式 admission、manifest 非法但 ID 冲突、硬链接资源根与副文件状态变化；保存验证映射，确认没有通过忽略副文件、放宽来源或删除场景消除失败。

## 10. 文档与最终交付

- [x] 10.1 更新 README、capability-status、总体架构、Roadmap 与 pitfall，补充受限 importer 字段表、可运行示例、冲突/失败语义及平台边界；逐条核对真实命令、来源规则和测试路径，不把剩余 P6/P7 能力写成已实现。
- [x] 10.2 完成验收后按既有流程同步本 change 的五项主 specs，保留其他 change；严格校验同步后规格并核对实际 record/fixture vocabulary，不在实现未完成时归档或宣称 P6 已退出。
- [x] 10.3 执行最终 make verify、严格 OpenSpec 校验、分支名检查和工作区差异检查，记录真实平台/结果及未执行范围；交付报告明确新功能验收证据与本方案阶段基线的区别，所有 tasks 只在对应验证通过后勾选。
