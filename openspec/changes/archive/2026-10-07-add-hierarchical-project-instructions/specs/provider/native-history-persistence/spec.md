## MODIFIED Requirements

### Requirement: Restored requests match uninterrupted requests

对相同 Provider 配置、相同已提交历史、相同下一条用户输入和相同项目指令快照，从 JSONL 恢复后的 RequestCompiler 输出 MUST 与未退出进程的 Conversation 输出保持相同的 Provider-native item 顺序、canonical request bytes 和 cache fingerprint。恢复不得生成新的 Provider item ID、签名、encrypted content 或其他 wire 字段来替代持久化值。

项目指令上下文 item/message SHALL 在请求时由 Provider 从当前不可变快照重新生成，MUST NOT 编码进 native commit、prepared sample 或恢复 codec。项目指令快照变化时，已恢复 native history 及其 revision、item 顺序和 opaque bytes MUST 保持不变；只有重新生成的临时上下文、请求 bytes 和对应 cache fingerprint 可发生内容派生的变化。

#### Scenario: Continue a restored OpenAI conversation

- **WHEN** OpenAI 会话持久化多个 commits、重启后发现相同项目指令快照并提交相同的下一条输入
- **THEN** 下一次 Responses request 与未重启路径的 input 顺序、canonical bytes 和 fingerprint 完全相同

#### Scenario: Continue a restored Anthropic conversation

- **WHEN** Anthropic 会话持久化多个 commits、重启后发现相同项目指令快照并提交相同的下一条输入
- **THEN** 下一次 Messages request 与未重启路径的 messages 顺序、canonical bytes 和 fingerprint 完全相同

#### Scenario: Resume after project instructions change

- **WHEN** 相同 Provider commits 在新进程中恢复，但新进程发现的项目指令快照与退出前不同
- **THEN** restored native history 的 revision、原生 item 顺序和 opaque bytes 与原 commits 完全一致
- **THEN** 下一请求只替换单一临时项目指令上下文，并产生可归因于新快照的 canonical bytes 与 fingerprint 变化
