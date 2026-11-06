## 1. 候选、目标和编辑计划

- [x] 1.1 实现同包私有强类型 candidate/identity、五项内置描述及 WithSkills 冻结候选装配；单测验证 available/UserInvocable、manual-only、invalid/shadowed/model-only、来源一致性、内置同名优先及 `$` 可达，并确认无 I/O 和包级可变 registry。
- [x] 1.2 实现首个非空白 token 的 sigil、64 字节名称范围与光标左片段查询；表驱动测试覆盖裸 sigil、前导 Unicode 空白、中间光标、参数区、多行/代码块、绝对路径、不合法字符和 UTF-8 边界。
- [x] 1.3 实现四级确定性匹配、稳定排序、身份选择保留与循环滚动；测试打乱输入枚举顺序仍得到相同候选，精确/前缀/子序列/描述与空查询次序均符合规格。
- [x] 1.4 通过既有 composer.replace 实现有界替换计划；测试完整 token 替换、已有参数及首尾空白不变、分隔符只补一次、hint 不写正文、4 MiB 恰好/超限原子失败以及合法字素光标。
- [x] 1.5 与 `/skills` 共用可显式调用谓词及必要的填入辅助逻辑；保留已有 skills snapshots，并以单测证明 picker 与补全的可接受 Skill 一致且都不加载正文。

## 2. 焦点与提交状态机

- [x] 2.1 在 Model 引入实例 completion 状态与 Update 派生准备；测试值复制不互相修改、短目标不保留旧大草稿、assistant delta/异步回复不清除 Esc/Paste/Modal 抑制。
- [x] 2.2 接入可见候选的上下键、Tab、Enter 和 Esc；测试 Tab 与 Skill Enter 只编辑、内置 Enter 只产生一次既有提交、Esc 首次不取回队列/停止、再次 Esc 走原语义、显式 Tab 只重开被抑制列表。
- [x] 2.3 提取并复用现有 composerSubmit 分支；回归五项本地命令的成功、非法参数及不可用控制错误，断言不把部分名称交给模型、不丢参数、不建立第二套执行路径。
- [x] 2.4 保留换行、Ctrl+C、历史召回、队列取回/returned tray 的原行为；测试 Alt+Enter/Ctrl+J、反斜杠换行、活动 turn/compaction 停止、空闲退出、召回时选择与接受编辑的边界。
- [x] 2.5 为审批、问卷、Hook 信任/浏览、Skills、context/cache、transcript、pending/retracting/cancelling 接入焦点隔离；逐类测试补全隐藏且按键只消费一次，关闭后草稿/光标/原控制身份保持。
- [x] 2.6 保持显式 Paste 优先原子插入并抑制候选自动重开；测试含 Tab/Enter/Esc/Ctrl+C 的正文不触发接受或提交，后续独立 Enter 提交实际文字，超限保持原文本/光标/历史。

## 3. 展示与预算

- [x] 3.1 实现纯内存 completion 布局、安全转义和来源/参数提示；固定 80x24、28x10、20x5、1x1 snapshots，覆盖长说明、中文/emoji、终端/bidi 注入和 NO_COLOR，断言选择标记及各行宽度。
- [x] 3.2 接入主线实际高度扣除、最多五项与一行提示、滚动及极小窗口停用；测试 resize 不改正文/字节光标/身份，选中项可见，零候选行时不吞 Tab/Enter/上下/Esc，并保持 transcript 阅读锚点。
- [x] 3.3 落实最多 261 个身份、单个结果集、2 MiB 新增保留容量与六行 96 KiB 显示预算；确定性容量测试验证极大尺寸/元数据不会无界分配，失败停用整组补全并保留普通编辑，不发布部分目录。
- [x] 3.4 避免流式增量重建目录、匹配和布局；增加 0/256 Skill、64 字节查询、长参数、4 MiB 草稿及连续 delta 基准，记录分配/耗时并以计数断言无关增量复用计算，无特定机器耗时硬门。

## 4. 终端与双 Provider 集成

- [x] 4.1 扩展真实 Bubble Tea 输入解码测试，覆盖 Tab、Enter、独立 Esc、Alt+Enter、Ctrl+J 和完整/分段 bracketed paste；以事件确认同步断言选择、关闭、粘贴后提交和终端退出，不使用 sleep 证明状态。
- [x] 4.2 在 app 层验证正常启动与恢复共用 assembleChatModel 的冻结目录；通过计数/故障注入证明筛选、导航、关闭、Tab 与 Skill Enter 时零 Skill 正文读取、零 Provider 请求、journal bytes 不变。
- [x] 4.3 为 OpenAI 与 Anthropic 分别建立相同最终输入的手输/补全对照；固定等价身份，验证普通及 manual-only Skill 正式准备恰好一次、参数/附件一致、真实 canonical 请求 bytes、native item 顺序和稳定 fingerprint 相等。
- [x] 4.4 验证补全后来源变化、准备取消、预算超限与过期 admission 回复；断言未接收输入不写 journal、不请求 Provider、草稿保持可编辑，既有取消/失败控制不污染后续 turn。
- [x] 4.5 对照 uninterrupted/restored 的下一请求、原生项与缓存证据，并运行现有 Provider golden/cache、Skill/resume 及 headless/stream-json 回归；确认补全状态不进入持久化或 headless 外部协议。
- [x] 4.6 在 Darwin 真实 PTY 完成 `/context`、带参数 Skill、候选 Esc/再次 Esc、审批期间按键、resize 与 NO_COLOR smoke；在 change 的 verification.md 记录实际命令、尺寸、平台、退出/终端恢复结果及未执行检查的原因，不把交叉编译当作原生验收。

## 5. 质量门、文档及验收

- [x] 5.1 同步 README 的发现入口、Tab/Enter/Esc 与 paste 范围，并更新 capability-status、Roadmap P5、总体架构的实际完成边界；新增踩坑仅记录已验证问题，核对所有测试路径和当前 record vocabulary，无新增持久化字段或无用依赖。
- [x] 5.2 执行 `openspec validate add-command-and-skill-completion --strict` 与 `make verify`；在 verification.md 记录 OpenSpec、分支策略、gofmt、vet、Staticcheck、全量测试、架构守卫和 race 的真实结果，失败必须解决或明确交付限制，不弱化测试。
- [x] 5.3 逐项复核规格场景和设计验证矩阵，核对所有任务完成证据、代码边界、原生语义及缓存等价；产出可审阅验收记录，保持未验证事项未勾选，完成后按项目流程同步 delta specs 并另行归档。
