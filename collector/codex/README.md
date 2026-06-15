# Codex Collector

本模块把 Codex hooks 事件转换成 Agent Light 的统一状态，并上报给 server。

## 全局配置

Codex hooks 放在：

```text
~/.codex/hooks.json
```

推荐全量接入以下官方事件：

```text
SessionStart, SubagentStart, UserPromptSubmit, PreToolUse,
PermissionRequest, PostToolUse, PreCompact, PostCompact,
SubagentStop, Stop
```

其中支持 matcher 的事件都建议直接用 `"*"`，避免不同版本的 `source` / `trigger` / tool 名差异导致漏采。

具体步骤：

```text
1. 把 hooks.example.json 里的 hooks 合并到 ~/.codex/hooks.json
2. 不要整文件覆盖已有 ~/.codex/hooks.json，只合并顶层 hooks
3. 在 Codex 里运行 /hooks
4. trust 新增或 changed 的 hooks
5. 新开或 resume 一个会话，验证 SessionStart
6. 查询 server 的 /events?limit=20&details=1 验证事件到达
```

如果你使用 cc-switch 切换 Codex provider，注意：`~/.codex/hooks.json` 只保存 hook 定义，信任记录保存在 `~/.codex/config.toml` 的 `[hooks.state]`。先用 `/hooks` trust 一次，然后把生成的 `[hooks.state]` 原样复制到 cc-switch 的 Codex 通用 `config.toml`，避免切换后反复要求手动信任。

## 状态映射

| Codex event | state | 灯效 | message |
| --- | --- | --- | --- |
| `SessionStart` | `idle` | 绿灯常亮 | `Codex 会话已开始` |
| `UserPromptSubmit` | `thinking` | 黄灯呼吸 | `Codex 正在思考` |
| `SubagentStart` | `busy` | 红灯常亮 | `Codex 子任务正在运行` |
| `PreToolUse` | `busy` | 红灯常亮 | `Codex 正在动手` |
| `PermissionRequest` | `approval` | 红灯快闪 | `Codex 需要审批` |
| `PostToolUse` 成功 | `thinking` | 黄灯呼吸 | `Codex 正在思考` |
| `PostToolUse` 失败 | `thinking` | 黄灯呼吸 | `Codex 工具执行失败，正在处理` |
| `PreCompact` | `thinking` | 黄灯呼吸 | `Codex 正在压缩上下文` |
| `PostCompact` | `thinking` | 黄灯呼吸 | `Codex 已完成上下文压缩` |
| `SubagentStop` | `thinking` | 黄灯呼吸 | `Codex 子任务完成，正在整理` |
| `Stop` | `idle` | 绿灯常亮 | `Codex 空闲` |

## 说明

- `SessionStart` 是瞬时状态，后续很容易被 `UserPromptSubmit` / `PreToolUse` / `Stop` 覆盖。
- `/status` 只返回最后一个状态；要查瞬时事件，请看 `/events?limit=20&details=1`。
- `PostToolUse` 失败仍归到 `thinking`，因为 Codex 往往会继续修正。
- `SubagentStop` 可能晚于主流程状态到达，短暂把灯盖回黄灯；如果你只关心最终空闲，可以在 hooks 配置里不接它。

## 详情字段

collector 会保留这些 `details`：

```text
hookEventName, knownCodexEvent, matcherValue, sessionId, turnId, cwd,
transcriptPath, model, permissionMode, prompt, source, sessionStartSource,
trigger, toolName, toolUseId, toolInputDescription, toolInput, toolResponse,
toolFailed, agentId, agentType, agentTranscriptPath, stopHookActive,
lastAssistantMessage, error, rawHook
```
