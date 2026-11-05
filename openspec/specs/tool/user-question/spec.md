# tool/user-question Specification

## Purpose
定义模型在当前工具调用位置向用户收集决定的能力，使问题、选项、文字回答和跳过状态具有明确关联，并能在双 Provider、持久化、取消及恢复之间保持完整、一致且有界的语义。

## Requirements

### Requirement: Questions and answers are bounded typed values

系统 SHALL 以 `user.question` 表达提问，输入仅包含有序 questions，数量 1–4；每题包含唯一 ASCII snake_case id、header、question、2–4 个有序 options 和 multi_select。id 为 1–64 bytes，header 为 1–12 个 Unicode code points，question 为非空白且不超过 2048 UTF-8 bytes；每个 option 的 label/description 分别为非空白且不超过 128/1024 UTF-8 bytes。题目文本在请求内唯一，选项 label 在题内唯一；完整原始参数不超过 64 KiB。模型不得提供 answers、权限决定、其他选项占位、宿主字段或任意扩展。

回答 SHALL 按题目顺序保存 question_id、零基 selected_options、text 和 skipped。索引必须唯一、有效且按选项原顺序规范化；单选最多一个，多选最多四个。非 skipped 回答必须至少选择一项或提供非空白文字；skipped 必须没有选项和文字。文字既可独立作为其他回答，也可补充已选项；保留正文空白，仅作非空判断。每次回答文字合计最多 16 KiB UTF-8 bytes，并受实际编码与结果预算约束。缺失/重复/未知题目、越界索引、null、重复键、大小写别名、未知字段、非法 Unicode 或尾随 JSON MUST 失败关闭。

#### Scenario: Answer with choices and notes
- **WHEN** 用户对多选题选择第二、第一项并补充说明
- **THEN** 回答按原选项顺序冻结两个索引，保留完整说明并绑定原 question_id

#### Scenario: Reject model-supplied answers
- **WHEN** Provider 参数中包含 answers 或未知宿主字段
- **THEN** sample 失败，不出现 ready、弹窗或任何工具执行

#### Scenario: Reject mismatched answers
- **WHEN** 回答包含重复 question_id、未知索引或单选题的多个选项
- **THEN** 回答 admission 拒绝，活动请求、草稿和 Session 不变

### Requirement: Question execution waits at one exclusive call slot

提问 SHALL 在完整 sample、usage 和 ready facts 已 Sync/finalize、前序调用结果已 durable 后准入。成功 started Sync 后才发布一个交互请求；等待期间后序调用不得准备候选、请求审批或执行，下一 sample 不得启动。提问无需副作用批准，不产生 approval fact 或任何文件、命令、session grant；同一 sample 的多个提问按原 call index 逐个处理。

回答结果 SHALL 包含完整冻结回答、明确跳过状态与匹配调用身份。普通跳过仅结束本次提问并允许模型继续；stop、取消和 shutdown 按既有 turn 取消与有序 outputs 契约收口。用户选择任何名为 Yes/Allow 的选项都不能改变工具授权。

#### Scenario: Order a read question and write
- **WHEN** 同一 sample 按 Read、Question、Write 排列
- **THEN** Read 结果先 durable；问题解决前 Write 不准备、不审批、不执行；最终 outputs 仍按原顺序提交

#### Scenario: Keep answers separate from permission
- **WHEN** 用户选择标签为 Allow all edits 的普通问题选项
- **THEN** 仅保存该问题回答，后续 Patch/Write/Exec 仍遵循原 policy 和审批

### Requirement: Noninteractive questions return a paired unavailable result

无交互回答能力的 --print、--json 与 stream-json SHALL 返回 `denied/user_input_unavailable` 的有界工具结果和匹配 Provider output，不发布交互请求、不读取 stdin、不启动等待任务、不代选推荐项。目录定义保持相同；模型可在既有 sample/call 预算内继续，外部 headless 格式和命令词汇不扩展。

#### Scenario: Ask in print mode
- **WHEN** 模型在 --print 中调用提问工具
- **THEN** 立即获得配对不可用结果，进程不因等用户回答而挂起，stdout 不出现问卷或控制事件

### Requirement: Question recovery reuses committed answers without asking again

恢复 SHALL 只依据当前 ready/started/result/native-output 事实。仅 ready 的提问补偿 `cancelled/session_interrupted_before_execution`；started 无结果沿用 `outcome_uncertain`；result 已 durable 则复用原始完整 preview bytes 补齐 outputs。恢复不弹题、不读取 UI 草稿、不代答、不重跑任一工具、不联网，并在本地补偿后关闭旧 turn。

#### Scenario: Crash while waiting for an answer
- **WHEN** journal 只有提问 started 而没有 result
- **THEN** 恢复以不确定结果配对收口，不重新显示问题或猜测用户曾作答

#### Scenario: Crash after answer persistence
- **WHEN** 回答 result 已 Sync 而 native outputs 尚未提交
- **THEN** 恢复使用逐字节相同的回答补齐同 call ID 输出，零用户交互和工具执行
