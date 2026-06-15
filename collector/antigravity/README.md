# Antigravity Collector

本模块是 Agent Light 的 Antigravity 收集端（Collector）。它能够拦截 Antigravity (Gemini Agent) 的 hooks 事件，将其转换为统一的状态上报给 Agent Light 服务器，并把对应的决策结果（stdout JSON）反馈给 Antigravity 以免阻断其执行。

---

## 配置文件路径

Antigravity 支持以下两种级别的 hooks 配置：

1. **全局级配置（推荐）**：
   * 路径：`~/.gemini/config/hooks.json`
   * 生效范围：在你运行 Antigravity 的**所有**项目目录中都会生效。
   
2. **项目级配置**：
   * 路径：`<workspace>/.agents/hooks.json`
   * 生效范围：仅在当前工作区生效。

---

## 配置内容示例

你需要将 `hooks.json` 配置为以下结构。**注意：如果你在全局配置，请务必将脚本路径替换为绝对路径**。

具体步骤：

```text
1. 打开 ~/.gemini/config/hooks.json
2. 如果文件不存在，创建 {}
3. 新增或更新顶层命名组 agent-light
4. 把下面片段写进 agent-light 组
5. 把脚本路径替换成本项目真实绝对路径
6. 重启或重新打开 Antigravity 会话
7. 查询 server 的 /events?limit=20&details=1 验证事件到达
```

注意：Antigravity 顶层不是 `{"hooks": ...}`，而是命名组结构，例如 `{"agent-light": {...}}`。

```json
{
  "agent-light": {
    "PreInvocation": [
      {
        "type": "command",
        "command": "node /absolute/path/to/agent_light/collector/antigravity/antigravity-hook.js PreInvocation"
      }
    ],
    "PostInvocation": [
      {
        "type": "command",
        "command": "node /absolute/path/to/agent_light/collector/antigravity/antigravity-hook.js PostInvocation"
      }
    ],
    "PreToolUse": [
      {
        "matcher": "*",
        "hooks": [
          {
            "type": "command",
            "command": "node /absolute/path/to/agent_light/collector/antigravity/antigravity-hook.js PreToolUse"
          }
        ]
      }
    ],
    "PostToolUse": [
      {
        "matcher": "*",
        "hooks": [
          {
            "type": "command",
            "command": "node /absolute/path/to/agent_light/collector/antigravity/antigravity-hook.js PostToolUse"
          }
        ]
      }
    ],
    "Stop": [
      {
        "type": "command",
        "command": "node /absolute/path/to/agent_light/collector/antigravity/antigravity-hook.js Stop"
      }
    ]
  }
}
```

---

## 映射与决策逻辑

### 状态映射

官方 hook 事件：

```text
PreInvocation, PostInvocation, PreToolUse, PostToolUse, Stop
```

| Antigravity Hook | 统一状态 `state` | 灯效 | 说明 |
| --- | --- | --- | --- |
| `PreInvocation` | `thinking` | 黄灯呼吸 | 开始思考与分析 |
| `PostInvocation` | `thinking` | 黄灯呼吸 | 工具调用阶段完成，执行循环可能继续 |
| `PreToolUse` (低危工具) | `busy` | 红灯常亮 | 执行低风险工具，自动允许 |
| `PreToolUse` (高危工具) | `approval` | 红灯快闪 | 执行高风险工具，挂起并等待用户审批 |
| `PostToolUse` 成功 | `thinking` | 黄灯呼吸 | 工具执行完成，继续处理 |
| `PostToolUse` 失败 | `thinking` | 黄灯呼吸 | 工具失败后继续处理，错误在 `details.error` |
| `Stop` (`fullyIdle == true`) | `idle` | 绿灯常亮 | 会话本轮任务彻底结束，释放灯效 |
| `Stop` (`fullyIdle == false`) | `busy` | 红灯常亮 | 会话暂停，但后台仍有正在运行的任务 |
| `Stop` 取消 / 中断 | `idle` | 绿灯常亮 | 执行已停止，原因在 `details.terminationReason` |

### 默认拦截与审批名单

在 `PreToolUse` 事件中，以下工具会被拦截并要求审批（即返回 `"ask"` 或 `"force_ask"`，并令指示灯呈现 `approval` 的红灯快闪状态）：
* `run_command` (返回 `"ask"`)
* `ask_permission` (返回 `"force_ask"`)
* `write_to_file` (返回 `"ask"`)
* `replace_file_content` (返回 `"ask"`)
* `multi_replace_file_content` (返回 `"ask"`)
* `search_web` (返回 `"ask"`)
* `read_url_content` (返回 `"ask"`)

其他工具（如 `view_file`, `list_dir`, `grep_search` 等）将自动允许执行（即返回 `"allow"`，且指示灯为 `busy` 状态的红灯常亮）。

### details 字段

Antigravity `details` 会保留：

```text
hookEventName, knownAntigravityEvent, toolName, toolArgs, stepIdx,
invocationNum, initialNumSteps, conversationId, workspacePaths,
transcriptPath, artifactDirectoryPath, error, fullyIdle,
terminationReason, executionNum, decision, rawHook
```

---

## 本地测试

你可以直接 curl POST 来模拟 Antigravity 的 hooks 信号上报。

1. **启动开发服务器**：
   ```bash
   cd /Users/apple/user/VscodeProject/agent_light/server
   go run .
   ```

2. **手动发事件**（直接 curl POST，无需脚本）：
     ```bash
     curl -s http://127.0.0.1:4318/api/devices/desk-light-01/events \
       -H 'Authorization: Bearer <collector-token>' \
       -H 'Content-Type: application/json' \
       -d '{"source":"antigravity","state":"approval","event":"PreToolUse","message":"测试审批"}'
     ```
   `state` 取值：`approval`（红灯快闪）/ `busy`（红灯常亮）/ `thinking`（黄灯慢闪）/ `idle`（绿灯常亮）。

3. **查询当前设备状态**：
   ```bash
   curl -s -H 'Authorization: Bearer <device-token>' \
     http://127.0.0.1:4318/api/devices/desk-light-01/status
   ```
