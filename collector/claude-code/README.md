# Claude Code Collector

本模块把 Claude Code hooks 事件转换成 Agent Light 的统一状态，并上报给 server。

第一次部署请先完成根目录 [README.md](../../README.md) 的“新手快速部署”：启动 server、配置 WLED、确认 MQTT 自测可用。然后再配置本 collector。

## 先改 collector 脚本顶部配置

打开：

```text
collector/claude-code/claude-hook.js
```

把顶部三项改成你的实际值：

```js
const DEVICE_ID = "desk-light-01";
const SERVER_URL = "http://127.0.0.1:4318";
const COLLECTOR_TOKEN = "填 server/env.json 里的 collectorToken";
```

对应关系：

```text
DEVICE_ID=<deviceId>
WLED Device Topic=wled/<deviceId>
server 发布 topic=wled/<deviceId>/api

例如：
DEVICE_ID=desk-light-01 -> WLED Device Topic=wled/desk-light-01
```

远程部署时，把 `SERVER_URL` 改成你的 server 地址。

## CLI 与 Desktop

优先按同一套 Claude Code hooks 做：

```text
collector/claude-code/claude-hook.js
```

如果 Claude Code Desktop 读取同一个 `~/.claude/settings.json`，CLI 和 Desktop 可以共用这一份全局配置。若 Desktop 使用自己的配置目录，仍然复用同一个 hook 脚本，只需要把相同的 `hooks` 配置写到 Desktop 对应 settings 文件里。

## 全局配置

Claude Code 用户级配置通常放在：

```text
~/.claude/settings.json
```

把 [settings.example.json](settings.example.json) 里的脚本路径替换成项目绝对路径：

```text
/Users/apple/user/VscodeProject/agent_light/collector/claude-code/claude-hook.js
```

本项目示例默认全量接入 Claude Code 当前官方生命周期事件。`FileChanged` 的 matcher 不是通配符，而是要监听的具体文件名；示例默认监听 `CLAUDE.md|settings.json|.env|.envrc`，可以按你的项目改。

若你只想让灯更安静，可以只保留核心 10 个事件：

```text
SessionStart
UserPromptSubmit
PreToolUse
PostToolUse
PostToolUseFailure
PermissionRequest
Notification
Stop
StopFailure
SessionEnd
```

> 不要接 `SubagentStop`：它会在主会话 `Stop` 之后触发，把 `idle` 盖回 `thinking`，导致回合结束灯不转绿。子 agent 的活动已由 `PreToolUse`/`PostToolUse`（Agent 工具本身）覆盖。

具体步骤：

```text
1. 打开 ~/.claude/settings.json
2. 如果文件不存在，创建 {}
3. 保留已有 permissions、hooks 等配置
4. 把 settings.example.json 里的 hooks 合并到顶层 hooks 字段；全量接入时保留全部事件
5. 把脚本路径替换成本项目真实绝对路径
6. 按需要调整 `FileChanged` 的 matcher 文件名列表
7. 在 Claude Code 里运行 `/hooks` 查看已注册事件
8. 触发一个 Claude Code 新会话或工具调用
9. 查询 server 的 /events?limit=20&details=1 验证事件到达
```

## 状态映射

统一灯效：

```text
idle = 绿灯常亮；thinking = 黄灯呼吸；busy = 红灯常亮；approval = 红灯快闪
```

