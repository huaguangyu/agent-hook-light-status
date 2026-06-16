# 状态模型

Agent Light 把不同 AI 工具的 hooks 事件统一成 4 个状态：

| state | 含义 | HTTP `color` | WLED preset |
| --- | --- | --- | --- |
| `idle` | 空闲、会话开始、回合结束、停止运行 | `green` | 1 |
| `thinking` | AI 正在分析、生成、压缩上下文、工具结束后继续判断 | `yellow` | 2 |
| `busy` | AI 正在执行工具、命令、子任务或后台任务 | `red` | 3 |
| `approval` | 明确需要用户审批、确认或输入 | `red` | 4 |

核心提醒语义：

```text
approval = 需要你参与
busy     = AI 正在动手
thinking = AI 正在思考
idle     = 空闲
```

## last-write-wins

server 按 `deviceId` 分桶保存状态。每个 `deviceId` 内部采用 `last-write-wins`：

```text
后收到的事件覆盖同一个 deviceId 的当前状态
不同 deviceId 互不影响
```

`/status` 只返回当前状态。像 `SessionStart` 这种瞬时事件，可能很快被 `UserPromptSubmit`、`PreToolUse` 或 `Stop` 覆盖。排查瞬时事件时看：

```text
GET /api/devices/:deviceId/events?limit=20&details=1
```

## TTL 回落

超过 `idleTtlMs` 没有新事件时，状态查询会回落到 `idle`。默认值是 20 分钟：

```json
{
  "idleTtlMs": 1200000
}
```

如果启用了 MQTT，server 的后台离线检查也会给对应 `deviceId` 推送 idle preset。

## 事件日志

server 给每个 `deviceId` 独立保留最近事件，默认 100 条：

```json
{
  "maxRecentEvents": 100
}
```

不会把不同用户或不同灯的事件混在一起。

## HTTP 字段

`GET /status` 只保留通用状态字段和一个调试用 `color`：

```json
{
  "state": "approval",
  "color": "red",
  "message": "Codex 需要审批",
  "source": "codex",
  "event": "PermissionRequest",
  "updatedAt": "2026-06-16 20:00:00"
}
```

服务端不再返回 `light`、`effect`、`display`。具体动画在 WLED preset 里配置。

## 三家事件映射

详细映射见：

| 工具 | 文档 |
| --- | --- |
| Codex | [collector/codex/README.md](../collector/codex/README.md) |
| Claude Code | [collector/claude-code/README.md](../collector/claude-code/README.md) |
| Antigravity | [collector/antigravity/README.md](../collector/antigravity/README.md) |
