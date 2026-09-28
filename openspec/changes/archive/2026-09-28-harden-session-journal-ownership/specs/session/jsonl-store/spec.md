## MODIFIED Requirements

### Requirement: A single writer assigns order and durably appends batches

每个活动 thread SHALL 在跨 goroutine、跨 Repository 实例和跨进程范围内只有一个 Session writer。进程在创建首个 metadata batch，或对既有 journal 执行可能 repair、续写的 load 前，MUST 先取得绑定到该 journal handle 的 exclusive lease；同一 lease MUST 连续保持到 writer 完成最终 Sync、关闭文件并停止接受 append。系统不得在 load 后释放所有权再重新打开文件续写。

当其他进程已经持有 lease 时，竞争者 MUST 在读取、截断、Provider 恢复或其他可见副作用前以稳定英文 `session_busy` 错误快速失败，且 journal bytes MUST 保持不变。lease MUST 随持有 handle 关闭或进程终止自动释放；系统不得依赖 PID 文件、超时删除或进程级全局 mutex 猜测所有权。

writer MUST 分配连续递增的 `seq` 与记录时间，调用方不得自行选择或回退序号。一个逻辑 batch 中的记录 MUST 按调用顺序编码，并 SHALL 以全有或全无的恢复语义追加；系统只有在该 batch 已写入并对文件执行成功 Sync 后才能向上层确认 durable success。

append admission MUST 有明确线性化点：在请求被接受前发生的 context 取消或有界队列背压 MUST 在分配 seq、写盘或产生其他副作用前拒绝请求；请求一旦被接受，writer MUST 返回确定的 durable success 或失败结果，不得因调用方随后取消而留下无人确认的后台提交。Close MUST 拒绝新的 admission、完成所有已接受请求，然后执行最终 Sync、关闭 handle 并释放 lease。状态检查与 admission 不得在锁内执行阻塞 channel 操作、磁盘 I/O 或等待其他 goroutine。

写入或 Sync 结果不确定时，writer MUST 进入不可继续写入的失败状态；同一进程不得在该 writer 上继续分配序号或启动新的 Provider 副作用。

#### Scenario: Acquire one writer across processes

- **WHEN** 两个进程同时尝试恢复并续写同一个 thread journal
- **THEN** 恰好一个进程取得 exclusive lease 并成为活动 writer
- **THEN** 另一个进程以 `session_busy` 失败，且不得读取后 repair、截断或追加该 journal

#### Scenario: Release ownership after process termination

- **WHEN** 持有 thread lease 的进程在未执行应用级 Close 的情况下终止
- **THEN** 操作系统释放该 lease，后续进程可以重新取得所有权并从完整校验后的下一 seq 继续
- **THEN** 系统不通过删除 stale PID 文件或等待租约 TTL 恢复所有权

#### Scenario: Append a successful batch

- **WHEN** writer 追加包含 Provider 原生提交和 turn 完成边界的 batch
- **THEN** 记录获得连续 seq、保持给定顺序并在 durable success 返回前完成文件 Sync

#### Scenario: Serialize concurrent append attempts

- **WHEN** 同一 writer 同时收到多个 append 请求
- **THEN** writer 串行处理已接受请求且最终文件中的 seq 严格递增，不出现重复、倒序或交错 payload

#### Scenario: Reject before append admission

- **WHEN** append context 在 admission 前已经取消，或有界 admission 队列没有容量
- **THEN** writer 在分配 seq 或写入 bytes 前拒绝请求
- **THEN** 既有 journal、后续 seq 和 writer 的可继续状态不受该拒绝影响

#### Scenario: Close races with append admission

- **WHEN** Close 与多个 append 请求并发发生
- **THEN** 每个请求都被明确归类为 Close 前已接受并获得确定结果，或 Close 后未接受且零副作用
- **THEN** Close 只在已接受请求处理完成、最终 Sync 和 handle 关闭后返回

#### Scenario: Stop after an ambiguous write failure

- **WHEN** append 或 Sync 返回无法确认 durable 状态的错误
- **THEN** writer 返回安全英文 session 错误并拒绝后续写入
- **THEN** 上层不会继续当前 conversation 的模型调用或其他副作用

## ADDED Requirements

### Requirement: Published Session revisions remain replay-compatible through immutable fixtures

每个已发布的 envelope schema revision 和已知 payload revision MUST 在仓库中保留由固定历史 bytes 构成的不可变 compatibility fixture，以及对应的预期 ReplayPlan 摘要。fixture MUST 独立于当前 encoder 生成，MUST 使用与生产相同的 Loader、checksum、batch、registry 和 ReplayPlanner 路径验证，并不得包含 secret、敏感 Header、base URL 或本机路径。

当前程序 MUST 能读取所有仍受支持的历史 fixture，并在其末尾继续追加更大 seq，而不重写、重新编码或复制 fixture 中的既有 records。版本升级 MUST 在写入新 revision 前增加从所有受支持旧 fixture 到当前 replay model 的 migration regression；默认迁移语义是版本专属解码与只读投影，不是在 resume 时原地改写 journal。

当前程序遇到不受支持的较新 required schema/kind/payload revision 时 MUST 在任何 repair、append 或 Provider 副作用前失败，并保持原始 bytes 不变。

#### Scenario: Replay the immutable v1 baseline

- **WHEN** 当前程序加载仓库内固定的 v1 root-thread JSONL fixture
- **THEN** Loader 与 ReplayPlanner 产生预期 identity、metadata、turn boundaries、native commits 和下一 seq
- **THEN** fixture 不是由测试运行时调用当前 encoder 临时生成

#### Scenario: Continue a historical fixture without rewriting it

- **WHEN** 程序从受支持的历史 fixture 恢复并 durable 追加一个新 batch
- **THEN** 新记录从 fixture 的下一 seq 开始
- **THEN** 追加前的全部 fixture bytes 保持逐字节不变

#### Scenario: Reject an unsupported newer required revision read-only

- **WHEN** journal 包含当前程序不支持的较新 required schema、kind 或 payload revision
- **THEN** 加载或恢复以稳定英文 Session 错误失败
- **THEN** journal 不被 repair、迁移、截断或追加，且不会发起 Provider 请求