| Claude Code Hook | 统一状态 | 灯效 | 运行时 message | 说明 |
| --- | --- | --- | --- | --- |
| `SessionStart` | `idle` | 绿灯常亮 | `Claude Code 会话已开始 (...)` | 会话开始 / resume / clear / compact |
| `Setup` | `thinking` | 黄灯呼吸 | `Claude Code 正在初始化 (...)` | `--init-only`、`--init`、`--maintenance` |
| `UserPromptSubmit` | `thinking` | 黄灯呼吸 | `Claude Code 正在思考` | 用户 prompt 提交 |
| `UserPromptExpansion` | `thinking` | 黄灯呼吸 | `Claude Code 正在展开指令...` | 命令 / skill 展开 |
| `PreToolUse` | `busy` | 红灯常亮 | `Claude Code 正在执行工具: <tool>` | 工具执行前 |
| `PermissionRequest` | `approval` | 红灯快闪 | `Claude Code 需要审批: <tool>` | 权限弹窗出现 |
| `PermissionDenied` | `thinking` | 黄灯呼吸 | `Claude Code 权限被拒，正在调整: <tool>` | 权限被自动模式拒绝 |
| `PostToolUse` 成功 | `thinking` | 黄灯呼吸 | `Claude Code 工具执行完成，正在判断下一步` | 工具成功后 |
| `PostToolUseFailure` / 工具失败 | `thinking` | 黄灯呼吸 | `Claude Code 工具执行失败，正在处理` | 工具失败后，错误看 `details.error` |
| `PostToolBatch` | `thinking` | 黄灯呼吸 | `Claude Code 工具批次完成，正在继续处理` | 并行工具批次完成 |
| `Notification` + `permission_prompt` | `approval` | 红灯快闪 | `Claude Code 等待工具审批` | 需要审批 |
| `Notification` + `idle_prompt` | `approval` | 红灯快闪 | `Claude Code 已完成，等待你的下一步` | 等待用户输入，提醒查看 |
| `Notification` + `elicitation_dialog` | `approval` | 红灯快闪 | `Claude Code MCP 请求用户输入` | MCP 弹出输入表单 |
| `Notification` + 其他通知 | `thinking` | 黄灯呼吸 | `Claude Code 通知更新` 或具体通知 | 普通通知 |
| `MessageDisplay` | `thinking` | 黄灯呼吸 | `Claude Code 正在显示回复` | assistant 消息展示中 |
| `SubagentStart` | `busy` | 红灯常亮 | `Claude Code 子任务正在运行...` | 子 agent 启动 |
| `SubagentStop` | `thinking` | 黄灯呼吸 | `Claude Code 子任务完成，正在整理...` | 子 agent 结束；全量接入时可能盖过 `Stop` |
| `TaskCreated` | `busy` | 红灯常亮 | `Claude Code 任务已创建: <task>` | 创建任务 |
| `TaskCompleted` | `thinking` | 黄灯呼吸 | `Claude Code 任务已完成，正在整理: <task>` | 标记任务完成 |
| `Stop` | `idle` | 绿灯常亮 | `Claude Code 空闲` | Claude 完成响应 |
| `StopFailure` | `idle` | 绿灯常亮 | `Claude Code 异常结束，已停止运行 (...)` | API / 系统错误导致回合结束 |
| `TeammateIdle` | `idle` | 绿灯常亮 | `Claude Code 队友空闲` | agent team 队友即将空闲 |
| `InstructionsLoaded` | `thinking` | 黄灯呼吸 | `Claude Code 已加载指令 (...)` | CLAUDE.md / rules 加载 |
| `ConfigChange` | `thinking` | 黄灯呼吸 | `Claude Code 配置已变更...` | settings / skills 改变 |
| `CwdChanged` | `thinking` | 黄灯呼吸 | `Claude Code 工作目录已变更: <cwd>` | 工作目录变化 |
| `FileChanged` | `thinking` | 黄灯呼吸 | `Claude Code 监听文件已变更: <file>` | watched file 变化 |
| `WorktreeCreate` | `busy` | 红灯常亮 | `Claude Code 正在创建 worktree: <path>` | 创建 worktree |
| `WorktreeRemove` | `busy` | 红灯常亮 | `Claude Code 正在移除 worktree: <path>` | 移除 worktree |
| `PreCompact` | `thinking` | 黄灯呼吸 | `Claude Code 正在压缩上下文 (...)` | 压缩前 |
| `PostCompact` | `thinking` | 黄灯呼吸 | `Claude Code 已完成上下文压缩 (...)` | 压缩后 |
| `Elicitation` | `approval` | 红灯快闪 | `Claude Code MCP 需要用户输入` | MCP 请求用户输入 |
| `ElicitationResult` | `thinking` | 黄灯呼吸 | `Claude Code MCP 用户输入已返回` | MCP 输入结果返回 |
| `SessionEnd` | `idle` | 绿灯常亮 | `Claude Code 会话已结束 (...)` | 会话结束 |

全量接入会记录更多细状态，但 `SubagentStop`、`MessageDisplay`、`FileChanged`、`CwdChanged` 这类事件可能比较高频；如果你只关注灯最终是否空闲，可以从全局 hooks 里去掉它们。

## 本地测试

启动 server：

```bash
cd /Users/apple/user/VscodeProject/agent_light/server
go run .
```

手动发事件（直接 curl POST，无需脚本）：

```bash
curl -s http://127.0.0.1:4318/api/devices/desk-light-01/events \
  -H 'Authorization: Bearer <collector-token>' \
  -H 'Content-Type: application/json' \
  -d '{"source":"claude-code","state":"approval","event":"ManualTest","message":"测试"}'
```

`state` 取 `idle` / `thinking` / `busy` / `approval`。

查询灯状态：

```bash
curl -s -H 'Authorization: Bearer <device-token>' \
  http://127.0.0.1:4318/api/devices/desk-light-01/status
```

## 注意

Claude Code 的 hook 能可靠捕捉工具执行、审批、Stop 等事件。若某个 Desktop 版本没有触发 hooks，Agent Light 无法从该进程直接拿到状态；这时需要确认 Desktop 是否读取 `~/.claude/settings.json`，或改用它提供的独立集成方式。
