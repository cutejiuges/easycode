## 1. Catalog 列表查询

- [x] 1.1 在 apply 开始时复查当前 artifacts 与 `17ad2/easycode` 的 `add-command-execution` 差异，核对 main 路由、Catalog 和共享文档冲突；以只读 diff 确认未复制其未提交代码，若出现能力重叠先更新提案。
- [x] 1.2 新增 `internal/session/catalog/list.go` 的强类型 scope/query/result、校验与 `List` 查询，使用现有 schema、ready/decodeEntry 和绑定参数；在 `list_test.go` 覆盖未知 scope、矛盾目录条件、非法路径、1/200 边界及 0/负数/201 拒绝，运行 `go test ./internal/session/catalog`。
- [x] 1.3 实现先目录筛选、双降序排序、limit+1 与 HasMore；以固定 Entry 测试空集、N/N+1、混合 Provider/model、同时间 ID 平局、子目录/同名前缀/worktree 不误匹配、全部目录和取消，并保留 LatestCompatible 精确筛选回归。

## 2. 独立应用生命周期

- [x] 2.1 新增 `internal/app/session_list.go` 与强类型参数，复用数据路径、规范化 cwd、Repository/Catalog 显式生命周期，实现协调后查询、先关闭再输出；通过 `session_list_test.go` 验证无配置、无效配置、无 API key、空存储、全目录不依赖 cwd、不发现项目指令、零 Provider 请求且零新 journal。
- [x] 2.2 接入 busy/invalid 全局警告、空列表成功、存储失败安全 Outcome 和取消；使用包内小型实例依赖/故障注入验证 open/reconcile/query/close 失败路径、阶段间取消、stdout/stderr 写入失败及资源最终关闭，不引入全局 hook 或 helper goroutine，运行 app 定向测试。

## 3. 文本展示与 CLI

- [x] 3.1 新增纯内存条目格式化与字段转义，固定完整 ID、UTC RFC3339Nano、Provider/wire/model、范围/目录、空状态、HasMore 及恢复指引；在 `internal/app/session_list_render_test.go` 和 `internal/app/testdata/session-list/` 提交当前/全部目录、空、截断与警告 golden，证明输出不依赖终端宽度或当前时间。
- [x] 3.2 补充 model/wire 256 与目录 512 code point 上限、保留完整转义单元和 UTF-8 的回归；覆盖 CR/LF/TAB、C0/C1/DEL、OSC/ANSI、Unicode 行分隔、bidi、超长多字节值及标题目录，验证不会伪造条目、输出 secret/正文/opaque payload 或拼接带路径的 shell 命令。
- [x] 3.3 新增 `cmd/easycode/sessions.go` 并仅在 main.go 的 run 增加首参数路由；在 `sessions_test.go` 覆盖默认 20、`--all`、`--limit`、两级帮助、缺失/未知子命令、聊天 flag 拒绝、额外位置参数、非整数/溢出参数，验证帮助与 usage error 在存储 IO 前返回 0/2。
- [x] 3.4 补充命令解析集成回归，验证 `--print "sessions list"` 仍是 prompt，`--resume`、`--continue`、`--json` 和 stream-json 原路径不变，使用拒绝读取的 stdin 验证列表零读取，运行 `go test ./cmd/easycode ./internal/app`。

## 4. 存储与恢复边界集成验证

- [x] 4.1 在 Catalog/app 列表集成测试中构造混合目录、Provider/model 的真实 journals，删除或用现有 corrupt/incompatible SQLite fixture 替换索引后重建；断言列表顺序/HasMore 等价、完整 journal bytes 不变、schema version 未变及无额外 native/terminal/tool records。
- [x] 4.2 复用 `internal/session/catalog/process_test.go` 的真实子进程握手模式，覆盖 indexed busy、unindexed busy、持有期间禁止 repair、进程释放后下一次列表可刷新；断言 busy 警告为全局、旧元数据不冒充实时、`--continue` 仍对 BusyUnindexed 失败关闭，禁止用 sleep 证明正确性。
- [x] 4.3 验证列表对未完成 turn 零补偿、对允许 EOF 半行/尾 batch 仅在 lease 下修复，以及 deleted/invalid journal 排除；复用既有安全/fault fixture 覆盖 symlink、目录替换与 Sync 失败，断言根外文件和合法已提交 bytes 不变，失败不会伪装空列表。
- [x] 4.4 增加“创建会话 -> 列出完整 ID -> 显式恢复”双 Provider 本地 HTTP stub 集成测试，断言列表阶段零请求、恢复仍检查 family/wire/model、续写 seq 单调、列表前后既有记录不变，并运行现有 canonical request/native restore/cache regression，证明未改变请求语义。

## 5. 文档、审查与质量门

- [x] 5.1 更新 README、`docs/architecture/overall-architecture.md` 的 Catalog 调用入口和 `docs/roadmap/product-roadmap.md` 的 P2 进度，明确 CLI 已交付但 picker/title/worktree 仍未实现、全量扫描与允许尾修复；将实际遇到的新增问题写入 pitfall，逐项核对命令名、测试路径及 reference 取舍与实现一致。
- [x] 5.2 完成契约审查：仅预期 CLI/app/catalog 接口变化，JSONL/SQLite vocabulary、Provider/native/cache/tool schema 不变，无 secret 泄漏、全局可变状态、空 facade、重复副作用或无用依赖；用 `git diff --check`、`openspec validate add-session-list-command --strict` 和实际 diff 验证。
- [x] 5.3 运行完整 `make verify`，确认 gofmt、go vet、Staticcheck、架构边界、全量测试及 race 全部通过；将执行结果与剩余限制用于交付说明，验收后按仓库流程同步两份 delta specs 并归档，不在实现未完成时标记任务完成。

## 验证记录

- 2026-10-23：`make verify` 通过，包含 gofmt、go vet、Staticcheck、架构测试、全量测试与 race。
- 列表新增测试分布于 `cmd/easycode/sessions_test.go`、`internal/app/session_list*_test.go`、`internal/session/catalog/list*_test.go`；固定文本在 `internal/app/testdata/session-list/`。
- TOCTOU 复用既有 `internal/session/repository_security_test.go`、`repository_enumeration_test.go` 的确定性注入；Sync 失败通过应用协调边界注入验证失败输出与清理，并运行既有 `TestJournalWriterPoisonsAfterSyncFailure`。本变更未修改底层 Loader/Writer，不将边界注入称为实际磁盘故障测试。
- 2026-10-23：归档前再次通过 `make verify` 与严格 change 校验；两份 delta 的全部需求及场景已核对与主规格一致，按用户请求归档。
